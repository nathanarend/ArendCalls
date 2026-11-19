//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// Log do lançador (bandeja + comandos install/start/stop...):
// %LOCALAPPDATA%\ArendCalls\lancador.log. Fica fora da pasta do programa porque
// a bandeja roda como usuário comum e Program Files não é gravável. Recebe também o
// stack de qualquer panic: o .exe é windowsgui, sem isso uma falha some sem rastro.
// O serviço tem o próprio log (logs\servidor.log) e grava panics em logs\falhas.log.

const launcherLogMax = 1 << 20 // passou de 1 MB, recomeça

var (
	logMu  sync.Mutex
	logOut *os.File
)

func launcherLogPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ArendCalls", "lancador.log")
}

// openLog abre (ou cria, com BOM UTF-8 para o Bloco de Notas/PowerShell) e liga a saída de panics.
func openLog(path string, maxBytes int64) *os.File {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && maxBytes > 0 && fi.Size() > maxBytes {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil
	}
	if fi, err := f.Stat(); err == nil && fi.Size() == 0 {
		_, _ = f.WriteString("\xEF\xBB\xBF")
	}
	return f
}

func openLauncherLog() {
	logOut = openLog(launcherLogPath(), launcherLogMax)
	if logOut != nil {
		_ = debug.SetCrashOutput(logOut, debug.CrashOptions{})
	}
}

// crashTo manda o stack de panics/falhas fatais para path (usado pelo serviço).
func crashTo(path string) {
	if f := openLog(path, launcherLogMax); f != nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
	}
}

func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logOut == nil {
		return
	}
	msg := strings.ReplaceAll(fmt.Sprintf(format, args...), "\n", " | ")
	fmt.Fprintf(logOut, "%s [pid %d] %s\r\n", time.Now().Format("2006-01-02 15:04:05"), os.Getpid(), msg)
}

// logErr registra um erro que seria ignorado (nil = nada).
func logErr(what string, err error) {
	if err != nil {
		logf("%s: %v", what, err)
	}
}
