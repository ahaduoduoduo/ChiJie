// Mock data for Chijie admin console.
// Models the shape returned by GET /api/nodes, /api/traffic, /api/fingerprints, /api/stats.

const REGION_NAMES = {
  US: "United States", HK: "Hong Kong", JP: "Japan", TW: "Taiwan",
  SG: "Singapore", KR: "Korea", DE: "Germany", UK: "United Kingdom",
  NG: "Nigeria", IN: "India", BR: "Brazil", CA: "Canada", AU: "Australia",
};
const REGION_FLAGS = {
  US: "🇺🇸", HK: "🇭🇰", JP: "🇯🇵", TW: "🇹🇼", SG: "🇸🇬", KR: "🇰🇷",
  DE: "🇩🇪", UK: "🇬🇧", NG: "🇳🇬", IN: "🇮🇳", BR: "🇧🇷", CA: "🇨🇦", AU: "🇦🇺",
};

let _id = 0;
const uid = (p) => `${p}_${++_id}`;

// Node pools
const initialPools = [
  {
    name: "brightdata",
    source: "template",
    type: "http_proxy",
    server: "brd.superproxy.io",
    port: 33335,
    username_template: "brd-customer-hl_b0cfa992-zone-iyf-country-{region}",
    password: "he6•••••••pm",
    residential: false,
    enabled: true,
    tags: ["template", "fallback"],
    nodes: [],
  },
  {
    name: "brightdata-res",
    source: "template",
    type: "http_proxy",
    server: "brd.superproxy.io",
    port: 33335,
    username_template: "brd-customer-hl_b0cfa992-zone-res-country-{region}",
    password: "rs9•••••••xx",
    residential: true,
    enabled: true,
    tags: ["template", "residential"],
    nodes: [],
  },
  {
    name: "direct",
    source: "direct",
    enabled: true,
    nodes: [],
  },
  {
    name: "hk-socks",
    source: "static",
    enabled: true,
    nodes: [
      { id: uid("n"), name: "hk-vps", type: "socks5", server: "10.20.30.40", port: 1080,
        region: "HK", residential: false, enabled: true, alive: true, latency: 38, fail_count: 0, tags: [] },
    ],
  },
  {
    name: "us-fleet",
    source: "static",
    enabled: true,
    nodes: [
      { id: uid("n"), name: "us-east-1", type: "socks5", server: "us1.proxy.internal", port: 1080,
        region: "US", residential: false, enabled: true, alive: true, latency: 142, fail_count: 0, tags: ["primary"] },
      { id: uid("n"), name: "us-west-2", type: "http_proxy", server: "us2.proxy.internal", port: 8080,
        region: "US", residential: false, enabled: true, alive: true, latency: 168, fail_count: 0, tags: [] },
      { id: uid("n"), name: "us-res-01", type: "socks5", server: "res-us.example.net", port: 1080,
        region: "US", residential: true, enabled: true, alive: true, latency: 312, fail_count: 0, tags: ["residential"] },
    ],
  },
  {
    name: "良心云",
    source: "subscription",
    url: "https://liangxin.xyz/api/v1/liangxin?OwO=•••",
    update_interval: "1h",
    last_updated: "12 min ago",
    last_error: "",
    enabled: true,
    reject_regex: ["流量|套餐|官网|剩余|到期"],
    nodes: [
      { id: uid("n"), name: "🇺🇸 美国洛杉矶 01 BGP", type: "vmess", region: "US", alias: "openai-us-primary",
        residential: false, enabled: true, alive: true, latency: 156, fail_count: 0, tags: [] },
      { id: uid("n"), name: "🇺🇸 美国硅谷 02 流媒体", type: "trojan", region: "US",
        residential: false, enabled: true, alive: true, latency: 188, fail_count: 0, tags: ["streaming"] },
      { id: uid("n"), name: "🇯🇵 日本东京 01", type: "vless", region: "JP",
        residential: false, enabled: true, alive: true, latency: 62, fail_count: 0, tags: [] },
      { id: uid("n"), name: "🇯🇵 日本大阪 02", type: "vmess", region: "JP",
        residential: false, enabled: true, alive: false, latency: 0, fail_count: 7, tags: [] },
      { id: uid("n"), name: "🇭🇰 香港中转 01", type: "trojan", region: "HK",
        residential: false, enabled: true, alive: true, latency: 22, fail_count: 0, tags: [] },
      { id: uid("n"), name: "🇭🇰 香港 IPLC 02", type: "vless", region: "HK",
        residential: false, enabled: true, alive: true, latency: 28, fail_count: 0, tags: ["iplc"] },
      { id: uid("n"), name: "🇸🇬 新加坡 01", type: "vmess", region: "SG",
        residential: false, enabled: true, alive: true, latency: 88, fail_count: 0, tags: [] },
      { id: uid("n"), name: "🇹🇼台湾高速01|BGP|流媒体", type: "trojan", region: "TW",
        residential: false, enabled: true, alive: true, latency: 44, fail_count: 0, tags: ["streaming"] },
      { id: uid("n"), name: "🇰🇷 韩国 01", type: "vless", region: "KR",
        residential: false, enabled: false, alive: false, latency: 0, fail_count: 0, tags: [] },
    ],
  },
  {
    name: "res-fleet",
    source: "subscription",
    url: "https://res.example.com/sub?token=•••",
    update_interval: "30m",
    last_updated: "4 min ago",
    last_error: "",
    enabled: true,
    residential: true,
    nodes: [
      { id: uid("n"), name: "US-Residential-NYC", type: "socks5", region: "US",
        residential: true, enabled: true, alive: true, latency: 280, fail_count: 0, tags: ["residential"] },
      { id: uid("n"), name: "US-Residential-LAX", type: "socks5", region: "US",
        residential: true, enabled: true, alive: true, latency: 311, fail_count: 1, tags: ["residential"] },
      { id: uid("n"), name: "HK-Residential-01", type: "http_proxy", region: "HK",
        residential: true, enabled: true, alive: true, latency: 198, fail_count: 0, tags: ["residential"] },
      { id: uid("n"), name: "JP-Residential-01", type: "socks5", region: "JP",
        residential: true, enabled: true, alive: false, latency: 0, fail_count: 12, tags: ["residential"] },
    ],
  },
];

