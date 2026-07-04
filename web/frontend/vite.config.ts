import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

const cyberUI = path.resolve(__dirname, './cyber-ui/packages')

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
      '@aspect/ui': path.resolve(cyberUI, 'ui/src'),
      '@aspect/theme': path.resolve(cyberUI, 'theme/src'),
      '@aspect/markdown': path.resolve(cyberUI, 'markdown/src'),
      '@aspect/viewer': path.resolve(cyberUI, 'viewer/src'),
      '@aspect/terminal': path.resolve(cyberUI, 'terminal/src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        ws: true,
      },
    },
  },
  build: {
    outDir: '../static',
    emptyOutDir: true,
    // Split stable vendor code into its own chunk so an app-code change doesn't
    // bust the cache for react/radix/i18next too (hashed assets get immutable
    // headers). Does not shrink first-load payload — that's what the lazy
    // terminal split and the viewer tree-shake do.
    chunkSizeWarningLimit: 900,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return
          if (/[\\/]node_modules[\\/](react|react-dom|scheduler)[\\/]/.test(id)) return 'react'
          if (id.includes('@radix-ui')) return 'radix'
          if (/i18next|react-i18next/.test(id)) return 'i18n'
          if (id.includes('lucide-react')) return 'icons'
        },
      },
    },
  },
})
