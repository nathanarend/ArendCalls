//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"arendcalls/launcher/icon"
)

type trayState int

const (
	stUnknown trayState = iota
	stNotInstalled
	stStopped
	stStarting
	stOK
	stAttention // servidor no ar, alguma conta do WhatsApp precisa de atenção
	stNoResponse
)

var stateInfo = map[trayState]struct {
	text  string
	color color.RGBA
}{
	stUnknown:      {"Verificando…", icon.Gray},
	stNotInstalled: {"Serviço não instalado", icon.Gray},
	stStopped:      {"Servidor parado", icon.Red},
	stStarting:     {"Servidor iniciando…", icon.Blue},
	stOK:           {"Servidor no ar", icon.Green},
	stAttention:    {"Servidor no ar · conta desconectada", icon.Amber},
	stNoResponse:   {"Servidor não responde", icon.Red},
}

func runTray() {
	// Um ícone por sessão de usuário.
	m, err := windows.CreateMutex(nil, false, u16(trayMutex))
	if err != nil {
		logf("bandeja: saindo, mutex: %v (outra bandeja já aberta nesta sessão?)", err)
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) && openedByUser {
			alreadyOpenNotice()
		}
		return
	}
	defer windows.CloseHandle(m)
	systray.Run(onTrayReady, func() { logf("bandeja: encerrada") })
}

// alreadyOpenNotice: duplo clique com a bandeja já aberta → avisa em vez de sair calado.
func alreadyOpenNotice() {
	t := &tray{state: stOK} // só para a consulta; sem ícone/menu
	st, detail := t.check(&http.Client{Timeout: 5 * time.Second})
	where := "O ícone fica na bandeja, perto do relógio (pode estar escondido na setinha ^)."
	if st == stOK || st == stAttention {
		msg := "O servidor já está online.\n\nPainel neste computador: " + localURL()
		if a := lanAddr(); a != "" {
			msg += "\nNos outros computadores: " + a
		}
		msgInfo(msg + "\n\n" + statusText(st, detail) + "\n\n" + where)
		return
	}
	msgInfo("O ArendCalls já está aberto.\n\nEstado: " + statusText(st, detail) +
		"\nUse o menu do ícone (clique direito) para iniciar ou reiniciar o servidor.\n\n" + where)
}

type tray struct {
	icons map[trayState][]byte

	mu      sync.Mutex
	state   trayState
	detail  string
	lastErr string    // último erro de consulta registrado (evita repetir a cada 4s)
	upSince time.Time // serviço visto RUNNING com o servidor ainda sem responder

	mStatus, mAddr, mOpen, mRestart, mStart, mStop, mInstall, mConfig, mLog, mLauncherLog, mFolder, mQuit, mExit *systray.MenuItem
}

func onTrayReady() {
	t := &tray{icons: map[trayState][]byte{}, state: -1}
	logf("bandeja: ícone pronto, painel %s", localURL())
	for st, info := range stateInfo {
		t.icons[st] = icon.ICO(icon.Render(16, info.color), icon.Render(20, info.color),
			icon.Render(24, info.color), icon.Render(32, info.color))
	}

	systray.SetTooltip(displayName)
	t.mStatus = systray.AddMenuItem("Verificando…", "")
	t.mStatus.Disable()
	t.mAddr = systray.AddMenuItem("", "Endereço para abrir nos outros computadores")
	t.mAddr.Disable()
	t.mAddr.Hide()
	systray.AddSeparator()
	t.mOpen = systray.AddMenuItem("Abrir painel do ArendCalls", "Abre no navegador")
	systray.AddSeparator()
	t.mRestart = systray.AddMenuItem("Reiniciar servidor", "Requer administrador; aplica mudanças do .env")
	t.mStart = systray.AddMenuItem("Iniciar servidor", "Requer administrador")
	t.mStop = systray.AddMenuItem("Parar servidor", "Requer administrador")
	t.mInstall = systray.AddMenuItem("Instalar serviço…", "Instala o servidor para iniciar com o Windows")
	systray.AddSeparator()
	t.mConfig = systray.AddMenuItem("Editar configuração (.env)", "Requer administrador; depois de salvar: Reiniciar servidor")
	t.mLog = systray.AddMenuItem("Ver log do servidor", "")
	t.mLauncherLog = systray.AddMenuItem("Ver log da bandeja", "Ícone, instalação e comandos")
	t.mFolder = systray.AddMenuItem("Abrir pasta do programa", "")
	systray.AddSeparator()
	t.mQuit = systray.AddMenuItem("Fechar só o ícone (o servidor continua)", "Só some da bandeja; o serviço segue rodando")
	t.mExit = systray.AddMenuItem("Encerrar tudo (parar servidor e fechar)", "Requer administrador")

	t.apply(stUnknown, "")
	systray.SetOnTapped(t.openApp) // clique esquerdo abre; direito mostra o menu

	go t.handleClicks()
	go t.watchQuitEvent()
	go t.poll()
}

