// Admin API client + adapters for the static prototype.

(function () {
  const TOKEN_KEY = "chijie.admin.token";
  const TOKEN_EXPIRES_KEY = "chijie.admin.token_expires_at";

	  class ApiError extends Error {
	    constructor(message, status, data, authRequired = false) {
	      super(message);
	      this.name = "ApiError";
	      this.status = status;
	      this.data = data;
	      this.authRequired = authRequired;
	    }
	  }

  const auth = {
    token: localStorage.getItem(TOKEN_KEY) || "",
    expiresAt: Number(localStorage.getItem(TOKEN_EXPIRES_KEY) || 0),
  };

  function setToken(token, expiresIn) {
    auth.token = token || "";
    auth.expiresAt = token && expiresIn ? Date.now() + expiresIn * 1000 : 0;
    if (auth.token) {
      localStorage.setItem(TOKEN_KEY, auth.token);
      localStorage.setItem(TOKEN_EXPIRES_KEY, String(auth.expiresAt));
    } else {
      localStorage.removeItem(TOKEN_KEY);
      localStorage.removeItem(TOKEN_EXPIRES_KEY);
    }
  }

	  function tokenMeta() {
	    if (!auth.token) return null;
	    const seconds = Math.max(0, Math.round((auth.expiresAt - Date.now()) / 1000));
	    return { token: auth.token, expiresAt: auth.expiresAt, seconds };
	  }

	  function needsLogin(status, message, options) {
	    if (options.noAuth) return false;
	    const text = String(message || "").toLowerCase();
	    return status === 401 ||
	      (status === 403 && (
	        text.includes("missing authorization") ||
	        text.includes("invalid authorization") ||
	        text.includes("unauthorized") ||
	        text.includes("token")
	      ));
	  }

  async function request(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (!headers.has("Accept")) headers.set("Accept", "application/json");
    if (options.body != null && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }
    if (!options.noAuth && auth.token) {
      headers.set("Authorization", `Bearer ${auth.token}`);
    }

    let response;
    try {
      response = await fetch(path, {
        ...options,
        headers,
        body: typeof options.body === "string" || options.body == null
          ? options.body
          : JSON.stringify(options.body),
      });
    } catch (err) {
      throw new ApiError(`request failed: ${err.message}`, 0, null);
    }

    const text = await response.text();
    let data = null;
    if (text) {
      try {
        data = JSON.parse(text);
      } catch {
        data = { message: text };
      }
    }

	    if (!response.ok) {
	      const rawMessage = data?.error || data?.message || `${response.status} ${response.statusText}`;
	      const authRequired = needsLogin(response.status, rawMessage, options);
	      if (authRequired) setToken("", 0);
	      const message = authRequired ? "Please sign in again to continue." : rawMessage;
	      throw new ApiError(message, response.status, data, authRequired);
	    }
    return data;
  }

  async function login(password) {
    const data = await request("/api/auth/login", {
      method: "POST",
      noAuth: true,
      body: { password },
    });
    setToken(data.token, data.expires_in);
    return tokenMeta();
  }

  function logout() {
    setToken("", 0);
  }

  function boolDefault(value, fallback = true) {
    return value == null ? fallback : value !== false;
  }

  function parseDurationMS(value) {
    if (typeof value === "number") return Math.round(value);
    if (!value) return 0;
    const raw = String(value).trim();
    const match = raw.match(/^([\d.]+)\s*(ns|us|µs|ms|s|m|h)?$/);
    if (!match) return Number.parseInt(raw, 10) || 0;
    const amount = Number(match[1]);
    const unit = match[2] || "ms";
    if (unit === "ns") return Math.round(amount / 1000000);
    if (unit === "us" || unit === "µs") return Math.round(amount / 1000);
    if (unit === "s") return Math.round(amount * 1000);
    if (unit === "m") return Math.round(amount * 60000);
    if (unit === "h") return Math.round(amount * 3600000);
    return Math.round(amount);
  }

  function maskSecret(value) {
    if (!value) return "";
    const text = String(value);
    if (text.length <= 4) return "••••";
    return `${text.slice(0, 2)}••••${text.slice(-2)}`;
  }

  function normalizePool(raw) {
    const cfg = raw.config || {};
    const source = raw.source || cfg.source || "";
    const pool = {
      ...cfg,
      name: raw.name,
      source,
      config: cfg,
      enabled: boolDefault(cfg.enabled, true),
      residential: !!cfg.residential,
      premium: !!cfg.premium,
      url: cfg.url || "",
      update_interval: cfg.update_interval || "",
      try_offline: !!cfg.try_offline,
      last_updated: "runtime",
      last_error: raw.error || "",
      type: cfg.type || "",
      template_type: cfg.template_type || "proxy",
      server: cfg.server || "",
      port: cfg.port || 0,
      endpoint: cfg.endpoint || "",
      bearer_token: maskSecret(cfg.bearer_token),
      username_template: cfg.username_template || "",
      password: maskSecret(cfg.password),
      priority: Number(cfg.priority || 0),
      coverage: cfg.coverage || (cfg.template_type === "chijie" ? "both" : (cfg.residential ? "residential" : "normal")),
      tags: cfg.tags || [],
      reject_regex: cfg.reject_regex || [],
      region_groups: raw.region_groups || [],
    };
    pool.nodes = (raw.nodes || []).map(node => normalizeNode(raw, pool, node));
    return pool;
  }

  function normalizeNode(rawPool, pool, node) {
    const name = node.name || "";
    return {
      ...node,
      id: `${rawPool.name}:${name}`,
      name,
      pool: rawPool.name,
      poolSource: pool.source,
      enabled: node.enabled !== false,
      alive: node.alive !== false,
      latency: parseDurationMS(node.latency),
      fail_count: node.fail_count || 0,
      region: node.region || baseRegionFromGroup(node.region_group) || "",
      residential: !!node.residential,
      premium: !!node.premium,
      tags: node.tags || [],
    };
  }

  function normalizeFingerprint(fp) {
    const custom = fp.type !== "preset";
    const config = fp.config || {};
    return {
      ...fp,
      config,
      custom,
      preset: fp.preset || config.preset || (custom ? "" : fp.name),
      ja3: fp.ja3 || config.ja3 || "",
      ja4: fp.ja4 || config.ja4 || "",
      akamai: fp.akamai || config.akamai || "",
      httpVersion: fp.http_version || config.http_version || "",
      method: fp.method || config.method || "",
      userAgent: fp.user_agent || config.user_agent || "",
      tested: true,
      lastTest: "runtime",
    };
  }

  function normalizeSeries(series, metrics) {
    const items = (series || []).slice(-60).map((bucket, index) => {
      const total = Number(bucket.requests || 0);
      const errors = Number(bucket.failures || 0);
      return {
        t: index,
        total,
        success: Math.max(0, total - errors),
        errors,
        avg: Number(bucket.avg_latency_ms || 0),
        p95: Number(bucket.p95_latency_ms ?? metrics?.p95_latency_ms ?? 0),
      };
    });
    if (items.length) return items;
    return Array.from({ length: 60 }, (_, t) => ({
      t,
      total: 0,
      success: 0,
      errors: 0,
      avg: 0,
      p95: 0,
    }));
  }

  function traceGroupFromFlags(trace) {
    if (!trace.region) return "DIRECT";
    let group = trace.region || "UN";
    if (trace.residential) group += "-RES";
    return group;
  }

  function baseRegionFromGroup(group) {
    return String(group || "").replace(/-(RES|PREM)/g, "");
  }

  function normalizeTrace(trace) {
    const group = trace.egress_group || traceGroupFromFlags(trace);
    const url = trace.url || trace.target || "";
    return {
      id: trace.id,
      ts: Date.parse(trace.timestamp) || Date.now(),
      type: trace.kind === "tunnel" ? "tunnel" : "http",
      method: trace.method || (trace.kind === "tunnel" ? "WS" : "GET"),
      url,
      region: trace.region || baseRegionFromGroup(group),
      group,
      strategy: trace.strategy || "random",
      residential: !!trace.residential,
      premium: !!trace.premium,
      pool: trace.egress_pool || "direct",
      node: trace.egress_node || trace.target || "direct",
      template: !!trace.egress_template,
      tls: trace.tls_fingerprint || "",
      status: Number(trace.status || 0),
      duration_ms: Number(trace.latency_ms || 0),
      bytes: Number(trace.request_bytes || 0) + Number(trace.response_bytes || 0),
      error: trace.error || "",
      group_key: trace.group_key || "",
      group_target: trace.group_target || "",
      group_count: Number(trace.group_count || 1),
      children: (trace.children || []).map(normalizeTrace),
    };
  }

  function normalizeTraffic(snapshot) {
    const metrics = snapshot?.metrics || {};
    const rawTraces = snapshot?.traces || [];
    const displayTraces = snapshot?.display_traces || rawTraces;
    return {
      metrics,
      requests: displayTraces.map(normalizeTrace),
      rawLoaded: rawTraces.length,
      rawTotal: Number(metrics.raw_requests || rawTraces.length),
      series: normalizeSeries(snapshot?.series || [], metrics),
    };
  }

  async function loadState(options = {}) {
    const trafficLimit = Number(options.trafficLimit || 200);
    const [nodes, fingerprints, traffic, stats] = await Promise.all([
      request("/api/nodes"),
      request("/api/fingerprints"),
      request(`/api/traffic?limit=${trafficLimit}`),
      request("/api/stats"),
    ]);
    const pools = (nodes.pools || []).map(normalizePool);
    return {
      pools,
      fingerprints: (fingerprints.fingerprints || []).map(normalizeFingerprint),
      traffic: normalizeTraffic(traffic),
      stats,
      regionGroups: window.PG.buildRegionGroups(pools),
      auth: tokenMeta(),
    };
  }

  function cloneConfig(pool) {
    return {
      ...(pool.config || {}),
      source: pool.source,
    };
  }

  function cleanNode(node) {
    const out = {
      name: String(node.name || "").trim(),
      type: String(node.type || "").trim(),
      server: String(node.server || "").trim(),
      port: Number(node.port || 0),
      region: String(node.region || "").trim().toUpperCase(),
      residential: !!node.residential,
      premium: !!node.premium,
      tags: node.tags || [],
    };
    if (node.username) out.username = node.username;
    if (node.password) out.password = node.password;
    if (node.extra && Object.keys(node.extra).length) out.extra = node.extra;
    out.enabled = node.enabled !== false;
    return out;
  }

  async function updatePool(name, config, newName = "") {
    return request("/api/nodes/pool", {
      method: "PUT",
      body: { name, new_name: newName, config },
    });
  }

  const api = {
    ApiError,
    getTokenMeta: tokenMeta,
    login,
    logout,
    createProxyToken: ({ name, duration }) => request("/api/auth/proxy-token", {
      method: "POST",
      body: { name, duration },
    }),
    loadState,
    refreshNodes: () => request("/api/nodes"),
    refreshSubscription: (pool) => request(`/api/nodes/refresh?pool=${encodeURIComponent(pool)}`, { method: "POST" }),
    reload: () => request("/api/reload", { method: "POST" }),
    updateLogging: ({ level }) => request("/api/system/logging", {
      method: "PUT",
      body: { level },
    }),
    updateHealthCheck: (config) => request("/api/system/health-check", {
      method: "PUT",
      body: config,
    }),
    updateProxySettings: (config) => request("/api/system/proxy", {
      method: "PUT",
      body: config,
    }),
    exportConfig: () => request("/api/config/export"),
    setNodeEnabled: ({ pool, node, enabled }) => request("/api/nodes/enabled", {
      method: "POST",
      body: { pool, node, enabled },
    }),
    testNode: ({ pool, node }) => request("/api/nodes/test", {
      method: "POST",
      body: { pool, node },
    }),
    testTemplate: ({ pool, region, url }) => request("/api/nodes/template/test", {
      method: "POST",
      body: { pool, region, url },
    }),
    deleteNode: ({ pool, node }) => request(`/api/nodes/node?pool=${encodeURIComponent(pool)}&node=${encodeURIComponent(node)}`, {
      method: "DELETE",
    }),
    setPoolEnabled: (pool, enabled) => {
      const config = cloneConfig(pool);
      config.enabled = enabled;
      return updatePool(pool.name, config);
    },
    updatePoolConfig: (pool, patch, newName = "") => {
      const config = { ...cloneConfig(pool), ...patch };
      return updatePool(pool.name, config, newName);
    },
    deletePool: (poolName) => request(`/api/nodes/pool?name=${encodeURIComponent(poolName)}`, { method: "DELETE" }),
    addStaticNode: (pool, node) => {
      const config = cloneConfig(pool);
      config.source = "static";
      config.nodes = [...(config.nodes || []), cleanNode(node)];
      return updatePool(pool.name, config);
    },
    addPool: (name, config) => request("/api/nodes", {
      method: "POST",
      body: { name, config },
    }),
    updateSubscriptionNode: (payload) => request("/api/nodes/subscription/node", {
      method: "PUT",
      body: payload,
    }),
    updateStaticNode: ({ pool, node, updatedNode }) => request("/api/nodes/node", {
      method: "PUT",
      body: { pool, node, updated_node: cleanNode(updatedNode) },
    }),
    getTraffic: (limit = 200) => request(`/api/traffic?limit=${limit}`).then(normalizeTraffic),
    addTrafficGroupingRule: (rule) => request("/api/traffic/grouping-rules", {
      method: "POST",
      body: rule,
    }),
    addFingerprint: ({ name, config, configText }) => request("/api/fingerprints", {
      method: "POST",
      body: { name, config, config_text: configText },
    }),
    deleteFingerprint: (name) => request(`/api/fingerprints/${encodeURIComponent(name)}`, { method: "DELETE" }),
    testFingerprint: ({ fingerprint, url }) => request("/api/fingerprints/test", {
      method: "POST",
      body: { fingerprint, url },
    }),
  };

  window.PG_API = api;
})();
