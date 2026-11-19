//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const (
	logMaxBytes  = 10 << 20         // gira o log a cada 10 MB
	logKeep      = 5                // servidor.log.1 .. .5
	gracefulStop = 12 * time.Second // espera o servidor desconectar as contas
)

func runService() {
	dir := appDir()
	crashTo(filepath.Join(dir, "logs", "falhas.log")) // panic do próprio serviço
	logw := newRotatingLog(filepath.Join(dir, "logs", "servidor.log"))
	defer logw.Close()
	if err := svc.Run(serviceName, &handler{dir: dir, log: logw}); err != nil {
		logw.note("falha no serviço: %v", err)
	}
}

type handler struct {
	dir string
	log *rotatingLog
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	h.log.note("serviço iniciado (versão %s) em %s", version, h.dir)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		supervise(ctx, h.dir, h.log)
	}()

	// PreShutdown: no desligamento do Windows o aviso chega antes, com o prazo
	// configurado no install (o Shutdown comum dá só ~5s, pouco para fechar as
	// contas e o banco).
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown | svc.AcceptPreShutdown}
	for c := range req {
		switch c.Cmd {
		case svc.Interrogate:
			st <- c.CurrentStatus
		case svc.Stop, svc.Shutdown, svc.PreShutdown:
			st <- svc.Status{State: svc.StopPending, WaitHint: uint32((gracefulStop + 5*time.Second).Milliseconds())}
			cancel()
			select {
			case <-done:
			case <-time.After(gracefulStop + 2*time.Second):
			}
			h.log.note("serviço parado")
			return false, 0
		}
	}
	cancel()
	<-done
	return false, 0
}

// supervise mantém arendcalls-server.exe rodando até ctx acabar. Se o servidor
// cair, espera (1s, 2s, 4s... até 30s) e sobe de novo; rodou mais de 1 min = zera
// a espera. O .env é relido a cada partida: "Reiniciar servidor" aplica mudanças.
func supervise(ctx context.Context, dir string, logw *rotatingLog) {
	job := killOnCloseJob()
	if job != 0 {
		defer windows.CloseHandle(job)
	}
	server := filepath.Join(dir, serverExe)
	backoff := time.Second
	for {
		started := time.Now()
		env := readEnv()
		addr := addrOf(env)
		// A chave do exemplo é pública: com ela qualquer um usaria a API. Sem
		// chave o servidor recusa a API por chave e só o painel funciona.
		badKey := env["API_KEY"] == "" || env["API_KEY"] == apiKeyDefault
		if badKey {
			env["API_KEY"] = ""
		}
		cmd := exec.Command(server, "-addr", addr, "-exit-on-stdin-close")
		cmd.Dir = dir // wacalls.db, recordings\ e client\dist relativos à pasta do programa
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v) // repetida: vale a última (a do .env)
		}
		cmd.Stdout = logw
		cmd.Stderr = logw
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
		// Fechar o stdin = pedido de desligamento limpo (-exit-on-stdin-close):
		// o servidor desconecta as contas e fecha o banco antes de sair.
		stdin, err := cmd.StdinPipe()
		if err == nil {
			err = cmd.Start()
		}

		if err != nil {
			logw.note("não foi possível iniciar %s: %v", serverExe, err)
		} else {
			logw.note("servidor iniciado (pid %d) em %s", cmd.Process.Pid, addr)
			if badKey {
				logw.note("AVISO: API_KEY ausente ou de exemplo no .env; API por chave desligada. " +
					"Rode \"ArendCalls.exe install\" para gerar uma")
			}
			assignToJob(job, cmd.Process.Pid)
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			select {
			case <-ctx.Done():
				_ = stdin.Close()
				select {
				case err := <-exited:
					logw.note("servidor encerrou: %v", errOrOK(err))
				case <-time.After(gracefulStop):
					logw.note("servidor não encerrou em %s; finalizando à força", gracefulStop)
					_ = cmd.Process.Kill()
					<-exited
				}
				return
			case err := <-exited:
				_ = stdin.Close()
				logw.note("servidor encerrou: %v", errOrOK(err))
			}
		}

		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		logw.note("reiniciando em %s", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func errOrOK(err error) any {
	if err == nil {
		return "código 0"
	}
	return err
}

// killOnCloseJob: se o serviço morrer de qualquer jeito, o Windows derruba o
// servidor junto (nada de servidor órfão segurando a porta e o banco).
func killOnCloseJob() windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	return job
}

func assignToJob(job windows.Handle, pid int) {
	if job == 0 {
		return
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(p)
	_ = windows.AssignProcessToJobObject(job, p)
}

// rotatingLog: arquivo de log com rotação por tamanho, seguro para escrita concorrente.
type rotatingLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func newRotatingLog(path string) *rotatingLog {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	l := &rotatingLog{path: path}
	l.open()
	return l
}

func (l *rotatingLog) open() {
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		l.f = nil
		return
	}
	l.f = f
	if fi, err := f.Stat(); err == nil {
		l.size = fi.Size()
	}
	if l.size == 0 {
		n, _ := f.WriteString("\xEF\xBB\xBF") // BOM: acentos certos no Bloco de Notas/PowerShell
		l.size = int64(n)
	}
}

func (l *rotatingLog) rotate() {
	if l.f != nil {
		l.f.Close()
	}
	for i := logKeep - 1; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", l.path, i), fmt.Sprintf("%s.%d", l.path, i+1))
	}
	_ = os.Rename(l.path, l.path+".1")
	l.size = 0
	l.open()
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(p)) > logMaxBytes {
		l.rotate()
	}
	if l.f == nil {
		return len(p), nil // sem log não derruba o servidor
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}

func (l *rotatingLog) note(format string, args ...any) {
	fmt.Fprintf(l, "%s [serviço] %s\r\n", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}

func (l *rotatingLog) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return l.f.Close()
	}
	return nil
}

var _ io.WriteCloser = (*rotatingLog)(nil)
