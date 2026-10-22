package main

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// Lado B (destino) da migração. Ver o diagrama em migration.go.

// ---- cliente da origem ----

type migSourceClient struct {
	base  string
	key   string
	httpc *http.Client
}

type migHTTPError struct {
	status int
	msg    string
}

func (e *migHTTPError) Error() string { return fmt.Sprintf("origem respondeu %d: %s", e.status, e.msg) }

// permanent: erro do cliente que repetir não resolve (408/429 são transitórios).
func (e *migHTTPError) permanent() bool {
	return e.status/100 == 4 && e.status != http.StatusRequestTimeout && e.status != http.StatusTooManyRequests
}

func (c *migSourceClient) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func readHTTPError(res *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &e)
	return &migHTTPError{status: res.StatusCode, msg: cmp.Or(e.Error, strings.TrimSpace(string(data)), res.Status)}
}

func (c *migSourceClient) call(ctx context.Context, timeout time.Duration, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	res, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return readHTTPError(res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *migSourceClient) lock(ctx context.Context, id, target string) (*migrationManifest, error) {
	var mf migrationManifest
	err := c.call(ctx, 12*time.Minute, http.MethodPost, "/api/migration/source/lock",
		map[string]string{"migrationId": id, "target": target}, &mf)
	if err != nil {
		return nil, err
	}
	return &mf, nil
}

func (c *migSourceClient) commit(ctx context.Context, id, sha string) error {
	return c.call(ctx, time.Minute, http.MethodPost, "/api/migration/source/"+id+"/commit",
		map[string]string{"dbSha256": sha}, nil)
}

func (c *migSourceClient) abort(ctx context.Context, id, reason string) error {
	return c.call(ctx, time.Minute, http.MethodPost, "/api/migration/source/"+id+"/abort",
		map[string]string{"reason": reason}, nil)
}

// download baixa um arquivo do manifest, retomando com Range quando a conexão
// cai, e só aceita o resultado com tamanho e sha256 exatos.
func (c *migSourceClient) download(ctx context.Context, id, name, dest string, size int64, sha string, retryBase time.Duration, onProgress func(int64)) error {
	const maxAttempts = 6
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if err := sleepCtx(ctx, backoff(retryBase, attempt, 30*time.Second)); err != nil {
				return err
			}
		}
		err := c.downloadOnce(ctx, id, name, dest, size, onProgress)
		if err == nil {
			sum, n, herr := fileSha256(dest)
			if herr == nil && n == size && sum == sha {
				return nil
			}
			if herr == nil {
				herr = fmt.Errorf("conferência falhou: %d de %d bytes, sha256 %.12s… ≠ %.12s…", n, size, sum, sha)
			}
			_ = os.Remove(dest) // corrompido: recomeça do zero
			lastErr = herr
			continue
		}
		var he *migHTTPError
		if errors.As(err, &he) && he.permanent() && he.status != http.StatusRequestedRangeNotSatisfiable {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("download de %s: %w", name, lastErr)
}

func (c *migSourceClient) downloadOnce(ctx context.Context, id, name, dest string, size int64, onProgress func(int64)) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	have := fi.Size()
	if have > size {
		if err := f.Truncate(0); err != nil {
			return err
		}
		have = 0
	}
	if have == size {
		return nil
	}

	req, err := c.newRequest(ctx, http.MethodGet, "/api/migration/source/"+id+"/files/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	res, err := c.httpc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		if err := f.Truncate(0); err != nil {
			return err
		}
		have = 0
	case http.StatusRequestedRangeNotSatisfiable:
		_ = f.Truncate(0)
		return readHTTPError(res)
	default:
		return readHTTPError(res)
	}
	if _, err := f.Seek(have, io.SeekStart); err != nil {
		return err
	}
	pw := &progressWriter{w: f, done: have, fn: onProgress}
	if _, err := io.Copy(pw, res.Body); err != nil {
		return err
	}
	return f.Sync()
}

