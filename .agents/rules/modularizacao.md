---
description: "Diretrizes de modularização do frontend (React + Vite) e separação de responsabilidades."
trigger: "always"
---

# Diretrizes de Modularização do Projeto

*   **Páginas Leves (Orquestradores):** Os arquivos de rotas e páginas (`client/src/pages/**/*.tsx`) devem atuar apenas como orquestradores leves e concisos, focados em interligar a lógica de estado/hooks com a renderização.
*   **Organização de Componentes:** Todo o layout, formulários, modais, tabelas e cards devem ser extraídos e isolados em componentes desacoplados. Eles devem ser organizados estruturalmente dentro de `client/src/components/`, utilizando divisões lógicas como `ui/` (componentes base), `layout/` (estruturas de tela), `form/` (campos), `domain/` (lógicas de negócio) e `shared/`.
*   **Sem Arquivos Monolíticos:** É terminantemente proibido criar arquivos monolíticos (seja em `pages/` ou componentes soltos na raiz de `components/`) que concentrem muita lógica, chamadas de API, modais e layouts no mesmo lugar. A separação de responsabilidades é obrigatória.
*   **Desacoplamento de Lógica:** Lógicas complexas, requisições de API e gerenciamento de estado global devem ser extraídos para hooks customizados (`client/src/hooks/`), serviços (`client/src/services/`) e stores (`client/src/stores/`).
*   **Tipagens e Contratos:** Centralize interfaces e tipos do TypeScript na pasta `client/src/types/`, evitando mantê-los acoplados rigidamente ao início dos arquivos de componentes quando forem compartilhados por várias partes da aplicação.
*   **Utilitários e Constantes:** Funções genéricas puras (formatação, cálculos) devem residir em `client/src/utils/`. Valores fixos, mensagens padronizadas e mapeamentos devem ficar em `client/src/constants/`.
*   **Bibliotecas e Configurações:** Configurações de dependências externas (como instâncias do Axios, conectores WebRTC) devem ser centralizadas em `client/src/lib/`.
*   **Padrão de Nomenclatura:** Utilize `PascalCase` para componentes React e arquivos em `pages/` e `components/` (ex: `CallsPage.tsx`). Utilize `camelCase` para arquivos de utilitários, constantes, hooks e serviços (ex: `useCallStatus.ts`).
