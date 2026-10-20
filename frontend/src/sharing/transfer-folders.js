// 文件夹按需枚举并排队传输，不读取或打包整个文件夹内容。
export const folderMaxConcurrency = 128;
export const folderDefaultConcurrency = 16;
const encoder = new TextEncoder();

export function canSaveFolders() {
  return (
    globalThis.isSecureContext &&
    typeof window.showDirectoryPicker === "function"
  );
}

export function folderPath(value, allowRoot = false) {
  if (value === "" && allowRoot) return [];
  if (typeof value !== "string" || encoder.encode(value).length > 1024)
    throw new Error("文件夹路径过长或无效");
  const parts = value.split("/");
  if (
    parts.length > 16 ||
    parts.some(
      (part) =>
        !part ||
        part === "." ||
        part === ".." ||
        encoder.encode(part).length > 180 ||
        /[\\:<>"|?*\p{Cc}\u202d\u202e]/u.test(part) ||
        /[. ]$/.test(part) ||
        /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(part),
    )
  )
    throw new Error("文件夹包含不安全或当前系统不支持的路径");
  return parts;
}

// 流式遍历只持有当前层迭代器和当前 File；文件总数不占用协议的累计任务额度。
async function* walkFolder(handle, path = "") {
  let empty = true;
  for await (const child of handle.values()) {
    empty = false;
    const relative = path ? `${path}/${child.name}` : child.name;
    folderPath(relative);
    if (child.kind === "directory") yield* walkFolder(child, relative);
    else
      yield { path: relative, file: await child.getFile(), directory: false };
  }
  if (empty)
    yield {
      path,
      directory: true,
      file: new File([], path.split("/").at(-1) || handle.name),
    };
}

async function prepareFolder(name, entries, maxFile, signal) {
  if (folderPath(name).length !== 1) throw new Error("文件夹名称无效");
  let count = 0,
    size = 0;
  for await (const entry of entries()) {
    signal?.throwIfAborted();
    folderPath(entry.path, entry.directory);
    if (entry.file.size > maxFile)
      throw new Error(`${entry.path} 超过单文件大小限制`);
    size += entry.file.size;
    count++;
    if (!Number.isSafeInteger(size) || size > maxFile * 4)
      throw new Error("文件夹总大小超过本次连接额度");
  }
  if (!count) throw new Error("未选择文件夹内容");
  return { name, count, size, entries };
}

export function readFolder(handle, maxFile, signal) {
  return prepareFolder(handle.name, () => walkFolder(handle), maxFile, signal);
}

export function folderFromFiles(files, maxFile, signal) {
  const first = files[0];
  const name = first && folderPath(first.webkitRelativePath)[0];
  function* entries() {
    for (const file of files) {
      const [root, ...path] = folderPath(file.webkitRelativePath);
      if (root !== name || !path.length)
        throw new Error("请选择一个完整文件夹");
      yield { file, path: path.join("/"), directory: false };
    }
  }
  return prepareFolder(name, entries, maxFile, signal);
}

async function requireMissing(parent, name) {
  try {
    await parent.getFileHandle(name);
  } catch (error) {
    if (error.name === "NotFoundError") return;
    // TypeMismatchError 表示同名目录，也不能覆盖或混入已有内容。
    if (error.name !== "TypeMismatchError") throw error;
  }
  throw new Error(`已存在“${name}”，请换一个保存目录，已有内容不会覆盖`);
}

export async function createFolderDestination(parent, name) {
  if (folderPath(name).length !== 1) throw new Error("文件夹名称无效");
  await requireMissing(parent, name);
  return parent.getDirectoryHandle(name, { create: true });
}

export async function createFolderEntry(root, path, directory) {
  const parts = folderPath(path, directory);
  let parent = root;
  for (const part of directory ? parts : parts.slice(0, -1))
    parent = await parent.getDirectoryHandle(part, { create: true });
  if (directory) return null;
  const name = parts.at(-1);
  await requireMissing(parent, name);
  return parent.getFileHandle(name, { create: true });
}