type progressWriter struct {
	w    io.Writer
	done int64
	fn   func(int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.fn != nil {
		p.fn(p.done)
	}
	return n, err
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func backoff(base time.Duration, attempt int, max time.Duration) time.Duration {
	d := base
	for i := 0; i < attempt && d < max; i++ {
		d *= 2
	}
	return min(d, max)
}

// ---- job de import ----

func (m *migrator) targetDir(id string) string {
	return filepath.Join(filepath.Dir(m.s.dbPath), "migration-in-"+id)
}

func (m *migrator) startImport(base, key, self string) (string, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	st, _, running := m.snapshot()
	switch {
	case running || st.targetBusy():
		return "", migConflict("já existe uma migração em andamento nesta instância")
	case st.Role == migRoleSource && st.blocksSessions():
		return "", migConflict("esta instância é origem de uma migração (%s)", st.Phase)
	}
	if n := m.s.sessions.pairedCount(); n > 0 {
		return "", migConflict("esta instância já tem %d conta(s) pareada(s). O clone substitui o banco inteiro: use uma instância nova (ou exclua as contas antes)", n)
	}
	if n := m.s.sessions.activeCallCount(); n > 0 {
		return "", migConflict("%d chamada(s) em andamento nesta instância", n)
	}

	id := newMigrationID()
	ctx := m.s.sessions.appCtx
	m.setPeerKey(key)
	err := m.update(ctx, func(st *migrationState) {
		*st = migrationState{Role: migRoleTarget, ID: id, Phase: migPhasePreparing, PeerURL: base, StartedAt: time.Now().UnixMilli()}
		st.addLog("info", "Migração iniciada. Origem: %s", base)
	})
	if err != nil {
		m.setPeerKey("")
		return "", err
	}
	m.s.sessions.blocked.Store(true)
	m.setRunning(true)
	src := &migSourceClient{base: base, key: key, httpc: m.httpc}
	go func() {
		defer m.setRunning(false)
		m.runImport(ctx, src, id, self)
	}()
	return id, nil
}

func (m *migrator) runImport(ctx context.Context, src *migSourceClient, id, self string) {
	staging := m.targetDir(id)
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		m.failBeforeImport(ctx, src, id, err)
		return
	}
	mf, err := m.fetchAndVerify(ctx, src, id, self, staging)
	if err != nil {
		m.failBeforeImport(ctx, src, id, err)
		return
	}
	if err := m.importClone(ctx, mf, staging); err != nil {
		m.failBeforeImport(ctx, src, id, err)
		return
	}
	m.afterImport(ctx, src, id, mf)
}

// fetchAndVerify: lock em A, download conferido e preparo do snapshot. Nada
// aqui toca o banco de B.
func (m *migrator) fetchAndVerify(ctx context.Context, src *migSourceClient, id, self, staging string) (*migrationManifest, error) {
	m.setProgress("Bloqueando a origem", 0, 0)
	m.logf("info", "Pedindo bloqueio da origem (desconecta as contas lá, sem logout)")
	mf, err := src.lock(ctx, id, self)
	if err != nil {
		return nil, fmt.Errorf("bloqueio da origem: %w", err)
	}
	if mf.ID != id || mf.DBSize <= 0 || len(mf.DBSha256) != 64 {
		return nil, errors.New("manifest da origem inválido")
	}
	if err := m.update(ctx, func(st *migrationState) {
		st.Manifest = mf
		st.addLog("info", "Origem bloqueada: %d conta(s), banco de %d bytes, %d gravação(ões) pendente(s)", len(mf.Sessions), mf.DBSize, len(mf.Files))
		for _, w := range mf.Warnings {
			st.addLog("warn", "Origem: %s", w)
		}
	}); err != nil {
		return nil, err
	}

	total := mf.DBSize
	for _, f := range mf.Files {
		total += f.Size
	}
	var base int64
	progress := func(label string) func(int64) {
		return func(n int64) { m.setProgress(label, base+n, total) }
	}
	dbFile := filepath.Join(staging, "snapshot.db")
	if err := src.download(ctx, id, "db", dbFile, mf.DBSize, mf.DBSha256, m.retryBase, progress("Baixando o banco")); err != nil {
		return nil, err
	}
	base += mf.DBSize
	for i, f := range mf.Files {
		if _, err := m.recordingDest(f); err != nil {
			return nil, err
		}
		label := fmt.Sprintf("Baixando gravações (%d/%d)", i+1, len(mf.Files))
		if err := src.download(ctx, id, f.Name, filepath.Join(staging, f.Name), f.Size, f.Sha256, m.retryBase, progress(label)); err != nil {
			return nil, err
		}
		base += f.Size
	}
	m.logf("info", "Download conferido (sha256 de %d arquivo(s))", 1+len(mf.Files))

	m.setProgress("Verificando o snapshot", 0, 0)
	if err := m.prepareSnapshot(ctx, dbFile, mf); err != nil {
		return nil, fmt.Errorf("verificação do snapshot: %w", err)
	}
	m.logf("info", "Snapshot íntegro, contagens conferem com a origem, schema atualizado para esta versão")
	return mf, nil
}

func validPathPart(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`+"\x00")
}

func (m *migrator) recordingDest(f migFile) (string, error) {
	if !validPathPart(f.SessionID) || !validPathPart(f.CallID) || !validPathPart(f.Name) {
		return "", fmt.Errorf("gravação com nome inválido no manifest: %q/%q", f.SessionID, f.CallID)
	}
	return filepath.Join(m.s.rec.dir, f.SessionID, "migrated", f.CallID+".wav"), nil
}

// prepareSnapshot confere o snapshot e o deixa pronto para entrar em B:
// integridade, contagens = manifest, schema na versão de B, segredos
// recifrados com a chave de B, caminhos das gravações em B.
func (m *migrator) prepareSnapshot(ctx context.Context, path string, mf *migrationManifest) error {
	sdb, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		return err
	}
	defer sdb.Close()
	sdb.SetMaxOpenConns(1)

	if err := integrityCheck(ctx, sdb); err != nil {
		return err
	}
	if err := foreignKeyCheck(ctx, sdb, "main"); err != nil {
		return err
	}
	counts, err := countAllTables(ctx, sdb, "main")
	if err != nil {
		return err
	}
	if diff := diffCounts(mf.Tables, counts); diff != "" {
		return fmt.Errorf("contagem de linhas não confere com a origem: %s", diff)
	}

	container := sqlstore.NewWithDB(sdb, "sqlite3", waLog.Noop)
	if err := container.Upgrade(ctx); err != nil {
		return fmt.Errorf("schema do WhatsApp da origem incompatível (atualize esta instância para a mesma versão da origem): %w", err)
	}
	if _, err := newSessionStore(ctx, sdb); err != nil {
		return err
	}
	if _, err := newRecordingStore(ctx, sdb); err != nil {
		return err
	}
	for _, ss := range mf.Sessions {
		if ss.JID == "" {
			continue
		}
		jid, err := types.ParseJID(ss.JID)
		if err != nil {
			continue
		}
		if dev, err := container.GetDevice(ctx, jid); err != nil || dev == nil {
			m.logf("warn", "Conta %q (%s) não tem chaves no snapshot; vai chegar desconectada (já estava assim na origem)", ss.Name, ss.JID)
		}
	}

	box := m.s.rec.secrets
	if err := rewriteSecrets(ctx, sdb, func(_, _, v string) (string, error) {
		if v == "" || strings.HasPrefix(v, encPrefix) || box == nil {
			return v, nil
		}
		return box.Encrypt(v)
	}); err != nil {
		return fmt.Errorf("recifrar segredos de gravação: %w", err)
	}

	for _, f := range mf.Files {
		dest, err := m.recordingDest(f)
		if err != nil {
			return err
		}
		if _, err := sdb.ExecContext(ctx, `UPDATE call_recordings SET local_path=? WHERE call_id=?`, dest, f.CallID); err != nil {
			return err
		}
	}

	if err := integrityCheck(ctx, sdb); err != nil {
		return err
	}
	if err := foreignKeyCheck(ctx, sdb, "main"); err != nil {
		return err
	}
	if _, err := sdb.ExecContext(ctx, `PRAGMA journal_mode=DELETE`); err != nil {
		return err
	}
	return sdb.Close()
}

func diffCounts(want, got map[string]int64) string {
	var diffs []string
	for name, n := range want {
		if g, ok := got[name]; !ok {
			diffs = append(diffs, name+" ausente")
		} else if g != n {
			diffs = append(diffs, fmt.Sprintf("%s %d≠%d", name, g, n))
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			diffs = append(diffs, name+" a mais")
		}
	}
	slices.Sort(diffs)
	return strings.Join(diffs, ", ")
}

// importClone troca o conteúdo de B pelo snapshot. Em erro, B volta a como
// estava (a transação não comitou). Em sucesso, B está com as chaves de A.
func (m *migrator) importClone(ctx context.Context, mf *migrationManifest, staging string) error {
	m.setProgress("Importando", 0, 0)
	pauseCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()
	if err := m.s.rec.pause(pauseCtx); err != nil {
		m.s.rec.resume()
		return err
	}
	m.s.sessions.dropAll()
	restoreB := func() {
		if err := m.s.sessions.reload(ctx, sqlstore.NewWithDB(m.s.db, "sqlite3", waLog.Noop)); err != nil {
			m.logf("error", "Recarregar as contas desta instância: %v", err)
		}
		m.s.rec.resume()
	}

	// Cópia de B como estava: o rollback (clone que não conecta) volta para ela.
	if _, err := m.s.db.ExecContext(ctx, `VACUUM INTO ?`, filepath.Join(staging, "before.db")); err != nil {
		restoreB()
		return fmt.Errorf("backup desta instância: %w", err)
	}

	var placed []string
	for _, f := range mf.Files {
		dest, _ := m.recordingDest(f)
		err := os.MkdirAll(filepath.Dir(dest), 0o755)
		if err == nil {
			err = moveFile(filepath.Join(staging, f.Name), dest)
		}
		if err != nil {
			removeFiles(placed)
			restoreB()
			return fmt.Errorf("gravação %s: %w", f.CallID, err)
		}
		placed = append(placed, dest)
	}

	_, err := m.cloneTables(ctx, filepath.Join(staging, "snapshot.db"), func(tx *sql.Tx, counts map[string]int64) error {
		// Fase imported na mesma transação dos dados: depois de um crash, o
		// boot sabe exatamente se B já tem as chaves ou não.
		return m.updateWith(ctx, tx, func(st *migrationState) {
			st.Phase = migPhaseImported
			st.Report = &migrationReport{Tables: counts, Files: len(mf.Files)}
			st.addLog("info", "Dados importados numa transação e conferidos tabela a tabela (linhas + sha256 do conteúdo)")
		})
	})
	if err != nil {
		removeFiles(placed)
		restoreB()
		return fmt.Errorf("import: %w", err)
	}

	// Daqui em diante B tem as chaves.
	m.s.sessions.blocked.Store(false)
	if err := m.s.sessions.reload(ctx, sqlstore.NewWithDB(m.s.db, "sqlite3", waLog.Noop)); err != nil {
		m.logf("error", "Carregar as contas importadas: %v", err)
	}
	m.s.rec.resume()
	return nil
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func removeFiles(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// cloneTables substitui todas as tabelas de main pelas de srcPath numa só
// transação, confere linhas e sha256 do conteúdo de cada tabela e as FKs, e
// chama finalize antes do commit. Segura a única conexão do pool o tempo todo:
// nada mais escreve no banco enquanto isso.
func (m *migrator) cloneTables(ctx context.Context, srcPath string, finalize func(tx *sql.Tx, counts map[string]int64) error) (map[string]int64, error) {
	conn, err := m.s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS mig_src`, srcPath); err != nil {
		return nil, fmt.Errorf("attach: %w", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), `DETACH DATABASE mig_src`); err != nil {
			// Conexão suja não volta para o pool.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()

	counts, err := m.cloneTx(ctx, conn, finalize)
	if err != nil {
		return nil, err
	}
	return counts, nil
}

func (m *migrator) cloneTx(ctx context.Context, conn *sql.Conn, finalize func(tx *sql.Tx, counts map[string]int64) error) (map[string]int64, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
		return nil, err
	}

	mainT, err := listTables(ctx, tx, "main")
	if err != nil {
		return nil, err
	}
	srcT, err := listTables(ctx, tx, "mig_src")
	if err != nil {
		return nil, err
	}
	delete(mainT, migrationExcludedTable)
	delete(srcT, migrationExcludedTable)

	for name, st := range srcT {
		mt, ok := mainT[name]
		if !ok {
			return nil, fmt.Errorf("tabela %s da origem não existe aqui (esta instância é mais antiga?)", name)
		}
		for _, c := range st.cols {
			if !slices.Contains(mt.cols, c) {
				return nil, fmt.Errorf("coluna %s.%s da origem não existe aqui (esta instância é mais antiga?)", name, c)
			}
		}
	}

	for name := range mainT {
		if _, err := tx.ExecContext(ctx, `DELETE FROM main.`+quoteIdent(name)); err != nil {
			return nil, fmt.Errorf("limpar %s: %w", name, err)
		}
	}
	for name, st := range srcT {
		cols := make([]string, len(st.cols))
		for i, c := range st.cols {
			cols[i] = quoteIdent(c)
		}
		list := strings.Join(cols, ", ")
		q := `INSERT INTO main.` + quoteIdent(name) + ` (` + list + `) SELECT ` + list + ` FROM mig_src.` + quoteIdent(name)
		if st.hasRowID {
			q += ` ORDER BY rowid` // preserva a ordem (ex.: lista de contas)
		}
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return nil, fmt.Errorf("copiar %s: %w", name, err)
		}
	}

	counts := make(map[string]int64, len(mainT))
	for name := range mainT {
		nm, err := countRows(ctx, tx, "main", name)
		if err != nil {
			return nil, err
		}
		counts[name] = nm
		st, ok := srcT[name]
		if !ok {
			if nm != 0 {
				return nil, fmt.Errorf("conferência: %s deveria estar vazia, tem %d", name, nm)
			}
			continue
		}
		ns, err := countRows(ctx, tx, "mig_src", name)
		if err != nil {
			return nil, err
		}
		if nm != ns {
			return nil, fmt.Errorf("conferência: %s tem %d linhas, origem %d", name, nm, ns)
		}
		dm, err := tableDigest(ctx, tx, "main", name, st.cols)
		if err != nil {
			return nil, err
		}
		ds, err := tableDigest(ctx, tx, "mig_src", name, st.cols)
		if err != nil {
			return nil, err
		}
		if dm != ds {
			return nil, fmt.Errorf("conferência: conteúdo de %s difere da origem", name)
		}
	}
	if err := foreignKeyCheck(ctx, tx, "main"); err != nil {
		return nil, err
	}
	if finalize != nil {
		if err := finalize(tx, counts); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return counts, nil
}

