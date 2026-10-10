import { defineConfig, loadEnv } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";
// 本站仅打包需要的页面；Markdown 按需加载，不生成全量资源预缓存。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    plugins: [vue()],
    resolve: {
      alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
    },
    define: {
      __APP_VERSION__: JSON.stringify("0.4.1"),
      __APP_ENV__: JSON.stringify(env.VITE_APP_ENV || "production"),
      __BACKEND_URL__: JSON.stringify(env.VITE_BACKEND_URL || ""),
    },
    server: { port: 3000, proxy: { "/api": "http://127.0.0.1:8787" } },
    build: {
      target: "es2022",
      minify: "terser",
      rollupOptions: {
        output: { manualChunks: { vue: ["vue", "vue-router"] } },
      },
    },
  };
});
