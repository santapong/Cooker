import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    // Production builds ship without sourcemaps; the prior Vite
    // default leaked the entire frontend source tree to anyone
    // with browser dev tools open. Set VITE_SOURCEMAP=true in the
    // build environment to opt back in for short-lived debug
    // builds.
    sourcemap: process.env.VITE_SOURCEMAP === 'true',
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
})
