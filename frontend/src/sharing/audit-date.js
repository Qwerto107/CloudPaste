// 日期按浏览器本地时区展开，结束日期包含当天最后一毫秒；不能把 YYYY-MM-DD 当作 UTC。
export function auditDateBoundary(value, endOfDay = false) {
  const date = new Date(`${value}T00:00:00`);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value) || !Number.isFinite(date.getTime()))
    throw new Error("请选择有效的日期");
  if (endOfDay) {
    // 设置本地钟点，兼容夏令时切换时不足或超过 24 小时的日期。
    date.setHours(23, 59, 59, 999);
  }
  return date.toISOString();
}
