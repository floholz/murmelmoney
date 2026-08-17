import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { VitePWA } from 'vite-plugin-pwa'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    svelte(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['favicon.svg', 'logo.svg', 'apple-touch-icon.png'],
      manifest: {
        name: 'murmelmoney',
        short_name: 'murmelmoney',
        description: 'Minimal self-hosted personal finance tracker',
        lang: 'en',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        orientation: 'portrait-primary',
        background_color: '#fafaf0',
        theme_color: '#ffea00',
        icons: [
          { src: 'pwa-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'pwa-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'pwa-maskable-192.png', sizes: '192x192', type: 'image/png', purpose: 'maskable' },
          { src: 'pwa-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // precache the built UI only; API + PocketBase admin always go to the network
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
        navigateFallback: 'index.html',
        navigateFallbackDenylist: [/^\/api\//, /^\/_\//],
        runtimeCaching: [],
      },
    }),
  ],
  server: {
    // during `npm run dev` proxy the API to the Go binary (`go run . serve`)
    proxy: { '/api': 'http://127.0.0.1:8070', '/_': 'http://127.0.0.1:8070' },
  },
})