// afterImport: espera as contas conectarem e decide commit ou rollback.
func (m *migrator) afterImport(ctx context.Context, src *migSourceClient, id string, mf *migrationManifest) {
	_ = m.update(ctx, func(st *migrationState) {
		st.Phase = migPhaseConnecting
		st.addLog("info", "Conectando as contas ao WhatsApp")
	})
	m.setProgress("Conectando as contas", 0, 0)
	connected, paired, missing := m.waitConnected(ctx, m.connectWait)
	_ = m.update(ctx, func(st *migrationState) {
		if st.Report == nil {
			st.Report = &migrationReport{}
		}
		st.Report.PairedSessions, st.Report.ConnectedOnB, st.Report.NotConnected = paired, connected, missing
	})

	if paired > 0 && connected == 0 {
		m.rollback(ctx, src, id, fmt.Sprintf("nenhuma das %d conta(s) conectou em %s", paired, m.connectWait))
		return
	}
	if len(missing) > 0 {
		m.logf("warn", "%d de %d conta(s) ainda não conectaram aqui (%s); seguem tentando", len(missing), paired, strings.Join(missing, ", "))
	}
	m.logf("info", "%d de %d conta(s) conectadas aqui", connected, paired)
	m.commitLoop(ctx, src, id, mf.DBSha256)
}