// Build region groups from pools
function buildRegionGroups(pools) {
  const groups = {};
  for (const pool of pools) {
    if (pool.source !== "static" && pool.source !== "subscription") continue;
    if (!pool.enabled) continue;
    for (const n of pool.nodes || []) {
      if (!n.region || n.region === "UN") continue;
      const code = n.residential ? `${n.region}-RES` : n.region;
      if (!groups[code]) {
        groups[code] = {
          code, region: n.region, residential: !!n.residential,
          name: REGION_NAMES[n.region] || n.region,
          flag: REGION_FLAGS[n.region] || "🏳️",
          count: 0, online: 0, sources: new Set(), latencies: [],
        };
      }
      groups[code].count += 1;
      groups[code].sources.add(pool.name);
      if (n.enabled && n.alive) {
        groups[code].online += 1;
        if (n.latency > 0) groups[code].latencies.push(n.latency);
      }
    }
  }
  // Template fallback availability
  const hasNormalTpl = pools.some(p => p.source === "template" && p.enabled && !p.residential);
  const hasResTpl = pools.some(p => p.source === "template" && p.enabled && p.residential);
  return Object.values(groups).map(g => ({
    ...g,
    sources: Array.from(g.sources),
    minLatency: g.latencies.length ? Math.min(...g.latencies) : null,
    templateBackup: g.residential ? hasResTpl : hasNormalTpl,
  })).sort((a, b) => {
    if (a.residential !== b.residential) return a.residential ? 1 : -1;
    return a.region.localeCompare(b.region);
  });
}

// TLS fingerprints
const initialFingerprints = [
  { name: "chrome", preset: "chrome", custom: false, tested: true, lastTest: "2m ago" },
  { name: "edge", preset: "edge", custom: false, tested: true, lastTest: "2m ago" },
  { name: "firefox", preset: "firefox", custom: false, tested: true, lastTest: "2m ago" },
  { name: "safari", preset: "safari", custom: false, tested: true, lastTest: "5m ago" },
  { name: "ios", preset: "ios", custom: false, tested: true, lastTest: "5m ago" },
  { name: "random", preset: "random", custom: false, tested: true, lastTest: "5m ago" },
  { name: "chrome-131-strict", preset: null, custom: true, tested: false,
    ja3: "771,4865-4866-4867-49195-49199,0-23-65281-10-11-35-16-5-13-18-51-45-43-27-17513,29-23-24,0", lastTest: "—" },
];

