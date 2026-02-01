import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // 这里就是代理配置
    proxy: {
      "/api": {
        target: "http://localhost:8080", // 后端地址 (Docker 跑起来的端口)
        changeOrigin: true, // 允许跨域
      },
    },
  },
});