func (t *tray) openApp() { logErr("abrindo navegador", shellOpen(localURL())) }

func (t *tray) handleClicks() {
	for {
		select {
		case <-t.mOpen.ClickedCh:
			t.openApp()
		case <-t.mRestart.ClickedCh:
			t.serviceAction("restart", stStarting)
		case <-t.mStart.ClickedCh:
			t.serviceAction("start", stStarting)
		case <-t.mStop.ClickedCh:
			if msgAsk("Parar o servidor?\n\nAs contas do WhatsApp desconectam e nenhuma chamada " +
				"é feita ou recebida até iniciar de novo.") {
				t.serviceAction("stop", stUnknown)
			}
		case <-t.mInstall.ClickedCh:
			t.serviceAction("install", stUnknown)
		case <-t.mConfig.ClickedCh:
			t.editConfig()
		case <-t.mLog.ClickedCh:
			log := filepath.Join(appDir(), "logs", "servidor.log")
			if _, err := os.Stat(log); err != nil {
				msgInfo("Ainda não há log. Ele é criado quando o serviço inicia:\n" + log)
				continue
			}
			logErr("abrindo log do servidor", shellOpen(log))
		case <-t.mLauncherLog.ClickedCh:
			logErr("abrindo log da bandeja", shellOpen(launcherLogPath()))
		case <-t.mFolder.ClickedCh:
			logErr("abrindo pasta", shellOpen(appDir()))
		case <-t.mQuit.ClickedCh:
			logf("bandeja: \"Fechar só o ícone\" clicado")
			systray.Quit()
			return
		case <-t.mExit.ClickedCh:
			t.exitAll()
		}
	}
}

// editConfig abre o .env no Bloco de Notas (.env não tem programa associado).
// Como administrador: a pasta do programa só é gravável por administradores
// (ver lockDownFolder).
func (t *tray) editConfig() {
	path := envPath()
	if _, err := os.Stat(path); err != nil {
		msgInfo("Ainda não há .env. Ele é criado ao instalar o serviço (menu > Instalar serviço).\n" + path)
		return
	}
	err := windows.ShellExecute(0, u16("runas"), u16("notepad.exe"), u16(syscall.EscapeArg(path)), nil, swShowNormal)
	if err != nil && !isCancelled(err) {
		msgError("Não foi possível abrir o .env como administrador:\n" + err.Error())
	}
}

// watchQuitEvent fecha a bandeja quando "encerrar"/uninstall sinaliza.
// Reset automático: o sinal é consumido aqui e não fica pendurado fechando a próxima bandeja.
func (t *tray) watchQuitEvent() {
	h, err := windows.CreateEvent(nil, 0, 0, u16(trayQuitEvt))
	if h == 0 {
		logf("evento de saída indisponível: %v", err)
		return
	}
	defer windows.CloseHandle(h)
	r, err := windows.WaitForSingleObject(h, windows.INFINITE)
	if r != windows.WAIT_OBJECT_0 {
		logf("evento de saída: espera falhou (%#x, %v); bandeja continua", r, err)
		return
	}
	logf("bandeja: pedido de saída recebido (encerrar/desinstalar)")
	systray.Quit()
}

func (t *tray) poll() {
	client := &http.Client{Timeout: 5 * time.Second}
	askedInstall, checkedPath, triedStart := false, false, false
	for {
		st, detail := t.check(client)
		t.apply(st, detail)
		// Antes de tentar iniciar: se o serviço aponta para outra pasta, iniciar
		// não adianta; o install oferecido aqui já sobe o servidor.
		if st != stNotInstalled && st != stUnknown && !checkedPath {
			checkedPath = true
			if t.offerRelocate() {
				triedStart = true
			}
		}
		// Aberto com duplo clique e servidor parado (ex.: depois de "Encerrar tudo"): sobe.
		if st == stStopped && openedByUser && !triedStart {
			triedStart = true
			t.serviceAction("start", stStarting)
		}
		if st == stNotInstalled && !askedInstall {
			askedInstall = true
			if msgAsk("O servidor do ArendCalls ainda não está instalado neste computador.\n\n" +
				"Instalar agora? Ele passará a iniciar sozinho junto com o Windows, " +
				"mesmo sem ninguém logado.\n(Será pedida permissão de administrador.)") {
				t.serviceAction("install", stUnknown)
			}
		}
		time.Sleep(4 * time.Second)
	}
}

