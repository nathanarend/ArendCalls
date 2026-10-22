package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// Migração entre servidores (clone completo do banco, chaves Signal incluídas).
//
// O destino (B) puxa da origem (A) com a API key de A:
//
//	B → A  lock    A desconecta tudo (sem logout), bloqueia e gera o snapshot
//	B ← A  files   banco + WAVs pendentes, cada um conferido por sha256
//	B      import  verifica, copia numa transação, confere tabela a tabela
//	B      connect espera as contas conectarem
//	B → A  commit  A apaga as chaves locais e vira tombstone   (deu certo)
//	B → A  abort   A desbloqueia e reconecta                   (B não usou as chaves)
//
// Regra de ouro: as mesmas chaves nunca rodam em dois lugares. Na dúvida o
// sistema para nos dois lados (A bloqueada) em vez de arriscar rodar em dobro;
// A só sai do bloqueio por commit, abort de B ou desbloqueio manual no painel.

const (
	migRoleSource = "source"
	migRoleTarget = "target"

	// origem (A)
	migPhaseLocked    = "locked"
	migPhaseCommitted = "committed"

	// destino (B)
	migPhasePreparing   = "preparing"    // lock + download + verificação; B intacta
	migPhaseImported    = "imported"     // dados já em B; daqui em diante nunca abortar A
	migPhaseConnecting  = "connecting"   // esperando as contas conectarem em B
	migPhaseCommitting  = "committing"   // confirmando para A (repete até conseguir)
	migPhaseRollingBack = "rolling_back" // B não conectou: apagando o clone antes de liberar A
	migPhaseDone        = "done"
	migPhaseAborted     = "aborted" // falhou com segurança: B intacta, A desbloqueada
	migPhaseFailed      = "failed"  // precisa de atenção (ver error)
)

// migrationExcludedTable nunca entra no clone: cada lado guarda o próprio estado.
const migrationExcludedTable = "migration_state"

// migrationLegacyColumns: colunas que bancos antigos ainda carregam (SQLite não
// remove coluna) mas que nenhuma versão atual usa. Ficam de fora do clone em vez
// de barrar a importação como se o destino fosse mais antigo.
var migrationLegacyColumns = map[string][]string{
	"sessions": {"api_key"}, // auth por conta, anterior à API key global
}

type migrationState struct {
	Role      string             `json:"role,omitempty"`
	ID        string             `json:"id,omitempty"`
	Phase     string             `json:"phase,omitempty"`
	PeerURL   string             `json:"peerUrl,omitempty"`
	Manifest  *migrationManifest `json:"manifest,omitempty"`
	Report    *migrationReport   `json:"report,omitempty"`
	Error     string             `json:"error,omitempty"`
	Log       []migrationLogLine `json:"log,omitempty"`
	StartedAt int64              `json:"startedAt,omitempty"`
	UpdatedAt int64              `json:"updatedAt,omitempty"`
}

type migrationLogLine struct {
	At    int64  `json:"at"`
	Level string `json:"level"` // info | warn | error
	Msg   string `json:"msg"`
}

const migrationLogMax = 200

type migrationManifest struct {
	ID        string           `json:"id"`
	CreatedAt int64            `json:"createdAt"`
	DBSize    int64            `json:"dbSize"`
	DBSha256  string           `json:"dbSha256"`
	Tables    map[string]int64 `json:"tables"`
	Sessions  []migSession     `json:"sessions"`
	Files     []migFile        `json:"files"`
	Warnings  []string         `json:"warnings,omitempty"`
}

type migSession struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	JID  string `json:"jid"`
}

// migFile é um WAV ainda não entregue ao B2; viaja junto para B terminar o handoff.
type migFile struct {
	Name      string `json:"name"`
	CallID    string `json:"callId"`
	SessionID string `json:"sessionId"`
	Size      int64  `json:"size"`
	Sha256    string `json:"sha256"`
	path      string // só em A
}

type migrationReport struct {
	Tables          map[string]int64 `json:"tables"`
	Files           int              `json:"files"`
	PairedSessions  int              `json:"pairedSessions"`
	ConnectedOnB    int              `json:"connectedOnB"`
	NotConnected    []string         `json:"notConnected,omitempty"`
	CommittedAt     int64            `json:"committedAt,omitempty"`
	SourceCommitted bool             `json:"sourceCommitted"`
}

type migrationProgress struct {
	Step       string `json:"step"`
	BytesDone  int64  `json:"bytesDone"`
	BytesTotal int64  `json:"bytesTotal"`
}

type migrator struct {
	s     *server
	httpc *http.Client

	opMu     sync.Mutex // serializa operações longas (lock, commit, abort, início do import)
	mu       sync.Mutex // protege state/progress/running
	state    migrationState
	progress migrationProgress
	running  bool   // job de import/commit/rollback em curso neste processo
	peerKey  string // API key de A (só no destino, até o fim da migração)

	// waitConnected é trocado nos testes (conectar exige o WhatsApp de verdade).
	waitConnected func(ctx context.Context, timeout time.Duration) (connected, paired int, missing []string)
	connectWait   time.Duration
	retryBase     time.Duration
}

