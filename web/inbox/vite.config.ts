import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// The inbox owns "/" and "/conversations/{id}" (the link operator emails point
// at), so its assets live under a base of their own to stay clear of those
// paths. In production the Go binary serves index.html for both; this mirrors
// that fallback in the dev server, which otherwise only answers under the base.
const devRoutes = {
  name: 'freesupp-inbox-dev-routes',
  apply: 'serve' as const,
  configureServer(server: { middlewares: { use: (fn: any) => void } }) {
    server.middlewares.use((req: { url?: string }, _res: unknown, next: () => void) => {
      const url = req.url ?? '/'
      if (url === '/' || url.startsWith('/conversations')) req.url = '/inbox/'
      next()
    })
  },
}

export default defineConfig({
  base: '/inbox/',
  plugins: [vue(), tailwindcss(), devRoutes],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5174,
    proxy: {
      '/api': 'http://localhost:8080',
      '/auth': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'happy-dom',
    include: ['src/**/*.test.ts'],
  },
})
