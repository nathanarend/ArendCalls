//go:build windows

package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Configuração do servidor: .env na pasta do programa, o mesmo formato do
// iniciar.sh (CHAVE=valor, sem aspas, # comenta). Leitura literal: o hash
// bcrypt de ADMIN_PASSWORD_HASH tem "$" e não pode ser expandido.

const (
	defaultAddr    = "127.0.0.1:8080"
	apiKeyDefault  = "troque_esta_chave"
	envFileName    = ".env"
	envExampleName = ".env.example"
)

func envPath() string { return filepath.Join(appDir(), envFileName) }

// readEnv devolve as chaves do .env (vazio se não existir).
func readEnv() map[string]string {
	vals := map[string]string{}
	f, err := os.Open(envPath())
	if err != nil {
		return vals
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\xEF\xBB\xBF") // Bloco de Notas salva com BOM
			first = false
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return vals
}

// listenAddr: ADDR do .env (padrão 127.0.0.1:8080).
func listenAddr() string { return addrOf(readEnv()) }

// addrOf: ADDR de um .env já lido.
func addrOf(env map[string]string) string {
	if a := env["ADDR"]; a != "" {
		return a
	}
	return defaultAddr
}

// addrParts separa host e porta de ADDR (":8080" → "", 8080).
func addrParts(addr string) (host string, port int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 8080
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > 65535 {
		n = 8080
	}
	return h, n
}

// localURL: endereço do painel nesta máquina.
func localURL() string {
	host, port := addrParts(listenAddr())
	switch host {
	case "", "0.0.0.0", "::", "127.0.0.1", "localhost":
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

// probeURL: endereço que a bandeja consulta (sempre local).
func probeURL(path string) string {
	host, port := addrParts(listenAddr())
	switch host {
	case "", "0.0.0.0", "localhost":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + path
}

// listensOnLAN: ADDR aceita conexões de outras máquinas (precisa de firewall).
func listensOnLAN() bool {
	host, _ := addrParts(listenAddr())
	ip := net.ParseIP(host)
	return host != "localhost" && (ip == nil || !ip.IsLoopback())
}

// ensureEnv garante um .env utilizável: cria a partir do .env.example e troca
// a API_KEY de exemplo por uma aleatória. Devolve true se gerou a chave agora.
func ensureEnv() (bool, error) {
	path := envPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(appDir(), envExampleName))
		if err != nil {
			return false, fmt.Errorf("%s e %s não encontrados em %s.\nExtraia o pacote completo antes de instalar.",
				envFileName, envExampleName, appDir())
		}
	} else if err != nil {
		return false, fmt.Errorf("lendo %s: %w", path, err)
	}

	lines := bytes.Split(data, []byte("\n"))
	replaced := false
	for i, l := range lines {
		k, v, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(string(l), "\xEF\xBB\xBF")), "=")
		if ok && strings.TrimSpace(k) == "API_KEY" && (strings.TrimSpace(v) == apiKeyDefault || strings.TrimSpace(v) == "") {
			key, err := randomKey()
			if err != nil {
				return false, err
			}
			lines[i] = []byte("API_KEY=" + key + "\r")
			replaced = true
		}
	}
	_, statErr := os.Stat(path)
	if !replaced && statErr == nil {
		return false, nil
	}
	if err := os.WriteFile(path, bytes.Join(lines, []byte("\n")), 0o600); err != nil {
		return false, fmt.Errorf("gravando %s: %w", path, err)
	}
	logf("env: %s gravado (API_KEY gerada=%v)", path, replaced)
	return replaced, nil
}

func randomKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("gerando API_KEY: %w", err)
	}
	return hex.EncodeToString(b), nil
}
