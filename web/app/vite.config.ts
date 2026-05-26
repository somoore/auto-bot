import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The SPA is served under /app/* in production by the Go server.
// In dev (`npm run dev`), Vite proxies API + WebSocket traffic to the Go
// server on :3000 so the page can hit /healthz and /websocket as if it
// were same-origin.
export default defineConfig({
  base: '/app/',
  plugins: [react()],
  server: {
    proxy: {
      '/healthz': {
        target: 'http://localhost:3000',
        changeOrigin: true,
      },
      '/websocket': {
        target: 'ws://localhost:3000',
        ws: true,
        changeOrigin: true,
      },
    },
  },
})
