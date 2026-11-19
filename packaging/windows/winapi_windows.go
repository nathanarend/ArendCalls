//go:build windows

package main

import (
	"errors"
	"net"

	"golang.org/x/sys/windows"
)

const (
	mbOK         = 0x00000000
	mbYesNo      = 0x00000004
	mbIconError  = 0x00000010
	mbIconQuest  = 0x00000020
	mbIconInfo   = 0x00000040
	mbTopmost    = 0x00040000
	mbSetFg      = 0x00010000
	idYes        = 6
	swShowNormal = 1
)

func u16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// msgBox mostra a caixa e registra no log o texto e a resposta (tudo que o usuário vê fica no log).
func msgBox(text string, flags uint32) int32 {
	kind := "aviso"
	if flags&mbIconError != 0 {
		kind = "ERRO"
	}
	logf("caixa %s: %s", kind, text)
	r, err := windows.MessageBox(0, u16(text), u16(displayName), flags|mbTopmost|mbSetFg)
	if flags&mbYesNo != 0 || err != nil {
		logf("caixa: resposta %d, erro %v", r, err)
	}
	return r
}

func msgInfo(text string)  { msgBox(text, mbOK|mbIconInfo) }
func msgError(text string) { msgBox(text, mbOK|mbIconError) }
func msgAsk(text string) bool {
	return msgBox(text, mbYesNo|mbIconQuest) == idYes
}

func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// runElevated relança este executável com UAC, passando args.
func runElevated(args string) error {
	return windows.ShellExecute(0, u16("runas"), u16(exePath()), u16(args), u16(appDir()), swShowNormal)
}

func isCancelled(err error) bool {
	return errors.Is(err, windows.ERROR_CANCELLED)
}

// shellOpen abre URL, arquivo ou pasta com o programa padrão do Windows.
func shellOpen(target string) error {
	return windows.ShellExecute(0, u16("open"), u16(target), nil, nil, swShowNormal)
}

// lanIP devolve o IPv4 da rede local (o que os outros computadores usam).
func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	best := ""
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip.IsPrivate() {
				return ip.String()
			}
			if best == "" {
				best = ip.String()
			}
		}
	}
	return best
}

// Eventos nomeados por sessão: "fechar a bandeja" (usado pelo uninstall).
func signalTrayQuit() {
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, u16(trayQuitEvt))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.SetEvent(h)
}

func trayRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, u16(trayMutex))
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}
