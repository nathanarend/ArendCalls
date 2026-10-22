package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
)

// Lado A (origem) da migração. Ver o diagrama em migration.go.

// migConflictError vira 409: estado incompatível, não adianta repetir.
type migConflictError struct{ msg string }

func (e *migConflictError) Error() string { return e.msg }

func migConflict(format string, args ...any) error {
	return &migConflictError{msg: fmt.Sprintf(format, args...)}
}

const migPhaseUnlocked = "unlocked" // origem desbloqueada sem migrar (abort)

// lockTimeout limita a espera pelo upload de gravação em curso antes do snapshot.
const lockTimeout = 2 * time.Minute

func (m *migrator) sourceDir(id string) string {
	return filepath.Join(filepath.Dir(m.s.dbPath), "migration-out-"+id)
}

func validMigrationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// lockSource para tudo em A e gera o snapshot. Idempotente para o mesmo id:
// se a resposta se perdeu, B pede de novo e recebe o mesmo manifest.
func (m *migrator) lockSource(ctx context.Context, id, target string) (*migrationManifest, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	st, _, _ := m.snapshot()
	switch {
	case st.Role == migRoleSource && st.Phase == migPhaseLocked && st.ID == id && st.Manifest != nil:
		return st.Manifest, nil
	case st.Role == migRoleSource && st.Phase == migPhaseLocked:
		return nil, migConflict("origem já bloqueada por outra migração (%s)", st.ID)
	case st.Role == migRoleSource && st.Phase == migPhaseCommitted:
		return nil, migConflict("esta instância já foi migrada para %s", st.PeerURL)
	case st.targetBusy():
		return nil, migConflict("esta instância está importando de outra; termine aquela migração antes")
	}
	if n := m.s.sessions.activeCallCount(); n > 0 {
		return nil, migConflict("%d chamada(s) em andamento — encerre antes de migrar", n)
	}
	if n := m.s.rec.liveCount(); n > 0 {
		return nil, migConflict("%d gravação(ões) em andamento — encerre as chamadas antes de migrar", n)
	}

	// 1. Persiste o bloqueio antes de mexer em qualquer coisa: se o processo
	// cair daqui pra frente, o boot mantém tudo parado até commit/abort.
	err := m.update(ctx, func(st *migrationState) {
		*st = migrationState{Role: migRoleSource, ID: id, Phase: migPhaseLocked, PeerURL: target, StartedAt: time.Now().UnixMilli()}
		st.addLog("info", "Bloqueio pedido por %s", target)
	})
	if err != nil {
		return nil, err
	}
	m.s.sessions.blocked.Store(true)

	// 2. Para gravação e contas (Disconnect, nunca Logout).
	pauseCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	if err := m.s.rec.pause(pauseCtx); err != nil {
		m.unlockSourceLocked(fmt.Sprintf("lock falhou: %v", err))
		return nil, migConflict("%v — tente de novo em instantes", err)
	}
	m.s.sessions.stopForMigration()
	m.logf("info", "Contas desconectadas (sem logout) e gravações pausadas")

	// 3. Snapshot.
	mf, err := m.buildSnapshot(ctx, id)
	if err != nil {
		m.unlockSourceLocked(fmt.Sprintf("snapshot falhou: %v", err))
		return nil, fmt.Errorf("snapshot: %w", err)
	}
	if err := m.update(ctx, func(st *migrationState) {
		st.Manifest = mf
		st.addLog("info", "Snapshot pronto: %d contas, %d bytes, %d gravação(ões) pendente(s)", len(mf.Sessions), mf.DBSize, len(mf.Files))
	}); err != nil {
		m.unlockSourceLocked(fmt.Sprintf("snapshot não persistido: %v", err))
		return nil, err
	}
	return mf, nil
}

