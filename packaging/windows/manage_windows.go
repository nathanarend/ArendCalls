//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	runKeyPath  = `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`
	stopTimeout = gracefulStop + 10*time.Second // servidor desconecta as contas antes de sair
)

// install registra (ou atualiza) o serviço, liga o ícone da bandeja no logon de
// todos os usuários, prepara o .env, libera a porta no firewall (se ADDR aceita
// a rede) e inicia o servidor.
func install() error {
	dir := appDir()
	exe := exePath()
	logf("install: pasta %s", dir)
	if _, err := os.Stat(filepath.Join(dir, serverExe)); err != nil {
		return fmt.Errorf("%s não encontrado em %s.\nExtraia o pacote completo antes de instalar.", serverExe, dir)
	}
	newKey, err := ensureEnv()
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("acesso ao gerenciador de serviços: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		// Já existe: aponta para este executável (pasta pode ter mudado / atualização).
		logErr("install: parando serviço existente", stopAndWait(s, stopTimeout))
		cfg, err := s.Config()
		if err != nil {
			s.Close()
			return fmt.Errorf("lendo configuração do serviço: %w", err)
		}
		logf("install: serviço existente, caminho %s → %s", cfg.BinaryPathName, syscall.EscapeArg(exe))
		cfg.BinaryPathName = syscall.EscapeArg(exe)
		cfg.DisplayName = displayName
		cfg.Description = serviceDesc
		cfg.StartType = mgr.StartAutomatic
		if err := s.UpdateConfig(cfg); err != nil {
			s.Close()
			return fmt.Errorf("atualizando serviço: %w", err)
		}
	} else {
		logf("install: criando serviço (%v)", err)
		s, err = m.CreateService(serviceName, exe, mgr.Config{
			DisplayName: displayName,
			Description: serviceDesc,
			StartType:   mgr.StartAutomatic,
		})
		if err != nil {
			return fmt.Errorf("criando serviço: %w", err)
		}
	}
	defer s.Close()

	logErr("install: prazo de pré-desligamento", setPreshutdownTimeout(s, stopTimeout))

	// Se o próprio lançador cair, o Windows sobe de novo (o servidor já é vigiado por ele).
	logErr("install: ações de recuperação", s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 24*60*60))

	if err := setTrayAutostart(exe); err != nil {
		return fmt.Errorf("registrando ícone da bandeja no logon: %w", err)
	}
	addr := listenAddr()
	_, port := addrParts(addr)
	if listensOnLAN() {
		firewallAllow(port)
	} else {
		firewallRemove() // ADDR voltou para 127.0.0.1: fecha a porta liberada antes
	}
	aclWarn := ""
	if err := lockDownFolder(dir); err != nil {
		logf("install: protegendo a pasta: %v", err)
		aclWarn = "\n\nATENÇÃO: não foi possível restringir a pasta do programa a administradores (" +
			err.Error() + "). O serviço roda como SYSTEM: qualquer usuário que puder alterar os " +
			".exe desta pasta ganha controle do computador. Prefira instalar em C:\\Program Files\\ArendCalls."
	}
	logf("install: serviço configurado, bandeja no logon, ADDR %s (rede=%v); iniciando", addr, listensOnLAN())

	if err := startAndWait(s, 20*time.Second); err != nil {
		return fmt.Errorf("serviço instalado, mas não iniciou: %w\nVeja logs\\servidor.log", err)
	}

	if !trayRunning() {
		// Pelo Explorer: a bandeja abre como o usuário comum, não herda o
		// administrador deste processo (Bloco de Notas, navegador etc. elevados).
		logErr("install: abrindo bandeja", exec.Command("explorer.exe", exe).Start())
	}

	where := "Painel neste computador: " + localURL()
	if ip := lanIP(); ip != "" && listensOnLAN() {
		where += fmt.Sprintf("\nNos outros computadores: http://%s:%d", ip, port)
	}
	extra := ""
	if newKey {
		extra = "\n\nUma API_KEY aleatória foi gerada no arquivo .env da pasta do programa " +
			"(menu do ícone > Editar configuração)."
	}
	msgInfo("Instalado com sucesso.\n\n" +
		"O servidor agora inicia junto com o Windows, mesmo sem ninguém logado, " +
		"e o ícone fica na bandeja (perto do relógio).\n\n" + where + extra + aclWarn)
	return nil
}

