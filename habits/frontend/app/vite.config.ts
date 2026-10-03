import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig(({ command }) => ({
  // В проде приложение живёт под telegram.resager.ru/app/habits/
  base: command === 'build' ? '/app/habits/' : '/',
  plugins: [vue()],
  server: {
    proxy: {
      // ws: true — терминальная консоль ходит по WebSocket через тот же префикс
      '/api': { target: 'http://localhost:8081', ws: true },
      '/uploads': 'http://localhost:8081',
    },
  },
}))