func (m *migrator) waitSessionsConnected(ctx context.Context, timeout time.Duration) (connected, paired int, missing []string) {
	deadline := time.Now().Add(timeout)
	for {
		_, ever, p, miss := m.s.sessions.connectedCount()
		if ever == p || time.Now().After(deadline) || sleepCtx(ctx, time.Second) != nil {
			return ever, p, miss
		}
	}
}

// commitLoop confirma para A até conseguir. Sem resposta, A segue bloqueada e
// B segue operando: nada roda em dobro, só falta A apagar as chaves dela.
func (m *migrator) commitLoop(ctx context.Context, src *migSourceClient, id, sha string) {
	_ = m.update(ctx, func(st *migrationState) {
		st.Phase = migPhaseCommitting
		st.addLog("info", "Confirmando para a origem")
	})
	m.setProgress("Confirmando para a origem", 0, 0)
	for attempt := 0; ; attempt++ {
		err := src.commit(ctx, id, sha)
		if err == nil {
			break
		}
		var he *migHTTPError
		if errors.As(err, &he) && he.status == http.StatusConflict {
			// A não está mais bloqueada para esta migração (desbloqueio manual ou
			// banco trocado). Se A voltou a operar, as duas têm as mesmas chaves:
			// o StreamReplaced derruba uma delas; o admin escolhe qual fica.
			m.setPeerKey("")
			_ = m.update(ctx, func(st *migrationState) {
				st.Phase = migPhaseFailed
				st.Error = fmt.Sprintf("A origem recusou a confirmação: %s. Se a origem voltou a operar, as duas instâncias têm as mesmas chaves — mantenha só uma conectada.", he.msg)
				st.addLog("error", "%s", st.Error)
			})
			m.setProgress("", 0, 0)
			return
		}
		_ = m.update(ctx, func(st *migrationState) {
			st.Error = fmt.Sprintf("confirmação pendente (tentativa %d): %v — a origem segue bloqueada, nada roda em dobro", attempt+1, err)
			st.addLog("warn", "%s", st.Error)
		})
		if sleepCtx(ctx, backoff(m.retryBase, attempt, 5*time.Minute)) != nil {
			return // desligando: o boot retoma o commit
		}
	}
	m.setPeerKey("")
	_ = m.update(ctx, func(st *migrationState) {
		st.Phase = migPhaseDone
		st.Error = ""
		if st.Report == nil {
			st.Report = &migrationReport{}
		}
		st.Report.SourceCommitted = true
		st.Report.CommittedAt = time.Now().UnixMilli()
		st.addLog("info", "Origem confirmou e apagou as chaves dela. Migração concluída.")
	})
	_ = os.RemoveAll(m.targetDir(id))
	m.setProgress("", 0, 0)
}

