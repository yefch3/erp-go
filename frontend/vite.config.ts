import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// /api 代理到 gateway：开发期前端与后端同源，无需 CORS。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