// lockDownFolder deixa a pasta do programa gravável só por administradores e
// SYSTEM (usuários: leitura e execução). O serviço roda como LocalSystem os .exe
// e lê o .env daqui: pasta criada em C:\ herda "Modificar" para todo usuário
// autenticado, e quem trocasse um .exe viraria SYSTEM. SIDs em vez de nomes:
// funciona em Windows de qualquer idioma.
func lockDownFolder(dir string) error {
	const (
		system = "*S-1-5-18"
		admins = "*S-1-5-32-544"
		users  = "*S-1-5-32-545"
	)
	// Dono = Administradores (o dono pode reescrever a ACL, então não pode ser
	// quem extraiu a pasta); pasta sem herança com as três permissões; conteúdo
	// passa a herdar só da pasta.
	steps := [][]string{
		{dir, "/setowner", admins, "/T", "/C", "/Q"},
		{dir, "/inheritance:r", "/grant:r", system + ":(OI)(CI)F", admins + ":(OI)(CI)F", users + ":(OI)(CI)RX", "/Q"},
		{filepath.Join(dir, "*"), "/reset", "/T", "/C", "/Q"},
	}
	for _, args := range steps {
		if err := run("icacls", args...); err != nil {
			return err
		}
	}
	logf("install: pasta restrita a administradores: %s", dir)
	return nil
}

// setPreshutdownTimeout: prazo do aviso de desligamento do Windows (PreShutdown),
// para o servidor fechar contas e banco antes de o sistema matar o processo.
func setPreshutdownTimeout(s *mgr.Service, d time.Duration) error {
	info := struct{ timeoutMs uint32 }{uint32(d.Milliseconds())} // SERVICE_PRESHUTDOWN_INFO
	return windows.ChangeServiceConfig2(s.Handle, windows.SERVICE_CONFIG_PRESHUTDOWN_INFO, (*byte)(unsafe.Pointer(&info)))
}

func uninstall() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("acesso ao gerenciador de serviços: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		logErr("uninstall: parando serviço", stopAndWait(s, stopTimeout))
		err = s.Delete()
		s.Close()
		if err != nil {
			return fmt.Errorf("removendo serviço: %w", err)
		}
	}
	removeTrayAutostart()
	firewallRemove()
	signalTrayQuit()
	msgInfo("Serviço removido.\n\nOs arquivos (.env, banco wacalls.db com as contas do WhatsApp, gravações, logs) continuam na pasta do programa.")
	return nil
}

func withService(fn func(*mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("acesso ao gerenciador de serviços: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return errors.New("serviço não instalado. Abra ArendCalls.exe e escolha instalar.")
	}
	defer s.Close()
	return fn(s)
}

// shutdownAll ("encerrar"): para o servidor e fecha a bandeja desta sessão.
// O serviço continua instalado (volta no próximo boot ou ao abrir o programa).
func shutdownAll() error {
	err := withService(func(s *mgr.Service) error { return stopAndWait(s, stopTimeout) })
	logErr("encerrar: parando serviço", err)
	signalTrayQuit()
	return err
}

func startService() error {
	return withService(func(s *mgr.Service) error { return startAndWait(s, 20*time.Second) })
}

func stopService() error {
	return withService(func(s *mgr.Service) error { return stopAndWait(s, stopTimeout) })
}

func restartService() error {
	return withService(func(s *mgr.Service) error {
		if err := stopAndWait(s, stopTimeout); err != nil {
			return err
		}
		return startAndWait(s, 20*time.Second)
	})
}

func startAndWait(s *mgr.Service, timeout time.Duration) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Running {
		return nil
	}
	if st.State == svc.StopPending {
		// Start numa parada em andamento devolve "já rodando" e nunca chega a
		// Running: espera parar de vez e então inicia.
		if err := waitState(s, svc.Stopped, stopTimeout); err != nil {
			return err
		}
		st.State = svc.Stopped
	}
	if st.State != svc.StartPending {
		if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return err
		}
	}
	return waitState(s, svc.Running, timeout)
}

func stopAndWait(s *mgr.Service, timeout time.Duration) error {
	st, err := s.Query()
	if err != nil {
		return err
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
			return err
		}
	}
	return waitState(s, svc.Stopped, timeout)
}

