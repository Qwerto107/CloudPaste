// 前端只读取原始浏览器字段；识别、校验、摘要和展示文案统一由服务端生成。
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
