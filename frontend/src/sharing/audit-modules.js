// 导航、筛选和设置共用模块名称，描述说明对应的日志记录范围。
export const auditModules = [
  {
    key: "auth",
    label: "账号认证",
    description: "记录登录、退出及身份验证结果。",
  },
  {
    key: "text",
    label: "文本分享",
    description: "记录文本创建、查看、修改和删除操作。",
  },
  {
    key: "file",
    label: "文件分享",
    description: "记录文件上传、访问、下载和删除操作。",
  },
  {
    key: "transfer",
    label: "设备互传",
    description: "记录连接申请、配对和文字、文件、图片传输。",
  },
  {
    key: "storage",
    label: "存储管理",
    description: "记录存储配置变更及连接测试。",
  },
  {
    key: "keys",
    label: "密钥管理",
    description: "记录 API Key 的创建、修改和删除操作。",
  },
  {
    key: "settings",
    label: "系统设置",
    description: "记录系统配置变更。",
  },
];
