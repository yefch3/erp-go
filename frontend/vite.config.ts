import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { isLiveEventsRequest } from './viteProxyPolicy.ts'

// /api 代理到 gateway：开发期前端与后端同源，无需 CORS。
export default defineConfig({
  plugins: [vue()],
  server: {
    // 默认还是 5173（本地 npm run dev 的习惯不变），但允许用 PORT 覆盖：
    // 同一台机器上要开第二个 dev server 时（比如一边留着自己的，一边让
    // 工具另起一个），5173 已经被占着，写死就起不来。没有 strictPort，
    // 所以就算不给 PORT，vite 也会自己顺延到下一个空端口。
    port: Number(process.env.PORT) || 5173,
    proxy: {
      '/api': {
        target: process.env.VITE_GATEWAY_URL || 'http://localhost:8080',
        changeOrigin: true,
        configure(proxy) {
          // The SSE feed is optional, best-effort UI freshness. During local
          // startup Vite can be ready before the gateway; http-proxy otherwise
          // turns every reconnect into a browser-visible 500 even though
          // live.ts is deliberately retrying with backoff. Convert only this
          // upstream connection failure to an empty success. Vite's own error
          // handler sees the response has ended and leaves it alone.
          //
          // Do not apply this to normal REST calls: a missing gateway must
          // remain visible when it prevents a real page operation.
          proxy.on('error', (_error, request, response) => {
            if (!response || !('req' in response)) return
            if (!isLiveEventsRequest(request.url)) return
            if (response.headersSent || response.writableEnded) return
            response.writeHead(204, {
              'Cache-Control': 'no-store',
              'Retry-After': '1',
            }).end()
          })
        },
      },
    },
  },
})