// rollback: o clone não conectou. Apaga o clone de B (volta ao backup) antes
// de liberar A — as chaves nunca ficam ativas nos dois lados.
func (m *migrator) rollback(ctx context.Context, src *migSourceClient, id, reason string) {
	_ = m.update(ctx, func(st *migrationState) {
		st.Phase = migPhaseRollingBack
		st.Error = reason
		st.addLog("warn", "Desfazendo o clone: %s", reason)
	})
	m.setProgress("Desfazendo o clone", 0, 0)
	m.s.sessions.blocked.Store(true)
	pauseCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	_ = m.s.rec.pause(pauseCtx)
	cancel()
	m.s.sessions.stopForMigration()

	if _, ever, _, _ := m.s.sessions.connectedCount(); ever > 0 {
		// Conectou no último instante: as chaves já foram usadas aqui, voltar
		// atrás deixaria A com estado velho. Segue com o clone.
		m.logf("warn", "Uma conta conectou durante o rollback; mantendo o clone aqui")
		m.s.sessions.blocked.Store(false)
		m.s.sessions.reconnectAll()
		m.s.rec.resume()
		st, _, _ := m.snapshot()
		m.commitLoop(ctx, src, id, st.Manifest.DBSha256)
		return
	}

	if err := m.restoreBackup(ctx, id); err != nil {
		// Fica em rolling_back (bloqueada): o boot tenta de novo. A segue
		// bloqueada. Nada roda.
		_ = m.update(ctx, func(st *migrationState) {
			st.Error = fmt.Sprintf("rollback falhou: %v — esta instância está parada com o clone e a origem bloqueada; reinicie para tentar de novo", err)
			st.addLog("error", "%s", st.Error)
		})
		return
	}
	st, _, _ := m.snapshot()
	if st.Manifest != nil {
		for _, f := range st.Manifest.Files {
			if dest, err := m.recordingDest(f); err == nil {
				_ = os.Remove(dest)
			}
		}
	}
	if err := m.s.sessions.reload(ctx, sqlstore.NewWithDB(m.s.db, "sqlite3", waLog.Noop)); err != nil {
		m.logf("error", "Recarregar as contas desta instância: %v", err)
	}
	m.s.rec.resume()
	m.logf("info", "Clone apagado; esta instância voltou a como estava")
	m.finishAborted(ctx, src, id, "Nenhuma conta conectou aqui ("+reason+"). Clone desfeito nesta instância")
}

