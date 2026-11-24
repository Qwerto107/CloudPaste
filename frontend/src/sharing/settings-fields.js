import { auditModules } from "./audit-modules.js";

const rateFields = [
  [
    "password_attempts_per_minute",
    "密码尝试次数",
    "每来源每分钟；管理密码、分享密码与 API Key 分别计数，API Key 只计失败验证",
  ],
  ["transfer_create_per_minute", "创建连接", "每来源每分钟"],
  [
    "transfer_join_per_minute",
    "加入连接",
    "每来源每分钟，配对码未匹配也计数",
  ],
  [
    "transfer_ws_connect_per_minute",
    "WebSocket 连接",
    "每来源每分钟",
  ],
  [
    "transfer_ws_messages_per_minute",
    "消息频率",
    "每连接每分钟，含聊天、信令与控制消息",
  ],
  [
    "transfer_relay_per_minute",
    "中继请求",
    "每来源每分钟，上传与下载共享次数",
  ],
  [
    "transfer_tool_per_minute",
    "下载工具请求",
    "每来源每分钟，HEAD、GET 与分段下载共享次数",
  ],
  [
    "transfer_folder_ws_messages_per_minute",
    "文件夹内部消息",
    "每来源每分钟，与普通消息分开计数",
  ],
  [
    "transfer_folder_relay_per_minute",
    "文件夹中继请求",
    "每来源每分钟，上传与下载共享次数",
  ],
];

