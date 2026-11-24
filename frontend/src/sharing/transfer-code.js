// 配对码为四位数字。展示时不加分隔；输入、粘贴时去掉空白和横杠后统一校验。
export function normalizePairingCode(value) {
  return String(value || "").replace(/[\s-]/g, "");
}

export function validPairingCode(value) {
  return /^[0-9]{4}$/.test(normalizePairingCode(value));
}

export function displayPairingCode(value) {
  const code = normalizePairingCode(value);
  return validPairingCode(code) ? code : "";
}
