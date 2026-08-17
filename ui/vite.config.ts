import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  server: {
    // during `npm run dev` proxy the API to the Go binary (`go run . serve`)
    proxy: { '/api': 'http://127.0.0.1:8090', '/_': 'http://127.0.0.1:8090' },
  },
})
