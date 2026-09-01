import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// Dev-only: proxy HTTP admin/topic reads to the local event_watch server so
// the browser sees them as same-origin (no CORS setup needed). The WS
// connection is direct — the user configures its URL in the UI.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/admin':  'http://localhost:8080',
      '/topics': 'http://localhost:8080',
      '/state':  'http://localhost:8080',
      '/events': 'http://localhost:8080',
    },
  },
});
