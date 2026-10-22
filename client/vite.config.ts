import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  build: {
    rollupOptions: {
      output: {
        // Bibliotecas fora do chunk do app: o navegador mantém em cache entre
        // deploys e nenhum chunk passa de 500 kB.
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (/node_modules\/(react|react-dom|scheduler)\//.test(id)) return "react";
          return "vendor";
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        // ws: transporte de áudio por WebSocket. O servidor exige Origin igual
        // ao Host no handshake com cookie; em dev o Host vira localhost:8080.
        ws: true,
        configure: (proxy) => {
          proxy.on("proxyReqWs", (proxyReq) => proxyReq.setHeader("Origin", "http://localhost:8080"));
        },
      },
    },
  },
});
