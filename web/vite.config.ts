import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// VITE_API_URL build aninda gomulur (bkz. .env.example, Dockerfile ARG).
export default defineConfig({
  plugins: [react()],
  server: { port: 5173 },
})
