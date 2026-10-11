import {
  copyFile,
  lstat,
  mkdir,
  readdir,
  realpath,
  rm,
} from "node:fs/promises";
import { dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const repositoryRoot = fileURLToPath(new URL("../", import.meta.url));

async function checkDirectoryPath(root, directory) {
  const suffix = relative(root, directory);
  if (!suffix || suffix.startsWith(`..${sep}`) || isAbsolute(suffix)) {
    throw new Error("构建目录必须位于项目内部，且不能是项目根目录");
  }
  let current = root;
  for (const part of suffix.split(sep)) {
    current = join(current, part);
    let info;
    try {
      info = await lstat(current);
    } catch (error) {
      if (error.code === "ENOENT") return;
      throw error;
    }
    if (info.isSymbolicLink() || !info.isDirectory()) {
      throw new Error(`构建目录不能经过符号链接或普通文件：${current}`);
    }
  }
}

async function buildFiles(directory, prefix = "") {
  const files = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const name = entry.name.toLowerCase();
    // 只处理构建产物，不把隐藏配置、源码目录、依赖或源码映射装进二进制。
    if (
      name.startsWith(".") ||
      ["src", "source", "node_modules"].includes(name) ||
      name.endsWith(".map")
    ) {
      throw new Error(
        `前端构建目录含不应发布的内容：${join(prefix, entry.name)}`,
      );
    }
    if (entry.isSymbolicLink()) {
      throw new Error(`前端构建目录不能包含符号链接：${entry.name}`);
    }
    const path = join(prefix, entry.name);
    if (entry.isDirectory()) {
      files.push(...(await buildFiles(join(directory, entry.name), path)));
    } else if (entry.isFile()) {
      files.push(path);
    } else {
      throw new Error(`前端构建目录含非普通文件：${path}`);
    }
  }
  return files;
}

export async function prepareWebUI(root = repositoryRoot) {
  root = await realpath(root);
  const source = resolve(root, "frontend/dist");
  const destination = resolve(root, "backend/internal/webui/dist");
  await checkDirectoryPath(root, source);
  await checkDirectoryPath(root, destination);
  const files = await buildFiles(source);
  if (!files.includes("index.html")) {
    throw new Error("缺少 frontend/dist/index.html，请先构建前端");
  }

  await mkdir(dirname(destination), { recursive: true });
  await checkDirectoryPath(root, destination);
  // 删除前再次核对绝对路径；只替换约定的生成目录，不能删除包目录或其他文件。
  if (
    !isAbsolute(destination) ||
    relative(root, destination) !== join("backend", "internal", "webui", "dist")
  ) {
    throw new Error("前端嵌入输出路径不符合约定，已停止清理");
  }
  await rm(destination, { recursive: true, force: true });
  await mkdir(destination, { recursive: true });
  for (const file of files) {
    const target = join(destination, file);
    await mkdir(dirname(target), { recursive: true });
    await copyFile(join(source, file), target);
  }
  return files.length;
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const count = await prepareWebUI();
  console.log(`已准备 ${count} 个前端文件：backend/internal/webui/dist`);
}