// restoreBackup volta B ao backup tirado antes do import (ou esvazia, se o
// backup sumiu — B não tinha contas pareadas).
func (m *migrator) restoreBackup(ctx context.Context, id string) error {
	m.s.sessions.dropAll()
	backup := filepath.Join(m.targetDir(id), "before.db")
	keep := func(tx *sql.Tx, _ map[string]int64) error {
		return m.updateWith(ctx, tx, func(st *migrationState) {
			st.addLog("info", "Backup desta instância restaurado")
		})
	}
	if _, err := os.Stat(backup); err == nil {
		_, err := m.cloneTables(ctx, backup, keep)
		return err
	}
	// Arquivo vazio = banco SQLite sem tabelas: o clone esvazia tudo.
	empty := filepath.Join(m.targetDir(id), "empty.db")
	if err := os.MkdirAll(m.targetDir(id), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		return err
	}
	_, err := m.cloneTables(ctx, empty, keep)
	return err
}

// failBeforeImport: falha antes de B ter as chaves. B está intacta; libera A.
func (m *migrator) failBeforeImport(ctx context.Context, src *migSourceClient, id string, cause error) {
	m.logf("error", "%v", cause)
	m.s.sessions.blocked.Store(false)
	if m.s.rec.paused.Load() {
		m.s.rec.resume() // boot em preparing pausou a gravação
	}
	m.finishAborted(ctx, src, id, fmt.Sprintf("Falhou antes de importar: %v. Esta instância não foi alterada", cause))
}

