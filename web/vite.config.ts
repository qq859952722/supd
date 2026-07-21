import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import path from 'path'

// M-05-001 修复：通过 manualChunks 拆分 818KB 单 bundle
// 将 monaco-editor（约 600KB）与 react 生态拆分为独立 chunk，
// 首屏仅需加载 index + vendor，编辑器相关路由懒加载 monaco
export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          // Monaco Editor 体积大（~600KB），独立成 chunk，仅在编辑器路由加载
          monaco: [
            'monaco-editor',
            '@monaco-editor/react',
            'monaco-yaml',
          ],
          // React 生态独立成 vendor chunk，长期缓存
          vendor: [
            'react',
            'react-dom',
            'react-router',
            '@tanstack/react-query',
            'zustand',
            'zod',
          ],
        },
      },
    },
    // 调高警告阈值，避免拆分后子 chunk 仍频繁报警
    chunkSizeWarningLimit: 600,
  },
})
