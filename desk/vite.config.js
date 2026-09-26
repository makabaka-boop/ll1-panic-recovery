import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发态把 /api 代理到 grammar 容器（compose 服务名 grammar:8080）；
// 生产镜像中由 desk 内嵌的静态服务器把 /api 反代到 grammar:8080。
export default defineConfig({
  plugins: [vue()],
  server: {
    host: '0.0.0.0',
    port: 5173,
    proxy: {
      '/api': {
        target: process.env.GRAMMAR_URL || 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