// Traffic — recent requests + minute-level series
const TYPES = ["http", "tunnel"];
const METHODS = ["GET", "POST", "PUT", "DELETE", "GET", "POST", "GET"];
const TARGETS = [
  "api.openai.com", "api.anthropic.com", "x.com", "instagram.com",
  "api.cloudflare.com", "discord.com", "telegram.org", "tiktok.com",
  "youtube.com", "api.stripe.com", "google.com",
];
const STRATEGIES = ["least-latency", "round-robin", "random"];

function buildTraffic(pools) {
  const realNodes = pools.flatMap(p =>
    (p.source === "static" || p.source === "subscription") && p.enabled
      ? p.nodes.filter(n => n.enabled && n.alive).map(n => ({ ...n, pool: p.name }))
      : []
  );
  const tplPools = pools.filter(p => p.source === "template" && p.enabled);
  const requests = [];
  const now = Date.now();
  for (let i = 0; i < 64; i++) {
    const useTemplate = Math.random() < 0.18;
    const residential = Math.random() < 0.22;
    const candidates = useTemplate
      ? tplPools.filter(p => !!p.residential === residential)
      : realNodes.filter(n => !!n.residential === residential);
    let region, pool, node, isTpl = false;
    if (useTemplate && candidates.length) {
      const codes = ["US","HK","JP","TW","SG","NG","IN","BR","KR","DE"];
      region = codes[Math.floor(Math.random() * codes.length)];
      pool = candidates[0].name;
      node = `template:${region.toLowerCase()}`;
      isTpl = true;
    } else if (candidates.length) {
      const c = candidates[Math.floor(Math.random() * candidates.length)];
      region = c.region; pool = c.pool; node = c.name;
    } else {
      region = "US"; pool = "us-fleet"; node = "us-east-1";
    }
    const okRoll = Math.random();
    let status = 200;
    let err = "";
    if (okRoll < 0.04) { status = 502; err = "upstream connect timeout"; }
    else if (okRoll < 0.07) { status = 429; err = "rate limited"; }
    else if (okRoll < 0.09) { status = 0; err = "egress dial failed"; }
    else if (okRoll < 0.12) status = 401;
    else if (okRoll < 0.18) status = 304;
    requests.push({
      id: uid("r"),
      ts: now - i * (8000 + Math.random() * 22000),
      type: TYPES[Math.random() < 0.92 ? 0 : 1],
      method: METHODS[Math.floor(Math.random() * METHODS.length)],
      url: `https://${TARGETS[Math.floor(Math.random() * TARGETS.length)]}/v1/path/${(Math.random()+1).toString(36).substring(2,8)}`,
      region,
      group: residential ? `${region}-RES` : region,
      strategy: STRATEGIES[Math.floor(Math.random() * STRATEGIES.length)],
      residential,
      pool,
      node,
      template: isTpl,
      tls: ["chrome","ios","safari","firefox",""][Math.floor(Math.random()*5)],
      status,
      duration_ms: Math.round(60 + Math.random() * 1400),
      bytes: Math.round(400 + Math.random() * 240000),
      error: err,
    });
  }
  // minute series
  const series = [];
  for (let i = 59; i >= 0; i--) {
    const total = Math.round(80 + Math.sin(i / 6) * 30 + Math.random() * 40);
    const errors = Math.round(Math.max(0, Math.random() * 6 - 1));
    series.push({
      t: i,
      total,
      success: total - errors,
      errors,
      p95: Math.round(280 + Math.sin(i / 9) * 90 + Math.random() * 80),
    });
  }
  return { requests: requests.sort((a,b) => b.ts - a.ts), series };
}

window.PG = {
  REGION_NAMES, REGION_FLAGS,
  initialPools, initialFingerprints,
  buildRegionGroups, buildTraffic,
  uid,
};