func waitState(s *mgr.Service, want svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		st, err := s.Query()
		if err != nil {
			return err
		}
		if st.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("tempo esgotado esperando o serviço (estado %d)", st.State)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// queryServiceState consulta o estado SEM exigir administrador (a bandeja roda
// como usuário comum). installed=false quando o serviço não existe.
func queryServiceState() (state uint32, installed bool, err error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, false, err
	}
	defer windows.CloseServiceHandle(scm)
	h, err := windows.OpenService(scm, u16(serviceName), windows.SERVICE_QUERY_STATUS)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return 0, true, err
	}
	return st.CurrentState, true, nil
}

// serviceExePath devolve o executável registrado no serviço, SEM exigir
// administrador (usuário comum tem SERVICE_QUERY_CONFIG por padrão).
func serviceExePath() (string, error) {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "", err
	}
	defer windows.CloseServiceHandle(scm)
	h, err := windows.OpenService(scm, u16(serviceName), windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return "", err
	}
	defer windows.CloseServiceHandle(h)

	// O Windows grava no buffer ponteiros ABSOLUTOS para dentro dele mesmo. Buffer local
	// pode ficar na pilha da goroutine (o compilador decide), e a pilha é copiada ao
	// crescer/encolher: os ponteiros passam a apontar para memória liberada → acesso
	// inválido, processo morre com código 2 sem stack no log. Buffer global nunca se move.
	svcCfgMu.Lock()
	defer svcCfgMu.Unlock()
	for {
		var need uint32
		cfg := (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&svcCfgBuf[0]))
		err = windows.QueryServiceConfig(h, cfg, uint32(len(svcCfgBuf)), &need)
		if err == nil {
			return unquoteExe(windows.UTF16PtrToString(cfg.BinaryPathName)), nil
		}
		if !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) || int(need) <= len(svcCfgBuf) {
			return "", err
		}
		svcCfgBuf = make([]byte, need)
	}
}

// Buffer de QueryServiceConfig: global de propósito (ver serviceExePath).
var (
	svcCfgMu  sync.Mutex
	svcCfgBuf = make([]byte, 1024)
)

// unquoteExe extrai o executável de uma linha de comando ("C:\x y\a.exe" args → C:\x y\a.exe).
func unquoteExe(cmdline string) string {
	s := strings.TrimSpace(cmdline)
	if rest, ok := strings.CutPrefix(s, `"`); ok {
		if exe, _, ok := strings.Cut(rest, `"`); ok {
			return exe
		}
		return rest
	}
	return s
}

// serviceInOtherFolder: serviço instalado, mas apontando para outro executável
// (pasta do programa mudou de lugar ou há outra cópia instalada). Devolve o caminho registrado.
func serviceInOtherFolder() (string, bool, error) {
	registered, err := serviceExePath()
	if err != nil || registered == "" {
		return registered, false, err
	}
	same := strings.EqualFold(filepath.Clean(registered), filepath.Clean(exePath()))
	return registered, !same, nil
}

// Ícone da bandeja no logon de qualquer usuário (HKLM\...\Run).
func setTrayAutostart(exe string) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(runKeyName, syscall.EscapeArg(exe)+" tray")
}

func removeTrayAutostart() {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		if k, err := registry.OpenKey(root, runKeyPath, registry.SET_VALUE); err == nil {
			_ = k.DeleteValue(runKeyName)
			k.Close()
		}
	}
}

func netsh(args ...string) error { return run("netsh", args...) }

// run executa um comando do sistema sem janela; o erro traz a saída.
func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// firewallAllow libera, para os outros computadores da rede, a porta do painel
// (TCP) e a faixa da mídia WebRTC das chamadas (UDP 50000-50100, cmd/server/bridge.go).
func firewallAllow(port int) {
	firewallRemove()
	logErr("firewall: liberando painel", netsh("advfirewall", "firewall", "add", "rule", "name="+firewallRule,
		"dir=in", "action=allow", "protocol=TCP", "localport="+strconv.Itoa(port), "profile=any"))
	logErr("firewall: liberando WebRTC", netsh("advfirewall", "firewall", "add", "rule", "name="+firewallRule,
		"dir=in", "action=allow", "protocol=UDP", "localport=50000-50100", "profile=any"))
}

// firewallRemove apaga todas as regras com o nome do app (TCP e UDP).
func firewallRemove() {
	// Falha esperada quando a regra ainda não existe; registra mesmo assim.
	logErr("firewall: removendo regras antigas", netsh("advfirewall", "firewall", "delete", "rule", "name="+firewallRule))
}
