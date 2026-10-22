package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

type migNode struct {
	srv *server
	ts  *httptest.Server
	dir string
	key string
}

// newMigNode sobe um servidor completo (newServer + boot + Restore) num dir
// próprio, como o main faz. Sem rec.start: nada tenta subir para o B2.
func newMigNode(t *testing.T, dir, apiKey, recKey string, wrap func(http.Handler) http.Handler) *migNode {
	t.Helper()
	t.Setenv("RECORDING_CONFIG_KEY", recKey)
	ctx, cancel := context.WithCancel(context.Background())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := newServer(ctx, filepath.Join(dir, "wacalls.db"), "", apiKey, filepath.Join(dir, "recordings"), 0, 1, nil, log)
	if err != nil {
		t.Fatal(err)
	}
	srv.mig.retryBase = 5 * time.Millisecond
	srv.mig.connectWait = 200 * time.Millisecond
	if err := srv.mig.boot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := srv.sessions.Restore(ctx); err != nil {
		t.Fatal(err)
	}
	srv.mig.resumeJobs(ctx)
	var h http.Handler = srv.routes()
	if wrap != nil {
		h = wrap(h)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		ts.Close()
		cancel()
		srv.db.Close()
	})
	return &migNode{srv: srv, ts: ts, dir: dir, key: apiKey}
}