func newMigrator(s *server) *migrator {
	m := &migrator{
		s:           s,
		httpc:       &http.Client{},
		connectWait: 90 * time.Second,
		retryBase:   2 * time.Second,
	}
	m.waitConnected = m.waitSessionsConnected
	return m
}

// ---- persistência ----

func ensureMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS migration_state (
		id           INTEGER PRIMARY KEY CHECK (id = 1),
		data         TEXT NOT NULL,
		peer_api_key TEXT
	)`)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func loadMigrationState(ctx context.Context, db *sql.DB) (migrationState, string, error) {
	var data string
	var key sql.NullString
	err := db.QueryRowContext(ctx, `SELECT data, peer_api_key FROM migration_state WHERE id=1`).Scan(&data, &key)
	if errors.Is(err, sql.ErrNoRows) {
		return migrationState{}, "", nil
	}
	if err != nil {
		return migrationState{}, "", err
	}
	var st migrationState
	if err := json.Unmarshal([]byte(data), &st); err != nil {
		return migrationState{}, "", fmt.Errorf("migration_state corrompido: %w", err)
	}
	return st, key.String, nil
}

func saveMigrationState(ctx context.Context, ex execer, st migrationState, peerAPIKey string) error {
	st.UpdatedAt = time.Now().UnixMilli()
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	_, err = ex.ExecContext(ctx, `INSERT INTO migration_state (id, data, peer_api_key) VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET data=excluded.data, peer_api_key=excluded.peer_api_key`,
		string(data), peerAPIKey)
	return err
}

func (st *migrationState) addLog(level, format string, args ...any) {
	st.Log = append(st.Log, migrationLogLine{At: time.Now().UnixMilli(), Level: level, Msg: fmt.Sprintf(format, args...)})
	if len(st.Log) > migrationLogMax {
		st.Log = st.Log[len(st.Log)-migrationLogMax:]
	}
}

// update aplica fn ao estado e persiste; só troca o estado em memória se
// gravou. A API key de A (peerKey) só fica guardada enquanto B ainda precisa
// falar com A, para commit/abort sobreviverem a restart.
func (m *migrator) update(ctx context.Context, fn func(st *migrationState)) error {
	return m.updateWith(ctx, m.s.db, fn)
}

// updateWith grava pelo execer dado (a transação do clone, que segura a única
// conexão do pool).
func (m *migrator) updateWith(ctx context.Context, ex execer, fn func(st *migrationState)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.state
	next.Log = slices.Clone(m.state.Log)
	fn(&next)
	if err := saveMigrationState(ctx, ex, next, m.peerKey); err != nil {
		return err
	}
	m.state = next
	return nil
}

func (m *migrator) setPeerKey(k string) {
	m.mu.Lock()
	m.peerKey = k
	m.mu.Unlock()
}

func (m *migrator) setRunning(v bool) {
	m.mu.Lock()
	m.running = v
	m.mu.Unlock()
}

func (m *migrator) logf(level, format string, args ...any) {
	m.mu.Lock()
	m.state.addLog(level, format, args...)
	m.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	switch level {
	case "error":
		m.s.log.Error("migration: " + msg)
	case "warn":
		m.s.log.Warn("migration: " + msg)
	default:
		m.s.log.Info("migration: " + msg)
	}
}

func (m *migrator) setProgress(step string, done, total int64) {
	m.mu.Lock()
	m.progress = migrationProgress{Step: step, BytesDone: done, BytesTotal: total}
	m.mu.Unlock()
}

func (m *migrator) snapshot() (migrationState, migrationProgress, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state
	st.Log = slices.Clone(m.state.Log)
	return st, m.progress, m.running
}

// blocksSessions: enquanto true, nenhuma conta conecta e as rotas de escrita
// de /api/sessions respondem 423.
func (st migrationState) blocksSessions() bool {
	switch st.Role {
	case migRoleSource:
		return st.Phase == migPhaseLocked || st.Phase == migPhaseCommitted
	case migRoleTarget:
		return st.Phase == migPhasePreparing || st.Phase == migPhaseRollingBack
	}
	return false
}

// targetBusy: B tem um job que ainda precisa terminar (não aceita outro import).
func (st migrationState) targetBusy() bool {
	if st.Role != migRoleTarget {
		return false
	}
	switch st.Phase {
	case migPhaseDone, migPhaseAborted, migPhaseFailed, "":
		return false
	}
	return true
}

func (m *migrator) blockedMessage() (string, bool) {
	m.mu.Lock()
	st := m.state
	m.mu.Unlock()
	if !st.blocksSessions() {
		return "", false
	}
	switch {
	case st.Role == migRoleSource && st.Phase == migPhaseCommitted:
		return "instância migrada para " + st.PeerURL + " — desativada", true
	case st.Role == migRoleSource:
		return "instância bloqueada: migração em andamento para " + st.PeerURL, true
	default:
		return "migração em andamento: importando de " + st.PeerURL, true
	}
}

// guard bloqueia escrita em contas enquanto a migração exige tudo parado.
func (m *migrator) guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && (strings.HasPrefix(r.URL.Path, "/api/sessions") || r.URL.Path == "/api/panel-settings") {
			if msg, blocked := m.blockedMessage(); blocked {
				writeJSON(w, http.StatusLocked, map[string]string{"error": msg})
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// ---- helpers de SQLite ----

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

type tableInfo struct {
	name     string
	cols     []string
	hasRowID bool
}

// listTables devolve as tabelas de usuário do schema (main ou um ATTACH).
func listTables(ctx context.Context, q queryer, schema string) (map[string]tableInfo, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, COALESCE(sql, '') FROM `+schema+`.sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	type raw struct{ name, sql string }
	var list []raw
	for rows.Next() {
		var r raw
		if err := rows.Scan(&r.name, &r.sql); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make(map[string]tableInfo, len(list))
	for _, r := range list {
		cols, err := tableColumns(ctx, q, schema, r.name)
		if err != nil {
			return nil, err
		}
		out[r.name] = tableInfo{
			name:     r.name,
			cols:     cols,
			hasRowID: !strings.Contains(strings.ToUpper(r.sql), "WITHOUT ROWID"),
		}
	}
	return out, nil
}

func tableColumns(ctx context.Context, q queryer, schema, table string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA `+schema+`.table_info(`+quoteIdent(table)+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func countRows(ctx context.Context, q queryer, schema, table string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+schema+`.`+quoteIdent(table)).Scan(&n)
	return n, err
}

