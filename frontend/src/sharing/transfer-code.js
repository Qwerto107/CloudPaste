// 横杠只用于展示；复制、输入及请求都使用统一的大写紧凑形式。
export function normalizePairingCode(value) {
  return String(value || "")
    .replace(/[\s-]/g, "")
    .toUpperCase();
}

export function validPairingCode(value) {
  return /^[A-Z][0-9]{4}$/.test(normalizePairingCode(value));
}

export function displayPairingCode(value) {
  const code = normalizePairingCode(value);
  return validPairingCode(code) ? `${code[0]}-${code.slice(1)}` : "";
}