func (n *migNode) do(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, n.ts.URL+path, rd)
	req.Header.Set("X-Api-Key", n.key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (n *migNode) count(t *testing.T, table string) int64 {
	t.Helper()
	c, err := countRows(context.Background(), n.srv.db, "main", table)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// seedSource: 2 contas, 1 device (chaves Signal), config de gravação com
// segredos cifrados e um WAV pendente de upload.
func seedSource(t *testing.T, n *migNode) (deviceJID types.JID, wav []byte) {
	t.Helper()
	ctx := context.Background()
	st := n.srv.sessions.store
	for _, id := range []string{"aaaa", "bbbb"} {
		if err := st.insert(ctx, id, "Conta "+id); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.setWebhookURL(ctx, "aaaa", "https://crm.exemplo/hook"); err != nil {
		t.Fatal(err)
	}

	dev := n.srv.sessions.container.NewDevice()
	deviceJID = types.NewADJID("5511999990000", 0, 7)
	dev.ID = &deviceJID
	dev.PushName = "Clínica"
	dev.Account = &waAdv.ADVSignedDeviceIdentity{Details: []byte("adv"), AccountSignature: make([]byte, 64), AccountSignatureKey: make([]byte, 32), DeviceSignature: make([]byte, 64)}
	if err := n.srv.sessions.container.PutDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	for i := range 50 {
		if err := dev.Identities.PutIdentity(ctx, fmt.Sprintf("55119%07d.0:0", i), [32]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}

	if err := n.srv.recStore.saveConfig(ctx, RecordingConfig{
		SessionID: "aaaa", B2Endpoint: "https://b2", B2Bucket: "bk", B2KeyID: "kid", B2AppKey: "app-secret",
		WebhookURL: "https://hook", WebhookSecret: "hmac-secret",
	}, n.srv.rec.secrets); err != nil {
		t.Fatal(err)
	}

	wav = bytes.Repeat([]byte("RIFFwave"), 4096)
	path := filepath.Join(n.dir, "recordings", "aaaa", "2026-10", "CALL1.wav")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, wav, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := n.srv.recStore.begin(ctx, RecordingRow{CallID: "CALL1", SessionID: "aaaa", LocalPath: path, Channels: "stereo", StartedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := n.srv.recStore.finishRecording(ctx, "CALL1", 60000, 2, RecStatusUploading); err != nil {
		t.Fatal(err)
	}
	return deviceJID, wav
}

func waitPhase(t *testing.T, n *migNode, phases ...string) migrationState {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, _, running := n.srv.mig.snapshot()
		for _, p := range phases {
			if st.Phase == p && !running {
				return st
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	st, _, _ := n.srv.mig.snapshot()
	t.Fatalf("fase esperada %v, ficou %q (erro %q)", phases, st.Phase, st.Error)
	return st
}

func startImport(t *testing.T, a, b *migNode) {
	t.Helper()
	code, out := b.do(t, "POST", "/api/migration/import", map[string]any{
		"sourceUrl": a.ts.URL, "sourceApiKey": a.key, "allowInsecure": true,
	})
	if code != http.StatusAccepted {
		t.Fatalf("import: %d %v", code, out)
	}
}

func TestMigrationCloneEndToEnd(t *testing.T) {
	ctx := context.Background()
	a := newMigNode(t, t.TempDir(), "key-a", "rec-key-a", nil)
	jid, wav := seedSource(t, a)
	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)

	startImport(t, a, b)
	st := waitPhase(t, b, migPhaseDone, migPhaseAborted, migPhaseFailed)
	if st.Phase != migPhaseDone {
		t.Fatalf("migração não concluiu: %s — %s", st.Phase, st.Error)
	}
	if st.Report == nil || !st.Report.SourceCommitted {
		t.Fatalf("relatório sem commit da origem: %+v", st.Report)
	}

	// B é um clone: contas (ordem e webhook), device, gravação.
	rows, err := b.srv.sessions.store.list(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "aaaa" || rows[1].ID != "bbbb" || rows[0].WebhookURL != "https://crm.exemplo/hook" {
		t.Fatalf("contas em B: %+v", rows)
	}
	if got := b.srv.sessions.infos(); len(got) != 2 {
		t.Fatalf("B não recarregou as contas em memória: %d", len(got))
	}
	dev, err := b.srv.sessions.container.GetDevice(ctx, jid)
	if err != nil || dev == nil || dev.PushName != "Clínica" {
		t.Fatalf("device não veio para B: %v %+v", err, dev)
	}
	if n := b.count(t, "whatsmeow_identity_keys"); n != 50 {
		t.Fatalf("identity keys em B: %d", n)
	}

	// Segredos: legíveis com a chave de B, cifrados (não iguais ao valor de A).
	cfg, err := b.srv.recStore.config(ctx, "aaaa", b.srv.rec.secrets)
	if err != nil || cfg.B2AppKey != "app-secret" || cfg.WebhookSecret != "hmac-secret" {
		t.Fatalf("config de gravação em B: %v %+v", err, cfg)
	}
	var raw string
	_ = b.srv.db.QueryRow(`SELECT b2_app_key_enc FROM recording_config WHERE session_id='aaaa'`).Scan(&raw)
	if !strings.HasPrefix(raw, encPrefix) {
		t.Fatalf("segredo em B deveria estar cifrado: %q", raw)
	}

	row, err := b.srv.recStore.get(ctx, "CALL1")
	if err != nil || row == nil {
		t.Fatalf("gravação pendente não veio: %v", err)
	}
	if !strings.HasPrefix(row.LocalPath, b.dir) {
		t.Fatalf("local_path deveria apontar para B: %s", row.LocalPath)
	}
	if got, err := os.ReadFile(row.LocalPath); err != nil || !bytes.Equal(got, wav) {
		t.Fatalf("WAV em B difere: %v", err)
	}

	// A: tombstone, sem chaves, rotas de escrita bloqueadas.
	ast, _, _ := a.srv.mig.snapshot()
	if ast.Role != migRoleSource || ast.Phase != migPhaseCommitted {
		t.Fatalf("A deveria estar committed: %+v", ast)
	}
	if n := a.count(t, "whatsmeow_device") + a.count(t, "whatsmeow_identity_keys"); n != 0 {
		t.Fatalf("A ainda tem %d linha(s) de chaves", n)
	}
	if code, _ := a.do(t, "POST", "/api/sessions", map[string]string{"name": "x"}); code != http.StatusLocked {
		t.Fatalf("A deveria responder 423, veio %d", code)
	}
	if !a.srv.sessions.blocked.Load() {
		t.Fatal("A deveria seguir bloqueada")
	}

	// Pastas temporárias removidas nos dois lados.
	for _, n := range []*migNode{a, b} {
		matches, _ := filepath.Glob(filepath.Join(n.dir, "migration-*"))
		if len(matches) != 0 {
			t.Fatalf("sobrou pasta de migração: %v", matches)
		}
	}
	// B não guarda mais a API key de A.
	if _, key, _ := loadMigrationState(ctx, b.srv.db); key != "" {
		t.Fatal("API key da origem ficou salva em B")
	}
}

func TestMigrationCorruptDownloadLeavesBothIntact(t *testing.T) {
	ctx := context.Background()
	corrupt := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/files/db") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(bytes.Repeat([]byte{0xAB}, 1024))
				return
			}
			h.ServeHTTP(w, r)
		})
	}
	a := newMigNode(t, t.TempDir(), "key-a", "rec-key-a", corrupt)
	seedSource(t, a)
	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)
	if err := b.srv.sessions.store.insert(ctx, "bown", "Conta de B"); err != nil {
		t.Fatal(err)
	}

	startImport(t, a, b)
	st := waitPhase(t, b, migPhaseDone, migPhaseAborted, migPhaseFailed)
	if st.Phase != migPhaseAborted {
		t.Fatalf("esperava aborted, veio %s — %s", st.Phase, st.Error)
	}
	if !strings.Contains(st.Error, "conferência falhou") {
		t.Fatalf("erro deveria citar a conferência: %s", st.Error)
	}

	ast, _, _ := a.srv.mig.snapshot()
	if ast.Phase != migPhaseUnlocked || a.srv.sessions.blocked.Load() {
		t.Fatalf("A deveria estar desbloqueada: %+v", ast)
	}
	if n := a.count(t, "whatsmeow_device"); n != 1 {
		t.Fatalf("A perdeu o device: %d", n)
	}
	rows, _ := b.srv.sessions.store.list(ctx)
	if len(rows) != 1 || rows[0].ID != "bown" {
		t.Fatalf("B foi alterada: %+v", rows)
	}
	if b.srv.sessions.blocked.Load() {
		t.Fatal("B ficou bloqueada")
	}
}

func TestMigrationRollbackWhenNothingConnects(t *testing.T) {
	ctx := context.Background()
	a := newMigNode(t, t.TempDir(), "key-a", "rec-key-a", nil)
	seedSource(t, a)
	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)
	if err := b.srv.sessions.store.insert(ctx, "bown", "Conta de B"); err != nil {
		t.Fatal(err)
	}
	b.srv.sessions.Restore(ctx)
	b.srv.mig.waitConnected = func(context.Context, time.Duration) (int, int, []string) {
		return 0, 1, []string{"Conta aaaa"}
	}

	startImport(t, a, b)
	st := waitPhase(t, b, migPhaseDone, migPhaseAborted, migPhaseFailed)
	if st.Phase != migPhaseAborted {
		t.Fatalf("esperava aborted, veio %s — %s", st.Phase, st.Error)
	}

	// B voltou ao backup: só a conta própria, sem device, sem a gravação de A.
	rows, _ := b.srv.sessions.store.list(ctx)
	if len(rows) != 1 || rows[0].ID != "bown" {
		t.Fatalf("B não voltou ao estado anterior: %+v", rows)
	}
	if n := b.count(t, "whatsmeow_device"); n != 0 {
		t.Fatalf("clone ficou em B: %d device(s)", n)
	}
	if n := b.count(t, "call_recordings"); n != 0 {
		t.Fatalf("gravação de A ficou em B: %d", n)
	}
	if _, err := os.Stat(filepath.Join(b.dir, "recordings", "aaaa", "migrated", "CALL1.wav")); !os.IsNotExist(err) {
		t.Fatalf("WAV de A ficou em B: %v", err)
	}

	ast, _, _ := a.srv.mig.snapshot()
	if ast.Phase != migPhaseUnlocked || a.srv.sessions.blocked.Load() {
		t.Fatalf("A deveria estar desbloqueada: %+v", ast)
	}
	if n := a.count(t, "whatsmeow_device"); n != 1 {
		t.Fatalf("A perdeu o device: %d", n)
	}
}

func TestMigrationRefusesTargetWithPairedAccount(t *testing.T) {
	a := newMigNode(t, t.TempDir(), "key-a", "rec-key-a", nil)
	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)
	dev := b.srv.sessions.container.NewDevice()
	jid := types.NewADJID("5511888880000", 0, 2)
	dev.ID = &jid
	b.srv.sessions.register(newSession(b.srv.sessions, "pp", "Pareada", "", false, whatsmeow.NewClient(dev, waLog.Noop)))

	code, out := b.do(t, "POST", "/api/migration/import", map[string]any{
		"sourceUrl": a.ts.URL, "sourceApiKey": a.key, "allowInsecure": true,
	})
	if code != http.StatusConflict {
		t.Fatalf("esperava 409, veio %d %v", code, out)
	}
	if st, _, _ := a.srv.mig.snapshot(); st.Phase != "" {
		t.Fatalf("A não deveria ter sido tocada: %+v", st)
	}
}

func TestMigrationRequiresHTTPSUnlessAllowed(t *testing.T) {
	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)
	code, _ := b.do(t, "POST", "/api/migration/import", map[string]any{
		"sourceUrl": "http://10.0.0.5:8080", "sourceApiKey": "k",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("http sem allowInsecure deveria dar 400, veio %d", code)
	}
}

func TestMigrationSourceLockSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	a := newMigNode(t, dir, "key-a", "rec-key-a", nil)
	seedSource(t, a)

	id := newMigrationID()
	code, out := a.do(t, "POST", "/api/migration/source/lock", map[string]string{"migrationId": id, "target": "https://b"})
	if code != http.StatusOK {
		t.Fatalf("lock: %d %v", code, out)
	}
	// Idempotente: mesmo id, mesmo snapshot.
	code, out2 := a.do(t, "POST", "/api/migration/source/lock", map[string]string{"migrationId": id, "target": "https://b"})
	if code != http.StatusOK || out2["dbSha256"] != out["dbSha256"] {
		t.Fatalf("lock repetido deveria devolver o mesmo manifest: %d %v", code, out2)
	}
	// Outro id: conflito.
	if code, _ := a.do(t, "POST", "/api/migration/source/lock", map[string]string{"migrationId": newMigrationID()}); code != http.StatusConflict {
		t.Fatalf("segundo lock deveria dar 409, veio %d", code)
	}
	// Commit com sha errado é recusado.
	if code, _ := a.do(t, "POST", "/api/migration/source/"+id+"/commit", map[string]string{"dbSha256": strings.Repeat("0", 64)}); code != http.StatusConflict {
		t.Fatalf("commit com sha errado deveria dar 409, veio %d", code)
	}

	// "Restart": outro processo no mesmo banco continua bloqueado.
	a2 := newMigNode(t, dir, "key-a", "rec-key-a", nil)
	if !a2.srv.sessions.blocked.Load() || !a2.srv.rec.paused.Load() {
		t.Fatal("após restart A deveria seguir bloqueada e com gravação pausada")
	}
	if code, _ := a2.do(t, "POST", "/api/sessions", map[string]string{"name": "x"}); code != http.StatusLocked {
		t.Fatalf("esperava 423, veio %d", code)
	}
	if code, _ := a2.do(t, "POST", "/api/migration/source/"+id+"/abort", nil); code != http.StatusOK {
		t.Fatalf("abort: %d", code)
	}
	if a2.srv.sessions.blocked.Load() {
		t.Fatal("abort deveria desbloquear")
	}
	if code, _ := a2.do(t, "POST", "/api/migration/source/"+id+"/commit", map[string]string{"dbSha256": out["dbSha256"].(string)}); code != http.StatusConflict {
		t.Fatalf("commit depois do abort deveria dar 409, veio %d", code)
	}
}