// finishAborted manda abort para A (com retry) e fecha o job.
func (m *migrator) finishAborted(ctx context.Context, src *migSourceClient, id, what string) {
	m.setProgress("Desbloqueando a origem", 0, 0)
	err := m.abortWithRetry(ctx, src, id, what)
	_ = os.RemoveAll(m.targetDir(id))
	m.setPeerKey("")
	_ = m.update(ctx, func(st *migrationState) {
		if err == nil {
			st.Phase = migPhaseAborted
			st.Error = what + ". A origem foi desbloqueada e voltou a operar."
			st.addLog("warn", "%s", st.Error)
			return
		}
		st.Phase = migPhaseFailed
		st.Error = fmt.Sprintf("%s. A origem NÃO confirmou o desbloqueio (%v): desbloqueie pelo painel dela — é seguro, esta instância não tem as chaves.", what, err)
		st.addLog("error", "%s", st.Error)
	})
	m.setProgress("", 0, 0)
}

func (m *migrator) abortWithRetry(ctx context.Context, src *migSourceClient, id, reason string) error {
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			if sleepCtx(ctx, backoff(m.retryBase, attempt, 30*time.Second)) != nil {
				return ctx.Err()
			}
		}
		if err = src.abort(ctx, id, reason); err == nil {
			return nil
		}
		var he *migHTTPError
		if errors.As(err, &he) && he.permanent() {
			return err
		}
	}
	return err
}

