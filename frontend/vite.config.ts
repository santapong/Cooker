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
    rollupOptions: {
      output: {
        // Split vendor libraries into stable, independently-cacheable
        // chunks so that a patch to one dep doesn't bust the cache for
        // the others.  @xyflow/react goes into its own chunk so that
        // PipelinesPage (the list, which doesn't render a canvas) never
        // pulls the ~150 KB canvas library.  Only PipelineEditorPage,
        // RunPage, and ComposePage import the canvas components and will
        // therefore pull this chunk lazily.
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          xyflow: ['@xyflow/react'],
          oidc: ['oidc-client-ts'],
          zustand: ['zustand'],
        },
      },
    },
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