func TestCloneTablesSkipsLegacyColumns(t *testing.T) {
	ctx := context.Background()
	a := newMigNode(t, t.TempDir(), "key-a", "rec-key-a", nil)
	if _, err := a.srv.db.Exec(`ALTER TABLE sessions ADD COLUMN api_key TEXT`); err != nil {
		t.Fatal(err)
	}
	if err := a.srv.sessions.store.insert(ctx, "old", "Antiga"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.srv.db.Exec(`UPDATE sessions SET api_key='legado'`); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "src.db")
	if _, err := a.srv.db.Exec(`VACUUM INTO ?`, src); err != nil {
		t.Fatal(err)
	}

	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)
	if _, err := b.srv.mig.cloneTables(ctx, src, nil); err != nil {
		t.Fatalf("coluna legada barrou o clone: %v", err)
	}
	if n := b.count(t, "sessions"); n != 1 {
		t.Fatalf("esperava 1 conta clonada, veio %d", n)
	}
}

func TestCloneTablesRejectsUnknownSourceTable(t *testing.T) {
	ctx := context.Background()
	b := newMigNode(t, t.TempDir(), "key-b", "rec-key-b", nil)
	src := filepath.Join(t.TempDir(), "src.db")
	sdb, err := sql.Open("sqlite", "file:"+src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.Exec(`CREATE TABLE future_feature (x)`); err != nil {
		t.Fatal(err)
	}
	sdb.Close()
	if err := b.srv.sessions.store.insert(ctx, "keep", "Fica"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.srv.mig.cloneTables(ctx, src, nil); err == nil || !strings.Contains(err.Error(), "future_feature") {
		t.Fatalf("esperava erro citando a tabela desconhecida, veio %v", err)
	}
	if n := b.count(t, "sessions"); n != 1 {
		t.Fatalf("clone com erro alterou B: %d contas", n)
	}
}
