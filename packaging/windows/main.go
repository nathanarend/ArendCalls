//go:build windows

// ArendCalls.exe: lançador do ArendCalls no Windows (10+/Server 2016+).
//
// Um único executável com dois papéis:
//   - Serviço do Windows: sobe no boot (sem ninguém logado), roda
//     arendcalls-server.exe escondido com a configuração do .env, reinicia se
//     cair e grava o log em logs\servidor.log.
//   - Ícone na bandeja: inicia no logon de cada usuário, mostra o estado
//     (serviço + servidor + contas do WhatsApp) e dá atalhos (painel, reiniciar, log...).
//
// Comandos (os que mexem no serviço pedem administrador via UAC sozinhos):
//
//	ArendCalls.exe              bandeja (padrão; oferece instalar se faltar)
//	ArendCalls.exe install      instala/atualiza e inicia o serviço
//	ArendCalls.exe uninstall    para e remove o serviço
//	ArendCalls.exe start|stop|restart|encerrar
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/svc"
)

const (
	serviceName  = "ArendCalls"
	displayName  = "ArendCalls"
	serviceDesc  = "Servidor do ArendCalls (chamadas de voz do WhatsApp). Painel no ícone da bandeja."
	serverExe    = "arendcalls-server.exe"
	runKeyName   = "ArendCallsBandeja"
	firewallRule = "ArendCalls"
	trayMutex    = `Local\ArendCallsBandeja`
	trayQuitEvt  = `Local\ArendCallsBandejaSair`
)

var version = "dev"

// openedByUser: aberto com duplo clique (sem argumento), não pelo logon do Windows.
var openedByUser bool

func main() {
	if isSvc, err := svc.IsWindowsService(); err == nil && isSvc {
		runService()
		return
	}

	cmd := ""
	if len(os.Args) > 1 {
		cmd = strings.ToLower(strings.TrimLeft(os.Args[1], "-/"))
	}
	openLauncherLog()
	logf("início: versão %s, comando %q, exe %s, admin=%v", version, cmd, exePath(), isElevated())
	defer logf("fim: comando %q", cmd)

	switch cmd {
	case "":
		openedByUser = true // duplo clique: garante o servidor no ar (ver tray.poll)
		runTray()
	case "tray", "bandeja":
		runTray()
	case "install", "instalar":
		asAdmin(cmd, install)
	case "uninstall", "desinstalar":
		asAdmin(cmd, uninstall)
	case "start", "iniciar":
		asAdmin(cmd, startService)
	case "stop", "parar":
		asAdmin(cmd, stopService)
	case "restart", "reiniciar":
		asAdmin(cmd, restartService)
	case "encerrar", "exit", "quit":
		asAdmin(cmd, shutdownAll)
	case "version", "versao":
		msgInfo(fmt.Sprintf("%s %s", displayName, version))
	default:
		msgError(fmt.Sprintf("Comando desconhecido: %q\n\nUse: install, uninstall, start, stop, restart, encerrar.", os.Args[1]))
		os.Exit(2)
	}
}

// asAdmin roda fn com privilégio de administrador: se o processo não estiver
// elevado, relança a si mesmo com UAC ("runas") e sai.
func asAdmin(cmd string, fn func() error) {
	if !isElevated() {
		logf("%s: pedindo administrador (UAC)", cmd)
		if err := runElevated(cmd); err != nil && !isCancelled(err) {
			msgError("Não foi possível obter permissão de administrador:\n" + err.Error())
			os.Exit(1)
		}
		return
	}
	if err := fn(); err != nil {
		msgError(err.Error())
		logf("fim: comando %q falhou", cmd)
		os.Exit(1)
	}
	logf("%s: concluído", cmd)
}

// appDir é a pasta do executável: lá ficam arendcalls-server.exe, client\dist,
// .env, wacalls.db, recordings\ e logs\.
func appDir() string {
	return filepath.Dir(exePath())
}

func exePath() string {
	exe, _ := os.Executable()
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		return r
	}
	return exe
}
