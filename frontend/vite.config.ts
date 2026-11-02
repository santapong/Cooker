/// <reference types="vitest" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
  build: {
    target: 'es2020',
    // Production builds ship without sourcemaps; the prior Vite
    // default leaked the entire frontend source tree to anyone
    // with browser dev tools open. Set VITE_SOURCEMAP=true in the
    // build environment to opt back in for short-lived debug
    // builds.
    sourcemap: process.env.VITE_SOURCEMAP === 'true',
    rollupOptions: {
      output: {
        // Split vendor libraries into stable, independently-cacheable
        // chunks so that a patch to one dep doesn't bust the cache for
        // the others.  @xyflow/react goes into its own chunk so that
        // PipelinesPage (the list, which doesn't render a canvas) never
        // pulls the ~150 KB canvas library.  Only PipelineEditorPage,
        // RunPage, and ComposePage import the canvas components and will
        // therefore pull this chunk lazily.
        manualChunks(id) {
          if (!id.includes('/node_modules/')) return
          const name = id.split('/node_modules/').pop()!
          if (/^(react|react-dom|react-router|react-router-dom|scheduler)\//.test(name)) return 'react'
          if (name.startsWith('@xyflow/')) return 'xyflow'
          if (name.startsWith('oidc-client-ts/')) return 'oidc'
          if (name.startsWith('zustand/')) return 'zustand'
        },
      },
    },
  },
  test: {
    // Unit tests live under src/; e2e/ holds Playwright specs (own runner).
    include: ['src/**/*.test.ts'],
    // Vitest stubs CSS imports to '' by default; the palette contrast test
    // (src/styles/tokens.test.ts) reads tokens.css via `?raw`, so let that
    // one file through Vite's asset pipeline.
    css: { include: [/tokens\.css/] },
  },
  preview: { host: '127.0.0.1' },
  server: {
    host: '127.0.0.1',
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
