# Plano de Ação

- Baixar a última versão do WaCalls do GitHub Releases (Concluído).
- Analisar a estrutura do projeto e preparar ambiente (Concluído).
- Implementar tradução pt-BR, tela de API e fluxo de renomeação de contas (Concluído).
- Simplificar arquitetura de chaves de API: remover chaves por conta e adotar chave única de Super-Usuário (`API_KEY`) (Concluído).
- Ajustar restauração de sessões no backend (`SessionManager.Restore`) para **nunca apagar instâncias despareadas do banco de dados**, mantendo-as em estado `logged_out` para preservar o ID de integração `{sid}` (Concluído).
- Criar suíte abrangente de testes de integração HTTP da API local (`cmd/server/httpapi_test.go`) (Concluído).
- Testar reinicialização do servidor e validar preservação de instâncias (Concluído).
- Corrigir distinção entre "Ligando..." (`starting`) e "Chamando..." (`ringing`):
  - Em `internal/voip/call/callstate.go`, a transição `TransitionOfferSent` agora mantém o estado em `core.CallStateInitiating` (`starting` / "Ligando...").
  - O estado só avança para `core.CallStateRinging` (`ringing` / "Chamando...") quando o WhatsApp entregar a notificação/toque no aparelho do destinatário e disparar o evento de recibo `ringer`/`delivered` (`TransitionRingingReceived`).
  - Suporte a aceitar/rejeitar chamadas diretamente tanto em `CallStateInitiating` quanto em `CallStateRinging`.
  - Atualização dos testes da máquina de estados em `foundation_test.go` e validação com `go test -v ./...` (Concluído).
- Gerar nova versão da imagem Docker `nathanarend/arendcalls:v2026.10` e `latest` e publicar no DockerHub com as correções de estados de chamada (Concluído).

## Nota sobre Resolução de JID e Status "Chamando" (Ringing)
**IMPORTANTE PARA ATUALIZAÇÕES FUTURAS:** 
O WhatsApp alterou a forma como envia os recibos de chamada (`events.Receipt`). O evento de `ringer` (quando o celular do destinatário começa a tocar) agora chega no backend com o `evt.Sender` no formato `@lid` (User ID interno do WhatsApp), enquanto a nossa chamada no servidor é registrada utilizando o JID do telefone (`@s.whatsapp.net`).

Para garantir que o status avance corretamente de "Ligando..." para "Chamando...", o código em `cmd/server/session.go` (no case `events.Receipt`) **deve obrigatoriamente** traduzir o `@lid` para o formato de número de telefone (usando `s.client.Store.LIDs.GetPNForLID`) antes de buscar a chamada ativa no `callRegistry`. Se essa conversão for removida em atualizações futuras, o sistema não encontrará a chamada na memória e o painel/CRM ficará preso eternamente no status "Ligando...".


- Implementar Hold com música de espera sintetizada em PCM 16kHz e suporte a troca WebRTC sem queda de chamada no WhatsApp.
- Gerar nova imagem Docker nathanarend/arendcalls:v2026.12 e latest e publicar no DockerHub (Concluído).
- Implementar painel sutil de métricas e recursos da VPS em tempo real sob demanda:
  - Backend: endpoint `GET /api/system/metrics` lendo métricas instantâneas (RAM do processo, RAM da VPS, CPU Load, Goroutines, Uptime, Disco e Chamadas ativas) sem overhead e sem persistência em banco.
  - Frontend: botão sutil no cabeçalho/sidebar com modal dinâmico que ativa polling de 2s somente quando aberto e encerra imediatamente ao fechar.
  - Testes e validação local com build e inicialização do servidor (Concluído).
- Otimização de Performance, Eliminação de Cortes de Áudio, Correção de Desligamento e Chamadas Zumbis:
  - Correção Crítica no `EndCall`/`RejectCall`: enviar stanzas de encerramento (`terminate`/`reject`) de forma síncrona com `context.WithTimeout` desacoplado da requisição HTTP antes de `cleanupMedia()`, garantindo que o WhatsApp remoto desligue imediatamente (Concluído).
  - Correção de Chamadas Travadas/Zumbis: resolução de JID/LID em eventos `CallTerminate`/`CallReject`, timeouts automáticos de chamada (60s) e encerramento ao fechar os Relays ICE (Concluído).
  - Otimização do Codec MLow (Go): implementar tabelas twiddle pré-computadas e scratch no `fft.go` (redução de 65% no uso de CPU e eliminação de alocações na hot path) (Concluído).
  - Jitter Buffer no Navegador: implementar pré-buffer adaptativo de 50ms e smoothing no `playback-processor.js` para evitar cortes por variações de rede (Concluído).
  - Fila estrita de 20ms (320 samples) na captura do microfone e isolamento do feedback de áudio no cliente (Concluído).
  - Telemetria de CPU do Processo: medição diferencial instantânea de tempo de CPU (`/proc/self/stat`) normalizada pelo total de núcleos da VPS (ex: 3.8% do host total de 8 cores) exibida em tempo real no modal de métricas (Concluído).
  - Bloqueio de Auto-Chamada Inteligente: suporte completo ao 9º dígito brasileiro (comparação de DDD + 8 dígitos finais) tanto no Frontend (bloqueio imediato com toast) quanto no Backend antes e após a resolução de JID canônico da Meta (Concluído).
  - UI/UX: Design moderno e arredondado (`rounded-full`) para os botões de ação da sessão (Ligar, Parar, Reiniciar, Desconectar e Histórico) com micro-interações (Concluído).