// ---- boot ----

// boot carrega o estado antes de Restore/rec.start: com bloqueio ativo nada
// conecta nem sobe gravação.
func (m *migrator) boot(ctx context.Context) error {
	st, key, err := loadMigrationState(ctx, m.s.db)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.state, m.peerKey = st, key
	m.mu.Unlock()
	if st.blocksSessions() {
		m.s.sessions.blocked.Store(true)
		m.s.rec.paused.Store(true)
		m.s.log.Warn("migration lock active: sessions stay disconnected", "role", st.Role, "phase", st.Phase, "peer", st.PeerURL)
	}
	return nil
}

// resumeJobs retoma, depois do Restore, o job de destino interrompido por restart.
func (m *migrator) resumeJobs(ctx context.Context) {
	st, _, _ := m.snapshot()
	if st.Role != migRoleTarget {
		return
	}
	m.mu.Lock()
	src := &migSourceClient{base: st.PeerURL, key: m.peerKey, httpc: m.httpc}
	m.mu.Unlock()

	var job func()
	switch st.Phase {
	case migPhasePreparing:
		// Caiu antes do import (a fase imported é gravada junto com os dados).
		job = func() { m.failBeforeImport(ctx, src, st.ID, errors.New("processo reiniciado durante o preparo")) }
	case migPhaseImported, migPhaseConnecting, migPhaseCommitting:
		// B já tem as chaves e pode ter conectado: só resta confirmar.
		job = func() {
			m.logf("warn", "Processo reiniciado; retomando a confirmação para a origem")
			m.commitLoop(ctx, src, st.ID, st.Manifest.DBSha256)
		}
	case migPhaseRollingBack:
		job = func() { m.rollback(ctx, src, st.ID, cmp.Or(st.Error, "retomando rollback após restart")) }
	default:
		return
	}
	m.setRunning(true)
	go func() {
		defer m.setRunning(false)
		job()
	}()
}

// ---- handlers (B) ----

func normalizeSourceURL(raw string, allowInsecure bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", errors.New("URL da origem inválida (ex.: https://calls.exemplo.com)")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !allowInsecure {
			return "", errors.New("a origem usa http: as chaves das contas iriam em texto puro. Use https ou marque 'permitir http' (só em rede privada)")
		}
	default:
		return "", errors.New("URL da origem precisa começar com https://")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.Path, "/"), nil
}

func requestBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "http"
		if r.TLS != nil {
			proto = "https"
		}
	}
	return proto + "://" + cmp.Or(r.Header.Get("X-Forwarded-Host"), r.Host)
}

func (s *server) handleMigrationImport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SourceURL     string `json:"sourceUrl"`
		SourceAPIKey  string `json:"sourceApiKey"`
		AllowInsecure bool   `json:"allowInsecure"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "corpo inválido"})
		return
	}
	base, err := normalizeSourceURL(body.SourceURL, body.AllowInsecure)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	key := strings.TrimSpace(body.SourceAPIKey)
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "informe a API_KEY da origem"})
		return
	}
	self := requestBaseURL(r)
	if strings.EqualFold(base, self) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "origem e destino são a mesma instância"})
		return
	}
	id, err := s.mig.startImport(base, key, self)
	if err != nil {
		writeMigrationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"migrationId": id})
}
