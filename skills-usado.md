# Skills Utilizados

- Desenvolvimento e testes executados utilizando ferramentas nativas Go (`go test -v ./...`) para verificação de rotas HTTP em `httptest.Server`, validação de autenticação, estado de chamadas e preservação de instâncias em `SessionManager.Restore`.
- Validação prática de persistência do banco de dados SQLite via cURL e reinicialização de processos.
- Compilação e tipagem do Frontend com ferramentas Node/NPM (`npm run build`).
- Compilação multi-stage de imagens Docker (`docker build`) e publicação no DockerHub (`docker push`) das tags `v2026.10` e `latest`.
- Análise aprofundada da máquina de estados VoIP e ciclo de vida de sinalização do WhatsApp (distinção entre `TransitionOfferSent` -> `CallStateInitiating` ["Ligando..."] e `TransitionRingingReceived` / recibos de ringer -> `CallStateRinging` ["Chamando..."]).
