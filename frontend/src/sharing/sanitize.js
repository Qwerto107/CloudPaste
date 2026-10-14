import DOMPurify from "dompurify";
import { safeEmbeddedImage } from "./images.js";

// 分享内容和站点 HTML 共用过滤规则，默认不让打开分享触发站外图片请求。
export function sanitizeHTML(value, allowExternalImages = false) {
  const body = DOMPurify.sanitize(value, {
    RETURN_DOM: true,
    USE_PROFILES: { html: true },
    FORBID_TAGS: ["style", "iframe", "form", "input", "video", "audio"],
    // 表格的旧式 background 属性也会加载图片，统一移除，避免绕过 img 的同源检查。
    FORBID_ATTR: ["style", "srcset", "ping", "background"],
  });
  for (const image of body.querySelectorAll("img")) {
    try {
      const source = image.getAttribute("src") || "";
      const url = new URL(source, window.location.origin);
      if (
        !safeEmbeddedImage(source) &&
        (!["http:", "https:"].includes(url.protocol) ||
          (!allowExternalImages && url.origin !== window.location.origin))
      ) {
        image.remove();
        continue;
      }
      image.loading = "lazy";
      image.referrerPolicy = "no-referrer";
    } catch {
      image.remove();
    }
  }
  for (const link of body.querySelectorAll("a"))
    link.setAttribute("rel", "noopener noreferrer");
  return body.innerHTML;
}
