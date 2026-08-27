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
