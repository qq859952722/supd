import { defineConfig } from 'vitest/config'
import path from 'path'

// L-04-001: 前端测试框架（Vitest，轻量，纯逻辑单测，不引 jsdom/Playwright）
// environment=node：被测 util 均为纯函数；api-client 通过 stub global fetch/window 测试，无需 DOM。
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
    // api-client 等模块含 token/toast 副作用，单测以纯逻辑为主，禁止输出噪声
    silent: false,
    coverage: {
      provider: 'v8',
      // L-04-001: 仅统计被纳入单测的纯逻辑 util，避免拉低整体未被测 UI 比例
      include: ['src/lib/**/*.ts'],
      exclude: ['src/lib/**/*.test.ts'],
      reporter: ['text', 'text-summary'],
    },
  },
})
