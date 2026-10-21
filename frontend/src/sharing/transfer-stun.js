// 只解析地址，不发起探测；实际可用链路由浏览器的 ICE 协商确定。
export function parseSTUNServers(value) {
  const invalid = () =>
    new Error(
      "STUN 地址每行一个，最多 5 个；格式为 stun:主机:端口，端口范围 1–65535",
    );
  if (
    typeof value !== "string" ||
    new TextEncoder().encode(value).length > 1284
  )
    throw invalid();
  const servers = [],
    seen = new Set();
  for (const raw of value.replaceAll("\r\n", "\n").split("\n")) {
    const line = raw.trim();
    if (!line) continue;
    const parts = /^stun:(?:\[([^\]]+)\]|([^:\[\]]+)):(\d+)$/.exec(line);
    if (!parts || line.length > 256 || /[\s/@?#\\]/u.test(line))
      throw invalid();
    const [, ipv6, hostname, port] = parts;
    if (Number(port) < 1 || Number(port) > 65535) throw invalid();
    if (ipv6) {
      if (!ipv6.includes(":")) throw invalid();
      try {
        new URL(`http://[${ipv6}]/`);
      } catch {
        throw invalid();
      }
    } else if (
      !hostname
        .replace(/\.$/, "")
        .split(".")
        .every((label) => /^[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?$/i.test(label))
    )
      throw invalid();
    const key = line.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    servers.push(line);
    if (servers.length > 5) throw invalid();
  }
  return servers;
}
