// 分享文字只包含对方打开所需的信息，不包含登录凭据或管理下载票据。
export function shareText(link, password = "") {
  return `链接：${link}${password ? `\n密码：${password}` : ""}`;
}
