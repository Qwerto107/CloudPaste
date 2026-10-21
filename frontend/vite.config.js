import { defineConfig, loadEnv } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";
// 本站仅打包需要的页面；Markdown 按需加载，不生成全量资源预缓存。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  return {
    plugins: [vue()],
    // 部署服务器为每次 HTML 响应替换标记，仅授权本页组件生成的样式。
    html: { cspNonce: "__CLOUDPASTE_STYLE_NONCE__" },
    resolve: {
      alias: [
        {
          find: "@",
          replacement: fileURLToPath(new URL("./src", import.meta.url)),
        },
        // 编辑器默认的图片缩放依赖会注入内联样式，改用纯脚本并静态加载 CSS。
        {
          find: /^medium-zoom$/,
          replacement: "medium-zoom/dist/pure/index.js",
        },
      ],
    },
    define: {
      __APP_VERSION__: JSON.stringify("0.8.11"),
      __APP_ENV__: JSON.stringify(env.VITE_APP_ENV || "production"),
      __BACKEND_URL__: JSON.stringify(env.VITE_BACKEND_URL || ""),
    },
    server: {
      port: 3000,
      proxy: { "/api": { target: "http://127.0.0.1:8787", ws: true } },
    },
    build: {
      target: "es2022",
      minify: "terser",
      rollupOptions: {
        output: { manualChunks: { vue: ["vue", "vue-router"] } },
      },
    },
  };
});