// check consulta serviço → servidor → contas. detail complementa o texto do
// estado ("2/3 contas conectadas").
func (t *tray) check(client *http.Client) (trayState, string) {
	state, installed, err := queryServiceState()
	switch {
	case err != nil:
		t.noteErr("consulta do serviço", err)
		return stUnknown, ""
	case !installed:
		return stNotInstalled, ""
	case state == windows.SERVICE_START_PENDING:
		t.setUpSince(false)
		return stStarting, ""
	case state != windows.SERVICE_RUNNING:
		t.setUpSince(false)
		return stStopped, ""
	}

	// Rota pública: só confirma que o HTTP está no ar.
	resp, err := client.Get(probeURL("/api/auth/status"))
	if err != nil {
		t.noteErr("servidor", err)
		// Serviço acabou de subir, servidor ainda carregando (restaura contas,
		// consulta a versão do WhatsApp). Passou do prazo: não está subindo.
		if since := t.setUpSince(true); time.Since(since) < startGrace {
			return stStarting, ""
		}
		return stNoResponse, ""
	}
	resp.Body.Close()
	t.setUpSince(false)
	if resp.StatusCode != http.StatusOK {
		t.noteErr("servidor", fmt.Errorf("HTTP %d", resp.StatusCode))
		return stNoResponse, ""
	}

	open, total, attention, err := t.accounts(client)
	if err != nil {
		t.noteErr("contas", err)
		return stOK, "" // servidor no ar; sem API_KEY válida não dá para ver as contas
	}
	t.noteErr("", nil)
	switch {
	case total == 0:
		return stOK, "nenhuma conta"
	case attention > 0:
		return stAttention, fmt.Sprintf("%d/%d contas conectadas", open, total)
	default:
		return stOK, fmt.Sprintf("%d/%d contas conectadas", open, total)
	}
}

// startGrace: tempo que o servidor tem para responder depois de o serviço subir.
const startGrace = 60 * time.Second

// setUpSince marca (on) ou limpa o início da espera pelo servidor e devolve o
// instante marcado.
func (t *tray) setUpSince(on bool) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case !on:
		t.upSince = time.Time{}
	case t.upSince.IsZero():
		t.upSince = time.Now()
	}
	return t.upSince
}

// accounts lista as contas do WhatsApp com a API_KEY do .env. attention conta as
// que precisam de alguém (deslogada, substituída por outro aparelho, esperando QR).
func (t *tray) accounts(client *http.Client) (open, total, attention int, err error) {
	key := readEnv()["API_KEY"]
	if key == "" || key == apiKeyDefault {
		return 0, 0, 0, errors.New("API_KEY não definida no .env")
	}
	req, _ := http.NewRequest(http.MethodGet, probeURL("/api/sessions"), nil)
	req.Header.Set("X-Api-Key", key)
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, 0, fmt.Errorf("/api/sessions: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Sessions []struct {
			State string `json:"state"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, 0, fmt.Errorf("/api/sessions: %w", err)
	}
	for _, s := range body.Sessions {
		total++
		switch s.State {
		case "open":
			open++
		case "logged_out", "replaced", "qr":
			attention++
		}
	}
	return open, total, attention, nil
}

// noteErr registra o erro só quando muda (a consulta roda a cada 4s).
func (t *tray) noteErr(what string, err error) {
	msg := ""
	if err != nil {
		msg = what + ": " + err.Error()
	}
	t.mu.Lock()
	changed := msg != t.lastErr
	t.lastErr = msg
	t.mu.Unlock()
	if changed && msg != "" {
		logf("%s", msg)
	}
}

func statusText(st trayState, detail string) string {
	text := stateInfo[st].text
	if st == stOK && detail != "" {
		text += " · " + detail
	} else if st == stAttention && detail != "" {
		text = "Servidor no ar · " + detail
	}
	return text
}

// lanAddr: endereço para os outros computadores ("" se ADDR é só local).
func lanAddr() string {
	ip := lanIP()
	if ip == "" || !listensOnLAN() {
		return ""
	}
	_, port := addrParts(listenAddr())
	return fmt.Sprintf("http://%s:%d", ip, port)
}

func (t *tray) apply(st trayState, detail string) {
	t.mu.Lock()
	changed := st != t.state || detail != t.detail
	t.state, t.detail = st, detail
	t.mu.Unlock()
	if !changed {
		return
	}
	text := statusText(st, detail)
	logf("estado: %s", text)
	systray.SetIcon(t.icons[st])
	systray.SetTooltip(displayName + " — " + text)
	t.mStatus.SetTitle("● " + text)

	up := st == stOK || st == stAttention
	if a := lanAddr(); up && a != "" {
		t.mAddr.SetTitle("Na rede: " + a)
		t.mAddr.Show()
	} else {
		t.mAddr.Hide()
	}

	installed := st != stNotInstalled && st != stUnknown
	running := installed && st != stStopped
	showIf(t.mInstall, st == stNotInstalled)
	showIf(t.mRestart, running)
	showIf(t.mStop, running)
	showIf(t.mStart, st == stStopped)
	if up {
		t.mOpen.Enable()
	} else {
		t.mOpen.Disable()
	}
}

func showIf(m *systray.MenuItem, v bool) {
	if v {
		m.Show()
	} else {
		m.Hide()
	}
}
