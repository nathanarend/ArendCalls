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
- Resolução de concorrência de sinalização em WhatsApp Multi-Device (desativação de timers anti-zombie ao conectar DataChannel WebRTC em instâncias espelho e verificação global de estado de chamada no `broker`).
- Correção no protocolo de sinalização WhatsApp VoIP: uso de `m.sock.SendNode` para stanzas assíncronas de terminate e reject (`<call><terminate/></call>` e `<call><reject/></call>`), evitando o bloqueio por timeout do `m.sock.Query`.
- Compilação e publicação multi-stage de imagem Docker `nathanarend/arendcalls:v2026.18` e `latest` no DockerHub.
- Versionamento Semântico e documentação de Release Notes (`RELEASE_NOTES-v2026.18.md`) para publicação da tag `v2026.18` no GitHub.
- Tratamento de falhas transitórias em sinalização VoIP: implementação de retentativas com intervalo (exponential backoff / retry) e propagação correta de status HTTP (`404` para inexistente, `502` para falha de envio de sinalização, `204/200` para sucesso).
- Resolução de conflitos de concorrência em ambiente multi-operador com validação de `X-Client-Id` obrigatório em endpoints de claim/accept.
- Robustez e resiliência na máquina de estados de chamadas do WhatsApp: idempotência em `TransitionRemoteAccepted` e resolução de JID com fallback para pacotes `CallAccept` sem `call-id` explícito.
