import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// The app is mounted at two paths (/widget/ and /t/{token}), so assets need one
// absolute base of their own rather than a path relative to the page.
// In production the Go binary serves index.html for /widget/ and /t/{token};
// this mirrors that fallback in the dev server, which otherwise only answers
// under the asset base.
const devRoutes = {
  name: 'freesupp-visitor-dev-routes',
  apply: 'serve' as const,
  configureServer(server: { middlewares: { use: (fn: any) => void } }) {
    server.middlewares.use((req: { url?: string }, _res: unknown, next: () => void) => {
      const url = req.url ?? '/'
      if (url === '/' || url.startsWith('/widget/') || url.startsWith('/t/')) req.url = '/visitor/'
      next()
    })
  },
}

export default defineConfig({
  base: '/visitor/',
  plugins: [vue(), devRoutes],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
      '/widget.js': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.ts'],
  },
})
