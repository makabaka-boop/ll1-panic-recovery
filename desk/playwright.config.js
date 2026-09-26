import { defineConfig } from '@playwright/test'

// 一条 e2e 流程需要两个服务：
//   1) Go grammar 计算服务（:8080）
//   2) Vite 开发服务器承载 desk（:5173），/api 代理到 grammar
// 本机 Go 安装在非标准目录时通过 PATH 透传。
const goBin = process.env.HOME ? `${process.env.HOME}/go-sdk/go/bin` : ''

export default defineConfig({
  testDir: './e2e',
  timeout: 30000,
  reporter: [['list']],
  use: {
    baseURL: 'http://localhost:5173',
    trace: 'on-first-retry'
  },
  webServer: [
    {
      command: 'go run .',
      cwd: '../grammar',
      url: 'http://localhost:8080/healthz',
      reuseExistingServer: !process.env.CI,
      timeout: 60000,
      env: {
        ...process.env,
        PATH: goBin ? `${goBin}:${process.env.PATH}` : process.env.PATH,
        CGO_ENABLED: '0',
        PORT: '8080'
      }
    },
    {
      command: 'npm run dev',
      url: 'http://localhost:5173',
      reuseExistingServer: !process.env.CI,
      timeout: 60000
    }
  ]
})
