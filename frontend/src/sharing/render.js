import { marked } from "marked";
import { sanitizeHTML } from "./sanitize.js";

export function renderMarkdown(content, allowExternalImages = false) {
  return sanitizeHTML(
    marked.parse(content, { breaks: true, gfm: true }),
    allowExternalImages,
  );
}
