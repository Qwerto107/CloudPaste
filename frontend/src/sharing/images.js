// 只嵌入不会执行脚本的位图，格式和大小检查同时用于编辑器与预览过滤。
export const embeddedImageLimit = 1024 * 1024;
const imageData =
  /^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/]+={0,2}$/;

export function safeEmbeddedImage(value) {
  return (
    value.length <= Math.ceil(embeddedImageLimit / 3) * 4 + 32 &&
    imageData.test(value)
  );
}

export async function embeddedImage(file) {
  if (!/^image\/(png|jpeg|gif|webp)$/.test(file.type))
    throw new Error("只支持 PNG、JPEG、GIF 和 WebP 图片");
  if (!file.size || file.size > embeddedImageLimit)
    throw new Error("图片不能为空且不能超过 1 MiB，请压缩后插入");
  const bytes = new Uint8Array(await file.arrayBuffer());
  const signature = Array.from(bytes.subarray(0, 12));
  const matches = {
    "image/png": signature.slice(0, 8).join() === "137,80,78,71,13,10,26,10",
    "image/jpeg":
      signature[0] === 255 && signature[1] === 216 && signature[2] === 255,
    "image/gif": new TextDecoder()
      .decode(bytes.subarray(0, 6))
      .match(/^GIF8[79]a$/),
    "image/webp":
      new TextDecoder().decode(bytes.subarray(0, 4)) === "RIFF" &&
      new TextDecoder().decode(bytes.subarray(8, 12)) === "WEBP",
  };
  if (!matches[file.type]) throw new Error("图片内容与格式不一致");
  // 分块编码避免大数组展开超过调用栈；总内存由单张图片上限约束。
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 8192)
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
  return `data:${file.type};base64,${btoa(binary)}`;
}
