import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { readFileSync, readdirSync } from 'node:fs'

export default defineConfig({
  plugins: [vue(), {
    name: 'include-frontend-licenses',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'vue-license.txt', source: readFileSync(new URL('./node_modules/vue/LICENSE', import.meta.url), 'utf8') })
      const lock = JSON.parse(readFileSync(new URL('./package-lock.json', import.meta.url), 'utf8')) as { packages: Record<string, { dev?: boolean; version?: string; license?: string }> }
      const notices = Object.entries(lock.packages).filter(([path, pkg]) => path && !pkg.dev).map(([path, pkg]) => {
        const directory = new URL(`./${path}/`, import.meta.url)
        const files = readdirSync(directory).filter(name => /^(license|licence|copying|notice)(\.|$)/i.test(name))
        return [path.replace(/^node_modules\//, ''), `Version: ${pkg.version}; License: ${pkg.license || 'See package notice'}`, ...files.map(name => readFileSync(new URL(name, directory), 'utf8'))].join('\n\n')
      })
      this.emitFile({ type: 'asset', fileName: 'frontend-licenses.txt', source: notices.join('\n\n' + '='.repeat(72) + '\n\n') })
    },
  }],
  build: {
    outDir: '../internal/server/web',
    emptyOutDir: true,
    target: 'es2022',
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      '/api': {
        target: process.env.PROTOWATCH_BACKEND || process.env.DDSWATCH_BACKEND || 'http://127.0.0.1:8080',
        // Keep Host and Origin together so the Go API can enforce same-origin writes.
        changeOrigin: false,
      },
    },
  },
})
