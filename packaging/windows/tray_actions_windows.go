//go:build windows

package main

import "os"

// Ações da bandeja que mexem no serviço (pedem administrador).

// serviceAction executa o comando com UAC (ou direto, se a bandeja já for admin).
func (t *tray) serviceAction(cmd string, optimistic trayState) {
	if optimistic != stUnknown {
		t.apply(optimistic, "")
	}
	logf("ação %q pedida pela bandeja", cmd)
	if isElevated() {
		go func() {
			fns := map[string]func() error{"restart": restartService, "start": startService, "stop": stopService, "install": install}
			if err := fns[cmd](); err != nil {
				msgError(err.Error())
			}
		}()
		return
	}
	err := runElevated(cmd)
	logf("ação %q com UAC: %v", cmd, err)
	if err != nil && !isCancelled(err) {
		msgError("Não foi possível obter permissão de administrador:\n" + err.Error())
	}
}

// exitAll ("Encerrar tudo"): para o servidor e fecha o ícone. Com administrador faz
// aqui mesmo; sem, o processo elevado para o serviço e sinaliza o evento de saída.
func (t *tray) exitAll() {
	if !msgAsk("Encerrar o ArendCalls?\n\n" +
		"O servidor para e o ícone sai da bandeja. As contas do WhatsApp desconectam e " +
		"nenhuma chamada é feita ou recebida até abrir o programa de novo (ou reiniciar o Windows).") {
		return
	}
	if isElevated() {
		go func() {
			if err := shutdownAll(); err != nil {
				msgError("Não foi possível parar o servidor:\n" + err.Error())
			}
		}()
		return
	}
	if err := runElevated("encerrar"); err != nil && !isCancelled(err) {
		msgError("Não foi possível obter permissão de administrador:\n" + err.Error())
	}
}

// offerRelocate: o serviço aponta para outro ArendCalls.exe (pasta movida
// ou outra cópia) → oferece reinstalar a partir desta pasta. Devolve true se o
// serviço é de outra pasta (iniciar daqui não adianta).
func (t *tray) offerRelocate() bool {
	registered, other, err := serviceInOtherFolder()
	logf("caminho do serviço: %q, outra pasta=%v, erro=%v", registered, other, err)
	if !other {
		return false
	}
	var why string
	if _, err := os.Stat(registered); err != nil {
		why = "O programa mudou de pasta e o serviço ainda aponta para o local antigo, " +
			"por isso o servidor não inicia.\n\n"
	} else {
		why = "O serviço está instalado a partir de outra cópia do programa.\n\n"
	}
	if msgAsk(why +
		"Registrado: " + registered + "\n" +
		"Esta pasta: " + exePath() + "\n\n" +
		"Atualizar a instalação para usar esta pasta?\n(Será pedida permissão de administrador.)") {
		t.serviceAction("install", stUnknown)
	}
	return true
}
