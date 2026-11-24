// 配对只上报浏览器提供的原始字段，设备详情摘要仍由服务端生成。
export async function collectTransferDevice(
  nav = globalThis.navigator,
  timeoutMs = 250,
) {
  if (!nav) return {};
  const agent = nav.userAgentData;
  let hints = agent || {};
  if (agent?.getHighEntropyValues) {
    let timer;
    try {
      hints =
        (await Promise.race([
          Promise.resolve()
            .then(() =>
              agent.getHighEntropyValues([
                "platformVersion",
                "fullVersionList",
                "model",
                "formFactors",
              ]),
            )
            .catch(() => agent),
          new Promise((resolve) => {
            timer = setTimeout(() => resolve(agent), timeoutMs);
          }),
        ])) || agent;
    } finally {
      clearTimeout(timer);
    }
  }
  // User-Agent 已由浏览器随 HTTP 请求发送，不在 JSON 中重复上传。
  return {
    platform: hints.platform ?? agent?.platform,
    platform_version: hints.platformVersion,
    mobile: hints.mobile ?? agent?.mobile,
    brands: agent?.brands,
    full_version_list: hints.fullVersionList,
    model: hints.model,
    form_factors: hints.formFactors,
    touch_points: nav.maxTouchPoints,
  };
}

const platforms = [
  ["Android", /\bAndroid\b/],
  ["Windows", /\bWindows\b/],
  ["macOS", /\bMacintosh\b/],
  ["Chrome OS", /\bCrOS\b/],
  ["Linux", /\bLinux\b/],
];
const browsers = [
  ["Edge", "Microsoft Edge", /\b(?:Edg|EdgA|EdgiOS|Edge)\//],
  ["Opera", "Opera", /\b(?:OPR|OPiOS)\//],
  ["Samsung Internet", "Samsung Internet", /\bSamsungBrowser\//],
  ["Firefox", "Firefox", /\b(?:Firefox|FxiOS)\//],
  ["Chrome", "Google Chrome", /\b(?:Chrome|CriOS)\//],
  ["Safari", "Safari", /\bVersion\/.*\bSafari\//],
  ["Chromium", "Chromium", /\bChromium\//],
];

function defaultDeviceLabel(nav, device, ua) {
  const platform = device.platform || nav?.userAgentData?.platform;
  // iPad 的桌面模式可能自报为 Mac，触控信息用于区分这两种情况。
  if (
    platform === "iPadOS" ||
    /\biPad\b/.test(ua) ||
    (ua.includes("Macintosh") && nav?.maxTouchPoints > 1)
  )
    return "iPadOS 平板";
  if (platform === "iOS" || /\b(?:iPhone|iPod)\b/.test(ua)) return "iOS 手机";

  const knownPlatform = platforms.find(([name]) => name === platform);
  const system = (
    knownPlatform || platforms.find(([, pattern]) => pattern.test(ua))
  )?.[0];
  const formFactors = device.form_factors || [];
  const mobile =
    device.mobile ?? nav?.userAgentData?.mobile ?? /\bMobile\b/.test(ua);
  let kind = "设备";
  if (formFactors.includes("Tablet")) kind = "平板";
  else if (formFactors.includes("Mobile")) kind = "手机";
  else if (!formFactors.includes("Desktop")) {
    if (mobile) kind = "手机";
    else if (system === "Android") kind = "平板";
  }
  return [system, kind].filter(Boolean).join(" ");
}

export function defaultTransferName(
  suffix,
  nav = globalThis.navigator,
  device = {},
) {
  const ua = nav?.userAgent || "";
  const brands = [
    ...(device.full_version_list || []),
    ...(device.brands || nav?.userAgentData?.brands || []),
  ];
  const browser =
    browsers.find(
      ([, brand, pattern]) =>
        pattern.test(ua) || brands.some((item) => item.brand === brand),
    )?.[0] || "浏览器";
  const prefix = "我的 ";
  const ending = ` - ${browser} - ${suffix}`;
  const model = typeof device.model === "string" ? device.model.trim() : "";
  // 过滤系统不接受的名称字符，并为浏览器名和随机后缀预留输入长度。
  const usableModel =
    model &&
    model.length <= 60 - prefix.length - ending.length &&
    !/[/\\\p{Cc}\p{Cf}]/u.test(model);
  const label = usableModel ? model : defaultDeviceLabel(nav, device, ua);
  return `${prefix}${label}${ending}`;
}
