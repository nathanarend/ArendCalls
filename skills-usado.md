# Skills Utilizados

- Desenvolvimento e testes executados utilizando ferramentas nativas Go (`go test -v ./...`) para verificação de rotas HTTP em `httptest.Server`, validação de autenticação, estado de chamadas e preservação de instâncias em `SessionManager.Restore`.
- Validação prática de persistência do banco de dados SQLite via cURL e reinicialização de processos.
- Compilação e tipagem do Frontend com ferramentas Node/NPM (`npm run build`).
- Compilação multi-stage de imagens Docker (`docker build`) e publicação no DockerHub (`docker push`) das tags `v2026.10` e `latest`.
- Análise aprofundada da máquina de estados VoIP e ciclo de vida de sinalização do WhatsApp (distinção entre `TransitionOfferSent` -> `CallStateInitiating` ["Ligando..."] e `TransitionRingingReceived` / recibos de ringer -> `CallStateRinging` ["Chamando..."]).
- Implementação de telemetria sob demanda em Go (`runtime.MemStats`, `runtime.NumGoroutine`, leitura de `/proc/meminfo`, `/proc/loadavg` e `syscall.Statfs`) para zero overhead em background.
- Desenvolvimento de interface reativa em React com ciclo de vida atrelado ao estado de visibilidade de modais, garantindo cancelamento de polling ao fechar.
- Análise de profiling de CPU no codec MLow (FFT trigonometria e twiddle tables) e engenharia de áudio para Jitter Buffer em AudioWorklet.
- Cálculo de amostragem diferencial de tempo de CPU por processo via `/proc/self/stat` (`utime` + `stime`) no Linux.
