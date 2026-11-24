export const expiryPresets = [
  [1, "1 小时"],
  [3, "3 小时"],
  [12, "12 小时"],
  [24, "1 天"],
  [72, "3 天"],
  [120, "5 天"],
  [168, "7 天"],
  [336, "14 天"],
  [720, "30 天"],
  [0, "长期"],
];

export function shareSettingsSummary(hours, password, maxViews) {
  const expiry =
    expiryPresets.find(([value]) => value === hours)?.[1] || "自定义有效期";
  return [
    expiry,
    password ? "密码保护" : "无密码",
    maxViews > 0 ? `限 ${maxViews} 次` : "不限次数",
  ].join(" · ");
}