// 展示信息与后端配置键分开维护；服务端仍负责权限和最终数值校验。
export const fields = {
  ...Object.fromEntries(
    rateFields.map(([key, label, help]) => [
      key,
      {
        label,
        help,
        min: 1,
        max: 1000000,
        unit: "次",
      },
    ]),
  ),
  ...Object.fromEntries(
    auditModules.map(({ key, label, description }) => [
      `audit_${key}_enabled`,
      { label, help: "" },
    ]),
  ),
  audit_enabled: {
    label: "启用操作日志",
    help: "关闭后仍可查看和清理已有日志",
  },
  audit_retention_days: {
    label: "操作日志保留天数",
    help: "范围 1–3650 天，到期记录自动清理",
    unit: "天",
    min: 1,
    max: 3650,
  },
  guest_text_share_enabled: {
    label: "创建文本",
    help: "未登录访客可创建文本分享",
  },
  guest_file_upload_enabled: {
    label: "上传文件",
    help: "只能写入公开、可写的存储",
  },
  guest_transfer_create_enabled: {
    label: "创建互传",
    help: "",
  },
  guest_transfer_join_enabled: {
    label: "加入互传",
    help: "",
  },
  transfer_rate_mb: {
    label: "中继总速率上限",
    help: "0 表示不限",
    unit: "MiB/s",
    min: 0,
    max: 10240,
  },
  transfer_max_relays: {
    label: "中继并发请求数",
    help: "0 表示不限",
    unit: "个",
    min: 0,
  },
  transfer_max_file_mb: {
    label: "互传单文件上限",
    help: "",
    unit: "MiB",
    min: 1,
    max: 10240,
  },
  transfer_folder_concurrency: {
    label: "文件夹默认并发数",
    help: "范围 1–128",
    unit: "个",
    min: 1,
    max: 128,
  },
  transfer_enabled: {
    label: "启用设备互传",
    help: "关闭后不能创建或加入新连接，已有连接不受影响",
  },
  transfer_stun: {
    label: "STUN 服务器列表",
    help: "每行一个，最多 5 个，留空停用",
    placeholder:
      "stun:stun.example.com:3478\nstun:stun-backup.example.com:3478",
    multiline: true,
    rows: 3,
  },
  transfer_debug_logs: {
    label: "点对点调试日志",
    help: "在浏览器控制台记录选中链路、延迟与传输统计，重新配对后生效",
  },
  max_uploads: {
    label: "上传并发数",
    help: "同时上传的文件数",
    unit: "个",
    min: 1,
    max: 65535,
  },
  max_downloads: {
    label: "下载并发数",
    help: "",
    unit: "个",
    min: 1,
    max: 65535,
  },
  max_staged_size_mb: {
    label: "上传暂存总额度",
    help: "所有进行中上传的临时空间",
    unit: "MiB",
    min: 1,
    max: 102400,
  },
  max_upload_size: {
    label: "单文件上传上限",
    help: "超过后拒绝上传",
    unit: "MiB",
    min: 1,
    max: 10240,
  },
  share_default_expiry_hours: {
    label: "新分享默认有效期",
    help: "创建时预选的期限，0 表示长期",
    unit: "小时",
    min: 0,
    max: 876000,
  },
  proxy_sign_all: {
    label: "下载票据校验",
    help: "下载链接需携带短期票据",
  },
  proxy_sign_expires: {
    label: "下载票据有效期",
    help: "",
    unit: "秒",
  },
  preview_max_text_kb: {
    label: "文本文件预览上限",
    help: "超过部分不预览，可下载完整文件",
    unit: "KiB",
    min: 16,
    max: 2048,
  },
  preview_text_types: {
    label: "文本格式",
    help: "",
    placeholder: "txt,md,json,log",
  },
  preview_image_types: {
    label: "图片格式",
    help: "",
    placeholder: "jpg,png,webp",
  },
  preview_video_types: {
    label: "视频格式",
    help: "",
    placeholder: "mp4,webm,mov",
  },
  preview_audio_types: {
    label: "音频格式",
    help: "",
    placeholder: "mp3,wav,ogg",
  },
  preview_allow_external_images: {
    label: "允许站外图片",
    help: "Markdown 中加载其他站点的图片",
  },
  site_title: {
    label: "站点标题",
    help: "",
    placeholder: "CloudPaste",
  },
  site_favicon_url: {
    label: "站点图标",
    help: "SVG 或 PNG 地址",
    placeholder: "/chain-cloud.svg",
  },
  site_footer_markdown: {
    label: "页脚内容",
    help: "支持 Markdown",
    placeholder: "填写版权信息或简短说明",
  },
  site_home_editor_enabled: {
    label: "文本分享入口",
    help: "",
  },
  site_upload_page_enabled: {
    label: "文件分享入口",
    help: "",
  },
  site_announcement_enabled: {
    label: "显示站点公告",
    help: "",
  },
  site_announcement_default_open: {
    label: "默认显示公告",
    help: "新访客打开页面时展开",
  },
  site_announcement_content: {
    label: "公告内容",
    help: "支持 Markdown",
    placeholder: "填写站点使用说明或通知",
  },
  site_github_enabled: {
    label: "显示 GitHub 链接",
    help: "",
  },
  site_github_url: {
    label: "GitHub 地址",
    help: "",
    placeholder: "https://github.com/Qwerto107/CloudPaste",
  },
  site_custom_head: {
    code: true,
    rows: 6,
    label: "页面顶部 HTML",
    help: "经过安全过滤",
    placeholder: "填写需要展示的 HTML（可选）",
  },
  site_custom_body: {
    code: true,
    rows: 6,
    label: "页面底部 HTML",
    help: "经过安全过滤",
    placeholder: "填写需要展示的 HTML（可选）",
  },
};
export const sections = {
  security: [
    {
      title: "认证限频",
      icon: "key",
      description:
        "一分钟固定窗口，范围 1–1000000；修改后保留当前计数，0 不表示关闭。",
      keys: ["password_attempts_per_minute"],
    },
    {
      title: "连接限频",
      icon: "link",
      keys: [
        "transfer_create_per_minute",
        "transfer_join_per_minute",
        "transfer_ws_connect_per_minute",
      ],
    },
    {
      title: "消息与中继限频",
      icon: "transfer",
      description:
        "互传按来源计数时，IPv6 使用 /64 网段；消息按已认证成员计数。",
      keys: [
        "transfer_ws_messages_per_minute",
        "transfer_relay_per_minute",
        "transfer_tool_per_minute",
        "transfer_folder_ws_messages_per_minute",
        "transfer_folder_relay_per_minute",
      ],
    },
  ],
  logs: [
    {
      title: "操作日志",
      description: "控制日志记录与保留时间，仅管理员可查看和清理。",
      icon: "file",
      keys: ["audit_enabled", "audit_retention_days"],
    },
    {
      title: "模块记录开关",
      description:
        "仅在操作日志总开关开启时记录。关闭模块后停止记录新操作，已有日志仍会保留。",
      icon: "grid",
      keys: auditModules.map(({ key }) => `audit_${key}_enabled`),
    },
  ],
  transfer: [
    {
      title: "连接设置",
      icon: "settings",
      keys: ["transfer_enabled", "transfer_stun", "transfer_debug_logs"],
    },
    {
      title: "传输资源",
      icon: "transfer",
      keys: [
        "transfer_rate_mb",
        "transfer_max_relays",
        "transfer_max_file_mb",
        "transfer_folder_concurrency",
      ],
    },
  ],
  global: [
    {
      title: "分享与上传",
      icon: "link",
      keys: [
        "share_default_expiry_hours",
        "max_upload_size",
        "max_uploads",
        "max_downloads",
        "max_staged_size_mb",
      ],
    },
    {
      title: "访客权限",
      icon: "key",
      keys: [
        "guest_text_share_enabled",
        "guest_file_upload_enabled",
        "guest_transfer_create_enabled",
        "guest_transfer_join_enabled",
      ],
    },
    {
      title: "下载安全",
      icon: "key",
      keys: ["proxy_sign_all", "proxy_sign_expires"],
    },
  ],
  preview: [
    {
      title: "预览范围",
      description: "控制文本读取大小与站外资源加载。",
      icon: "file",
      keys: ["preview_max_text_kb", "preview_allow_external_images"],
    },
    {
      title: "支持的文件格式",
      description: "未列入的文件仍可下载，不提供在线预览。",
      icon: "folder",
      keys: [
        "preview_text_types",
        "preview_image_types",
        "preview_video_types",
        "preview_audio_types",
      ],
    },
  ],
  site: [
    {
      title: "站点信息",
      description: "设置站点名称、图标和页脚。",
      icon: "cloud",
      keys: ["site_title", "site_favicon_url", "site_footer_markdown"],
    },
    {
      title: "页面入口",
      description: "控制分享入口是否显示；实际操作仍由访客或账号权限决定。",
      icon: "grid",
      keys: [
        "site_home_editor_enabled",
        "site_upload_page_enabled",
        "site_github_enabled",
        "site_github_url",
      ],
    },
    {
      title: "站点公告",
      description: "发布简短通知或使用说明。",
      icon: "bell",
      keys: [
        "site_announcement_enabled",
        "site_announcement_default_open",
        "site_announcement_content",
      ],
    },
    {
      title: "高级自定义",
      description: "按需添加页面 HTML，默认无需配置。",
      icon: "settings",
      advanced: true,
      keys: ["site_custom_head", "site_custom_body"],
    },
  ],
};
