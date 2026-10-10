// 在页面绘制前应用主题；外部脚本兼容禁止内联脚本的 CSP。
(() => {
  let theme;
  try {
    theme = localStorage.getItem("sharing-theme");
  } catch {
    // 存储不可用时仍能跟随系统，页面中的开关也可以正常使用。
  }
  if (theme !== "dark" && theme !== "light") {
    theme = window.matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  }
  document.documentElement.dataset.theme = theme;
})();
