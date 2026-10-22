import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
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