// buildSnapshot copia o banco com VACUUM INTO (consistente com o WAL) e os
// WAVs pendentes para a pasta da migração. Segredos de gravação saem
// decifrados: B recifra com a chave dela (o arquivo já carrega as chaves
// Signal, que valem muito mais — o transporte é que precisa ser seguro).
func (m *migrator) buildSnapshot(ctx context.Context, id string) (*migrationManifest, error) {
	dir := m.sourceDir(id)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	dbFile := filepath.Join(dir, "snapshot.db")
	if _, err := m.s.db.ExecContext(ctx, `VACUUM INTO ?`, dbFile); err != nil {
		return nil, fmt.Errorf("vacuum into: %w", err)
	}

	mf := &migrationManifest{ID: id, CreatedAt: time.Now().UnixMilli()}
	sdb, err := sql.Open("sqlite", "file:"+dbFile+"?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	defer sdb.Close()
	sdb.SetMaxOpenConns(1)

	if _, err := sdb.ExecContext(ctx, `DROP TABLE IF EXISTS `+migrationExcludedTable); err != nil {
		return nil, err
	}
	if err := decryptSecretsForTransfer(ctx, sdb, m.s.rec.secrets, mf); err != nil {
		return nil, err
	}
	if err := integrityCheck(ctx, sdb); err != nil {
		return nil, err
	}
	if err := foreignKeyCheck(ctx, sdb, "main"); err != nil {
		return nil, err
	}
	if mf.Tables, err = countAllTables(ctx, sdb, "main"); err != nil {
		return nil, err
	}

	rows, err := sdb.QueryContext(ctx, `SELECT id, name, COALESCE(jid, '') FROM sessions ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var ss migSession
		if err := rows.Scan(&ss.ID, &ss.Name, &ss.JID); err != nil {
			rows.Close()
			return nil, err
		}
		mf.Sessions = append(mf.Sessions, ss)
	}
	rows.Close()

	// WAVs ainda não entregues ao B2 (o handoff termina em B).
	rows, err = sdb.QueryContext(ctx, `SELECT call_id, session_id, COALESCE(local_path, '') FROM call_recordings
		WHERE COALESCE(local_path, '') != '' AND status IN (?, ?, ?) ORDER BY rowid`,
		string(RecStatusRecording), string(RecStatusUploading), string(RecStatusFailed))
	if err != nil {
		return nil, err
	}
	type pending struct{ callID, sessionID, path string }
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.callID, &p.sessionID, &p.path); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, p)
	}
	rows.Close()

	if _, err := sdb.ExecContext(ctx, `PRAGMA journal_mode=DELETE`); err != nil {
		return nil, err
	}
	if err := sdb.Close(); err != nil {
		return nil, err
	}

	for i, p := range list {
		if _, err := os.Stat(p.path); err != nil {
			mf.Warnings = append(mf.Warnings, fmt.Sprintf("gravação %s: arquivo local ausente (%s)", p.callID, p.path))
			continue
		}
		name := fmt.Sprintf("rec-%d", i)
		dst := filepath.Join(dir, name)
		if err := linkOrCopy(p.path, dst); err != nil {
			return nil, fmt.Errorf("gravação %s: %w", p.callID, err)
		}
		sum, size, err := fileSha256(dst)
		if err != nil {
			return nil, err
		}
		mf.Files = append(mf.Files, migFile{Name: name, CallID: p.callID, SessionID: p.sessionID, Size: size, Sha256: sum})
	}

	sum, size, err := fileSha256(dbFile)
	if err != nil {
		return nil, err
	}
	mf.DBSha256, mf.DBSize = sum, size
	return mf, nil
}

func decryptSecretsForTransfer(ctx context.Context, db *sql.DB, box *secretBox, mf *migrationManifest) error {
	if box == nil {
		return nil
	}
	return rewriteSecrets(ctx, db, func(sessionID, col, v string) (string, error) {
		if !strings.HasPrefix(v, encPrefix) {
			return v, nil
		}
		plain, err := box.Decrypt(v)
		if err != nil {
			// Já era ilegível em A (chave trocada/ausente): segue como está.
			mf.Warnings = append(mf.Warnings, fmt.Sprintf("conta %s: %s não pôde ser decifrado em A (%v); reconfigure em B", sessionID, col, err))
			return v, nil
		}
		return plain, nil
	})
}

// rewriteSecrets aplica fn aos segredos cifrados de recording_config.
func rewriteSecrets(ctx context.Context, db *sql.DB, fn func(sessionID, col, v string) (string, error)) error {
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='recording_config'`).Scan(&exists); err != nil || exists == 0 {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT session_id, COALESCE(b2_app_key_enc, ''), COALESCE(webhook_secret_enc, '') FROM recording_config`)
	if err != nil {
		return err
	}
	type rec struct{ id, key, secret string }
	var list []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.id, &r.key, &r.secret); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()
	for _, r := range list {
		key, err := fn(r.id, "b2_app_key", r.key)
		if err != nil {
			return err
		}
		secret, err := fn(r.id, "webhook_secret", r.secret)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `UPDATE recording_config SET b2_app_key_enc=?, webhook_secret_enc=? WHERE session_id=?`, key, secret, r.id); err != nil {
			return err
		}
	}
	return nil
}

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// unlockSourceLocked devolve A ao normal: estado, contas e gravação. Chamar
// com opMu travado.
func (m *migrator) unlockSourceLocked(reason string) {
	ctx := context.WithoutCancel(m.s.sessions.appCtx)
	st, _, _ := m.snapshot()
	err := m.update(ctx, func(st *migrationState) {
		st.Phase = migPhaseUnlocked
		st.addLog("warn", "Desbloqueada: %s", reason)
	})
	if err != nil {
		// Sem persistir, um restart voltaria bloqueado; seguro, só avisa.
		m.s.log.Error("migration: unlock not persisted", "err", err)
	}
	_ = os.RemoveAll(m.sourceDir(st.ID))
	m.s.sessions.blocked.Store(false)
	m.s.sessions.reconnectAll()
	m.s.rec.resume()
}

// commitSource: B confirmou. Apaga as chaves Signal locais (sem logout — o
// device continua vivo em B) e deixa A como tombstone.
func (m *migrator) commitSource(ctx context.Context, id, dbSha string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	st, _, _ := m.snapshot()
	if st.Role == migRoleSource && st.Phase == migPhaseCommitted && st.ID == id {
		return nil
	}
	if st.Role != migRoleSource || st.ID != id {
		return migConflict("nenhuma migração %s nesta origem", id)
	}
	if st.Phase != migPhaseLocked {
		return migConflict("migração %s não está bloqueada aqui (fase %q) — A pode ter voltado a operar", id, st.Phase)
	}
	if st.Manifest == nil || st.Manifest.DBSha256 != dbSha {
		return migConflict("sha256 do banco não confere com o snapshot desta migração")
	}

	if err := m.wipeLocalKeys(ctx); err != nil {
		return fmt.Errorf("apagar chaves locais: %w", err)
	}
	for _, s := range m.s.sessions.all() {
		s.replaceClient(whatsmeow.NewClient(m.s.sessions.container.NewDevice(), m.s.sessions.waLogger))
		s.setAuth(AuthSnapshot{State: "logged_out", Paired: false})
	}
	err := m.update(ctx, func(st *migrationState) {
		st.Phase = migPhaseCommitted
		st.addLog("info", "Migração confirmada por %s. Chaves locais apagadas; instância desativada.", st.PeerURL)
	})
	if err != nil {
		return err
	}
	_ = os.RemoveAll(m.sourceDir(id))
	return nil
}

// wipeLocalKeys apaga todo o estado do whatsmeow (menos a versão do schema) e
// os JIDs das contas, numa transação.
func (m *migrator) wipeLocalKeys(ctx context.Context) error {
	conn, err := m.s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return err
	}
	tables, err := listTables(ctx, tx, "main")
	if err != nil {
		return err
	}
	for name := range tables {
		if !strings.HasPrefix(name, "whatsmeow_") || name == "whatsmeow_version" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.`+quoteIdent(name)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET jid=NULL`); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *migrator) abortSource(id, reason string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	st, _, _ := m.snapshot()
	if st.Role != migRoleSource || st.ID != id || st.Phase == migPhaseUnlocked {
		return nil // nunca bloqueou (ou já desbloqueou): nada a fazer
	}
	if st.Phase == migPhaseCommitted {
		return migConflict("migração %s já foi confirmada; as chaves desta instância foram apagadas", id)
	}
	m.unlockSourceLocked(reason)
	return nil
}

// ---- handlers (A) ----

func writeMigrationError(w http.ResponseWriter, err error) {
	var c *migConflictError
	if errors.As(err, &c) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": c.msg})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func (s *server) handleMigrationLock(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MigrationID string `json:"migrationId"`
		Target      string `json:"target"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !validMigrationID(body.MigrationID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "migrationId inválido"})
		return
	}
	// Desacoplado do request: se B cair no meio, o snapshot termina e B pede
	// de novo (idempotente) ou manda abort.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Minute)
	defer cancel()
	mf, err := s.mig.lockSource(ctx, body.MigrationID, body.Target)
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mf)
}

func (s *server) handleMigrationFile(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("mid"), r.PathValue("name")
	st, _, _ := s.mig.snapshot()
	if st.Role != migRoleSource || st.Phase != migPhaseLocked || st.ID != id || st.Manifest == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "nenhuma migração bloqueada com esse id"})
		return
	}
	path := ""
	if name == "db" {
		path = filepath.Join(s.mig.sourceDir(id), "snapshot.db")
	} else {
		for _, f := range st.Manifest.Files {
			if f.Name == name {
				path = filepath.Join(s.mig.sourceDir(id), f.Name)
			}
		}
	}
	if path == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "arquivo fora do manifest"})
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	// ServeContent atende Range: B retoma download interrompido.
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

func (s *server) handleMigrationCommit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DBSha256 string `json:"dbSha256"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if err := s.mig.commitSource(context.WithoutCancel(r.Context()), r.PathValue("mid"), body.DBSha256); err != nil {
		writeMigrationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "committed"})
}

func (s *server) handleMigrationAbort(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "abort pedido via API"
	}
	if err := s.mig.abortSource(r.PathValue("mid"), reason); err != nil {
		writeMigrationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlocked"})
}
