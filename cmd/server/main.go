package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	rtdebug "runtime/debug"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "wacalls.db", "SQLite database path")
	staticDir := flag.String("static", "", "Directory for static files (default: client/dist next to the executable or in the working dir)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	maxCalls := flag.Int("max-calls", 0, "Max concurrent calls per session (0 = unlimited)")
	apiKeyFlag := flag.String("apikey", "", "Global API Key for admin access (overrides API_KEY env var)")
	pprofAddr := flag.String("pprof", "", "If set, serve net/http/pprof on this address (bind to localhost, e.g. 127.0.0.1:6060 — never expose publicly)")
	recDir := flag.String("recordings-dir", "recordings", "Directory for server-side call recordings")
	recWorkers := flag.Int("recording-workers", 3, "Concurrent B2 upload workers for call recordings")
	flag.Parse()

	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	// Sem GOGC no ambiente (duplo clique, serviço Windows), aplica o mesmo
	// tuning da imagem Docker: o default 100 custa CPU no hot path de áudio.
	gogc := os.Getenv("GOGC")
	if gogc == "" {
		rtdebug.SetGCPercent(200)
		gogc = "200"
	}
	log.Info("runtime",
		"gomaxprocs", runtime.GOMAXPROCS(0), "numcpu", runtime.NumCPU(),
		"gogc", gogc, "gomemlimit", cmp.Or(os.Getenv("GOMEMLIMIT"), "off"))

	*staticDir = resolveStaticDir(*staticDir)
	if _, err := os.Stat(*staticDir); err == nil {
		log.Info("serving web panel", "dir", *staticDir)
	} else {
		log.Warn("web panel not found — serving API only; use -static to point to client/dist", "static", *staticDir)
	}

	if *pprofAddr != "" {
		pmux := http.NewServeMux()
		pmux.HandleFunc("/debug/pprof/", pprof.Index)
		pmux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		pmux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		pmux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		pmux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		go func() {
			log.Warn("pprof listener enabled — do not expose this port publicly", "addr", *pprofAddr)
			if err := http.ListenAndServe(*pprofAddr, pmux); err != nil {
				log.Error("pprof listener error", "err", err)
			}
		}()
	}

	apiKey := *apiKeyFlag
	if apiKey == "" {
		apiKey = os.Getenv("API_KEY")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := newServer(ctx, *dbPath, *staticDir, apiKey, *recDir, *maxCalls, *recWorkers, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer srv.sessions.disconnectAll()

	srv.rec.start()

	if err := srv.sessions.Restore(ctx); err != nil {
		log.Error("session restore failed", "err", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{Addr: *addr, Handler: srv.routes()}
	go func() {
		log.Info("HTTP server listening", "addr", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server error", "err", err)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
}

// resolveStaticDir: sem -static, procura client/dist ao lado do executável
// (duplo clique, serviço Windows) e depois no diretório atual.
func resolveStaticDir(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "client", "dist"))
	}
	candidates = append(candidates, filepath.Join("client", "dist"))
	for _, dir := range candidates {
		if fi, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !fi.IsDir() {
			return dir
		}
	}
	return ""
}
