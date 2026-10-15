// 展示信息与后端配置键分开维护；服务端仍负责权限和最终数值校验。
export const fields = {
  guest_text_share_enabled: {
    label: "创建文本",
    help: "允许未登录访客创建文本分享。",
  },
  guest_file_upload_enabled: {
    label: "上传文件",
    help: "允许访客向公开、可写的存储上传文件并创建分享，仍受上传额度限制。",
  },
  guest_transfer_create_enabled: {
    label: "创建互传",
    help: "允许访客创建配对码，独立于文本和文件分享权限。",
  },
  guest_transfer_join_enabled: {
    label: "加入互传",
    help: "允许访客通过配对码加入连接；关闭后需先登录。",
  },
  transfer_rate_mb: {
    label: "中继总速率上限",
    help: "所有中继文件共享此带宽，0 表示不限速。保存后立即生效，不影响点对点直连。",
    unit: "MiB/s",
    min: 0,
    max: 10240,
  },
  transfer_max_relays: {
    label: "中继并发请求数",
    help: "全站同时中继的请求数量，范围 1–16；下载工具的每个分段各占一路。降低后不会中断现有传输，仅限制新请求。",
    unit: "个",
    min: 1,
    max: 16,
  },
  transfer_max_file_mb: {
    label: "互传单文件上限",
    help: "新建配对连接使用此上限，不能超过服务器配置的硬上限；不影响普通文件分享。",
    unit: "MiB",
    min: 1,
    max: 10240,
  },
  transfer_debug_logs: {
    label: "点对点调试日志",
    help: "默认关闭。开启后，浏览器控制台输出双方连接地址、端口、链路指标和传输异常；保存后重新连接生效。",
  },
  max_upload_size: {
    label: "单文件上传上限",
    help: "单文件最大 10 GiB（10240 MiB），可调低，不能超过服务器配置的上限。",
    unit: "MiB",
    min: 1,
    max: 10240,
  },
  share_default_expiry_hours: {
    label: "新分享默认有效期",
    help: "创建分享时默认使用此时长，填写 0 表示不过期。",
    unit: "小时",
    min: 0,
    max: 876000,
  },
  proxy_sign_all: {
    label: "下载票据校验",
    help: "下载请求必须携带有效票据，此安全策略始终开启。",
  },
  proxy_sign_expires: {
    label: "下载票据有效期",
    help: "用于一次访问期间的下载与媒体播放，与分享链接的有效期独立。",
    unit: "秒",
  },
  preview_max_text_kb: {
    label: "文本文件预览上限",
    help: "仅读取文件开头的指定大小；完整内容仍可下载。范围为 16–2048 KiB。",
    unit: "KiB",
    min: 16,
    max: 2048,
  },
  preview_text_types: {
    multiline: true,
    rows: 2,
    label: "文本格式",
    help: "填写扩展名，以英文逗号分隔，不需要加点。",
    placeholder: "txt,md,json,log",
  },
  preview_image_types: {
    multiline: true,
    rows: 2,
    label: "图片格式",
    help: "填写允许直接展示的图片扩展名，以英文逗号分隔。",
    placeholder: "jpg,png,webp",
  },
  preview_video_types: {
    multiline: true,
    rows: 2,
    label: "视频格式",
    help: "使用浏览器原生播放器，能否播放取决于浏览器及文件编码。",
    placeholder: "mp4,webm,mov",
  },
  preview_audio_types: {
    multiline: true,
    rows: 2,
    label: "音频格式",
    help: "使用浏览器原生播放器，扩展名以英文逗号分隔。",
    placeholder: "mp3,wav,ogg",
  },
  preview_allow_external_images: {
    label: "允许站外图片",
    help: "开启后，Markdown 可向第三方站点请求图片；关闭时仅加载本站图片。",
  },
  site_title: {
    label: "站点标题",
    help: "显示在导航栏与浏览器标签页。",
    placeholder: "CloudPaste",
  },
  site_favicon_url: {
    label: "站点图标",
    help: "填写图标地址，建议使用本站资源；留空使用默认图标。",
    placeholder: "/chain-cloud.svg",
  },
  site_footer_markdown: {
    label: "页脚内容",
    help: "支持 Markdown，留空显示 Copyright © 2026 Qwerto107 · CloudPaste. All rights reserved.",
    placeholder: "填写版权信息或简短说明",
  },
  site_home_editor_enabled: {
    label: "文本分享入口",
    help: "在站点导航中显示文本分享，并允许访问创建页面。",
  },
  site_upload_page_enabled: {
    label: "文件分享入口",
    help: "在站点导航中显示文件分享，并允许访问上传页面。",
  },
  site_announcement_enabled: {
    label: "显示站点公告",
    help: "开启且填写内容后，在导航栏显示公告按钮。",
  },
  site_announcement_default_open: {
    label: "默认显示公告",
    help: "启用公告后，打开或刷新页面时自动展开；访客仍可点击右上角公告按钮收起。",
  },
  site_announcement_content: {
    label: "公告内容",
    help: "支持 Markdown，可设置默认展开；留空时不显示公告。",
    placeholder: "填写站点使用说明或通知",
  },
  site_github_enabled: {
    label: "显示 GitHub 链接",
    help: "控制右上角 GitHub 图标的显示与隐藏。",
  },
  site_github_url: {
    label: "GitHub 地址",
    help: "默认打开本项目仓库，支持 HTTP 或 HTTPS 地址；留空隐藏图标。",
    placeholder: "https://github.com/Qwerto107/CloudPaste",
  },
  site_custom_head: {
    code: true,
    rows: 6,
    label: "页面顶部 HTML",
    help: "展示在页面内容上方。脚本、事件属性、表单与 iframe 会被过滤。",
    placeholder: "填写需要展示的 HTML（可选）",
  },
  site_custom_body: {
    code: true,
    rows: 6,
    label: "页面底部 HTML",
    help: "展示在页面内容下方，使用与顶部内容相同的安全过滤规则。",
    placeholder: "填写需要展示的 HTML（可选）",
  },
};
export const sections = {
  transfer: [
    {
      title: "传输保护",
      icon: "transfer",
      description:
        "按服务器带宽和负载调整，设置会保存并在重启后恢复。不限速时仍保留背压、固定缓冲和并发保护。",
      keys: ["transfer_rate_mb", "transfer_max_relays", "transfer_max_file_mb"],
    },
    {
      title: "连接调试",
      icon: "settings",
      keys: ["transfer_debug_logs"],
    },
  ],
  global: [
    {
      title: "访客权限",
      description: "分别控制未登录访客的操作，不影响已登录账号的权限。",
      icon: "key",
      keys: [
        "guest_text_share_enabled",
        "guest_file_upload_enabled",
        "guest_transfer_create_enabled",
        "guest_transfer_join_enabled",
      ],
    },
    {
      title: "分享与上传",
      description: "为新分享设置默认规则。",
      icon: "link",
      keys: ["share_default_expiry_hours", "max_upload_size"],
    },
    {
      title: "下载安全",
      description: "由服务端固定启用，无需手动配置。",
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
