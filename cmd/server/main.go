package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dbPath := flag.String("db", "wacalls.db", "SQLite database path")
	staticDir := flag.String("static", "", "Directory for static files (e.g. client/dist)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	maxCalls := flag.Int("max-calls", 0, "Max concurrent calls per session (0 = unlimited)")
	apiKeyFlag := flag.String("apikey", "", "Global API Key for admin access (overrides API_KEY env var)")
	flag.Parse()

	logLevel := slog.LevelInfo
	if *debug {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	apiKey := *apiKeyFlag
	if apiKey == "" {
		apiKey = os.Getenv("API_KEY")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := newServer(ctx, *dbPath, *staticDir, apiKey, *maxCalls, log)
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer srv.sessions.disconnectAll()

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
