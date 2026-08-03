import path from "path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

const API_TARGET = process.env.VITE_API_PROXY_TARGET ?? "http://127.0.0.1:18080"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      // 与源站一致：@/* → apps/web 根（源 tsconfig paths "@/*": ["./*"]），
      // 让迁移文件的 @/lib、@/components、@/hooks 零改动。src/ 仅放 Vite bootstrap。
      "@": path.resolve(__dirname, "."),
      "@workspace/ui": path.resolve(__dirname, "../../packages/ui/src"),
    },
  },
  server: {
    port: 8091,
    proxy: {
      // Browser calls use /api; the backend owns routes from /authn, /iam, and /oauth.
      "/api": {
        target: API_TARGET,
        changeOrigin: true,
        rewrite: (requestPath) => requestPath.replace(/^\/api/u, ""),
      },
    },
  },
})
