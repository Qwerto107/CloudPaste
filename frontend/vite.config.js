import { defineConfig, loadEnv } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";
import { execSync } from "node:child_process";
// 控制台版本标识使用的构建日期与提交号；CI 优先读取 GITHUB_SHA。
function buildInfo() {
  const now = new Date();
  const pad = (value) => String(value).padStart(2, "0");
  const date = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`;
  // Docker 构建上下文不含 .git，由工作流通过 APP_COMMIT 传入提交号。
  let commit = (process.env.APP_COMMIT || process.env.GITHUB_SHA || "").slice(0, 7);
  if (!commit)
    try {
      commit = execSync("git rev-parse --short=7 HEAD", { stdio: ["ignore", "pipe", "ignore"] })
        .toString()
        .trim();
    } catch {
      commit = "unknown";
    }
  return { date, commit };
}
// 本站仅打包需要的页面；Markdown 按需加载，不生成全量资源预缓存。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const build = buildInfo();
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
      __APP_VERSION__: JSON.stringify("1.0.8"),
      __APP_COMMIT__: JSON.stringify(build.commit),
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