func countAllTables(ctx context.Context, q queryer, schema string) (map[string]int64, error) {
	tables, err := listTables(ctx, q, schema)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(tables))
	for name := range tables {
		if name == migrationExcludedTable {
			continue
		}
		n, err := countRows(ctx, q, schema, name)
		if err != nil {
			return nil, err
		}
		out[name] = n
	}
	return out, nil
}

// tableDigest: sha256 do conteúdo das colunas dadas, em ordem total. quote()
// dá a forma literal do SQLite (blob vira X'..'), independente do driver.
func tableDigest(ctx context.Context, q queryer, schema, table string, cols []string) (string, error) {
	if len(cols) == 0 {
		return "", nil
	}
	sel := make([]string, len(cols))
	ord := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = "quote(" + quoteIdent(c) + ")"
		ord[i] = fmt.Sprint(i + 1)
	}
	rows, err := q.QueryContext(ctx, `SELECT `+strings.Join(sel, ", ")+` FROM `+schema+`.`+quoteIdent(table)+
		` ORDER BY `+strings.Join(ord, ", "))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	h := sha256.New()
	vals := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return "", err
		}
		for _, v := range vals {
			fmt.Fprintf(h, "%d:%s|", len(v.String), v.String)
		}
		h.Write([]byte{'\n'})
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func integrityCheck(ctx context.Context, q queryer) error {
	var res string
	if err := q.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity_check: %s", res)
	}
	return nil
}

func foreignKeyCheck(ctx context.Context, q queryer, schema string) error {
	rows, err := q.QueryContext(ctx, `PRAGMA `+schema+`.foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table string
		var rowid sql.NullInt64
		var parent string
		var fkid int
		_ = rows.Scan(&table, &rowid, &parent, &fkid)
		return fmt.Errorf("foreign_key_check: %s (rowid %d) aponta para %s inexistente", table, rowid.Int64, parent)
	}
	return rows.Err()
}

func fileSha256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func newMigrationID() string {
	return newSessionID()
}

// ---- status (os dois lados) ----

func (s *server) handleMigrationStatus(w http.ResponseWriter, r *http.Request) {
	st, prog, running := s.mig.snapshot()
	resp := map[string]any{
		"role":      st.Role,
		"id":        st.ID,
		"phase":     st.Phase,
		"peerUrl":   st.PeerURL,
		"error":     st.Error,
		"log":       st.Log,
		"startedAt": st.StartedAt,
		"updatedAt": st.UpdatedAt,
		"running":   running,
		"progress":  prog,
		"report":    st.Report,
	}
	if mf := st.Manifest; mf != nil {
		resp["manifest"] = map[string]any{
			"dbSize":   mf.DBSize,
			"dbSha256": mf.DBSha256,
			"tables":   mf.Tables,
			"sessions": mf.Sessions,
			"files":    len(mf.Files),
			"warnings": mf.Warnings,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
