const kinds = {
  pdf: ["pdf"],
  video: ["mp4", "mov", "webm", "mkv", "avi"],
  image: ["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "heic"],
  archive: ["zip", "rar", "7z", "gz", "tar", "tgz", "bz2", "xz"],
  doc: [
    "md",
    "txt",
    "doc",
    "docx",
    "rtf",
    "odt",
    "csv",
    "xls",
    "xlsx",
    "ppt",
    "pptx",
  ],
  audio: ["mp3", "m4a", "wav", "flac", "ogg", "aac"],
};

export function fileDisplay(name) {
  const dot = name.lastIndexOf(".");
  const extension = dot > 0 ? name.slice(dot + 1).toLowerCase() : "";
  return {
    label: (extension || "FILE").slice(0, 4).toUpperCase(),
    kind:
      Object.keys(kinds).find((kind) => kinds[kind].includes(extension)) ||
      "other",
  };
}

export function formatFileSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KiB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MiB`;
  return `${(bytes / 1024 ** 3).toFixed(1)} GiB`;
}
