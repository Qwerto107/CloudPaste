// 聊天图片单独限额；在交给图片解码器前检查像素，避免小体积超大尺寸图片占满内存。
export const chatImageLimit = 4 * 1024 * 1024;
export const chatImageHistory = 4;

export async function validateChatImage(file, type = file.type) {
  if (!file.size || file.size > chatImageLimit)
    throw new Error("聊天图片单张不能超过 4 MiB");
  if (!["image/png", "image/jpeg"].includes(type))
    throw new Error("聊天图片仅支持 PNG 和 JPEG，其他格式可作为文件发送");
  const data = new DataView(await file.arrayBuffer());
  let width = 0,
    height = 0;
  if (
    type === "image/png" &&
    data.byteLength >= 33 &&
    data.getUint32(0) === 0x89504e47 &&
    data.getUint32(4) === 0x0d0a1a0a &&
    data.getUint32(8) === 13 &&
    data.getUint32(12) === 0x49484452
  ) {
    width = data.getUint32(16);
    height = data.getUint32(20);
    // APNG 的多帧解码不受单帧像素限制，聊天预览只接收静态图片。
    for (let offset = 8; offset + 12 <= data.byteLength;) {
      const length = data.getUint32(offset);
      if (offset + length + 12 > data.byteLength)
        throw new Error("图片内容或格式无效");
      if (data.getUint32(offset + 4) === 0x6163544c)
        throw new Error("动态图片请作为文件发送");
      offset += length + 12;
    }
  } else if (
    type === "image/jpeg" &&
    data.byteLength >= 4 &&
    data.getUint16(0) === 0xffd8
  ) {
    let offset = 2;
    while (offset + 4 <= data.byteLength) {
      if (data.getUint8(offset++) !== 0xff) break;
      while (offset < data.byteLength && data.getUint8(offset) === 0xff)
        offset++;
      if (offset + 3 > data.byteLength) break;
      const marker = data.getUint8(offset++);
      if (marker === 0xda || marker === 0xd9) break;
      const length = data.getUint16(offset);
      if (length < 2 || offset + length > data.byteLength) break;
      if ([0xc0, 0xc1, 0xc2].includes(marker) && length >= 8) {
        height = data.getUint16(offset + 3);
        width = data.getUint16(offset + 5);
        break;
      }
      offset += length;
    }
  }
  if (!width || !height) throw new Error("图片内容或格式无效");
  if (width > 4096 || height > 4096 || width * height > 4 * 1024 * 1024)
    throw new Error(
      "聊天图片最多 419 万像素，长边不超过 4096；大图请作为文件发送",
    );
}
