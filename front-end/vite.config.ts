/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// https://vitejs.dev/config/
export default defineConfig({
  base: "/", // 配置基础路径，如果部署在子目录下（如 /app/），请修改此处
  plugins: [react(), tailwindcss()],
  server: {
    host: true,
    // 这里就是代理配置
    proxy: {
      "/api": {
        target: "http://localhost:8080", // 后端地址 (Docker 跑起来的端口)
        changeOrigin: true, // 允许跨域
      },
    },
  },
  test: {
    globals: true,
  },
});
