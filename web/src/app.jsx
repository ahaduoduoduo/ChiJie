// Main app: backend data loading + layout + page routing.

const { useState, useReducer, useEffect, useCallback } = React;
const { ToastProvider, useToast } = window.UI;

const EMPTY_TRAFFIC = {
  metrics: {},
  requests: [],
  rawLoaded: 0,
  rawTotal: 0,
  series: Array.from({ length: 60 }, (_, t) => ({ t, total: 0, success: 0, errors: 0, avg: 0, p95: 0 })),
};

function emptyState() {
  return {
    pools: [],
    fingerprints: [],
    regionGroups: [],
    traffic: EMPTY_TRAFFIC,
    stats: null,
    auth: window.PG_API.getTokenMeta(),
    loading: true,
    booted: false,
    authRequired: false,
    error: "",
  };
}

function rebuildRegionGroups(pools) {
  return window.PG.buildRegionGroups(pools || []);
}

function reducer(state, action) {
  switch (action.type) {
    case "setData":
      return {
        ...state,
        ...action.data,
        regionGroups: action.data.regionGroups || rebuildRegionGroups(action.data.pools),
        loading: false,
        booted: true,
        authRequired: false,
        error: "",
      };
    case "setTraffic":
      return { ...state, traffic: action.traffic || EMPTY_TRAFFIC };
    case "setLoading":
      return { ...state, loading: action.loading, error: action.error || state.error };
    case "setError":
      return { ...state, loading: false, error: action.error || "" };
    case "setAuthRequired":
      return {
        ...state,
        loading: false,
        authRequired: true,
        auth: null,
        error: action.error || "",
      };
    case "setLoggedOut":
      return { ...emptyState(), loading: false, authRequired: true };
    case "toggleNodeLocal": {
      const pools = state.pools.map(p => ({
        ...p,
        nodes: (p.nodes || []).map(n => n.id === action.id ? { ...n, enabled: action.enabled } : n),
      }));
      return { ...state, pools, regionGroups: rebuildRegionGroups(pools) };
    }
    case "togglePoolLocal": {
      const pools = state.pools.map(p => p.name === action.name ? { ...p, enabled: action.enabled } : p);
      return { ...state, pools, regionGroups: rebuildRegionGroups(pools) };
    }
    default:
      return state;
  }
}

function findPool(pools, name) {
  return (pools || []).find(p => p.name === name);
}

function findNodeById(pools, id) {
  for (const pool of pools || []) {
    for (const node of pool.nodes || []) {
      if (node.id === id) return { pool, node };
    }
  }
  return null;
}

const NAV = [
  { id: "overview", label: "Overview", icon: "dashboard" },
  { id: "egress", label: "Egress", icon: "globe" },
  { id: "subscriptions", label: "Subscriptions", icon: "rss" },
  { id: "templates", label: "Templates", icon: "tpl" },
  { id: "tls", label: "TLS Profiles", icon: "shield" },
  { id: "traffic", label: "Traffic", icon: "activity" },
  { id: "system", label: "System", icon: "cog" },
];

function pageFromPath(pathname) {
  const slug = String(pathname || "/").replace(/^\/+|\/+$/g, "");
  if (!slug || slug === "login") return slug === "login" ? null : "overview";
  return NAV.some(item => item.id === slug) ? slug : "overview";
}

function pathForPage(page) {
  return page === "overview" ? "/" : `/${page}`;
}

function replacePath(path) {
  if (window.location.pathname !== path) {
    window.history.replaceState({}, "", path);
  }
}

function Sidebar({ page, setPage, state }) {
  const counts = {
    egress: state.pools.flatMap(p => p.nodes || []).length,
    subscriptions: state.pools.filter(p => p.source === "subscription").length,
    templates: state.pools.filter(p => p.source === "template").length,
    tls: state.fingerprints.length,
    traffic: state.traffic.metrics?.requests ?? state.traffic.requests.length,
  };
  return (
    <div className="sidebar">
      <div className="brand">
        <div className="brand-mark"></div>
        <div className="brand-name">Chijie</div>
      </div>
      <div className="nav">
        <div className="nav-section-label">Console</div>
        {NAV.map(n => {
          const Icon = window.Ic[n.icon];
          return (
            <div key={n.id} className={`nav-item ${page === n.id ? "active" : ""}`} onClick={() => setPage(n.id)}>
              <Icon />
              <span>{n.label}</span>
              {counts[n.id] != null && <span className="badge-count">{counts[n.id]}</span>}
            </div>
          );
        })}
      </div>
      <div className="sidebar-foot">
        <div className="health-pulse"></div>
        <div>
          <div style={{fontSize:12, color:"var(--fg-0)", fontWeight:450}}>Admin API</div>
          <div style={{fontSize:10.5, color:"var(--fg-3)", marginTop:2}} className="mono">
            {state.error ? "attention required" : "connected"}
          </div>
        </div>
      </div>
    </div>
  );
}

function fmtToken(seconds) {
  if (!seconds) return "JWT";
  if (seconds < 60) return `JWT · ${seconds}s`;
  if (seconds < 3600) return `JWT · ${Math.round(seconds / 60)}m`;
  return `JWT · ${Math.round(seconds / 3600)}h`;
}

function Topbar({ page, state, onRefresh, onLogout, busy }) {
  const label = NAV.find(n => n.id === page)?.label || "";
  return (
    <div className="topbar">
      <div className="crumbs">
        <span>Console</span><span className="sep">/</span>
        <span className="now">{label}</span>
      </div>
      <div className="topbar-spacer"></div>
      <div className="topbar-meta mono">/api/{page === "overview" ? "stats" : page}</div>
      <button className="btn-icon" title="Reload" disabled={busy} onClick={onRefresh}><Ic.refresh/></button>
      <div className="topbar-user">
        <div className="avatar">AD</div>
        <div>admin<div style={{fontSize:10.5, color:"var(--fg-3)", marginTop:1}} className="mono">{fmtToken(state.auth?.seconds)}</div></div>
        <button className="btn-icon" title="Logout" onClick={onLogout}><Ic.x/></button>
      </div>
    </div>
  );
}

function ShellMessage({ title, message, action }) {
  return (
    <div className="center-shell">
      <div className="login-panel">
        <div className="brand" style={{padding:0, marginBottom:22}}>
          <div className="brand-mark"></div>
          <div className="brand-name">Chijie</div>
        </div>
        <h1>{title}</h1>
        {message && <p>{message}</p>}
        {action}
      </div>
    </div>
  );
}

function LoginScreen({ onLogin, error }) {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [localError, setLocalError] = useState("");

  const submit = async (event) => {
    event.preventDefault();
    setBusy(true);
    setLocalError("");
    const ok = await onLogin(password);
    if (!ok) setLocalError("Login failed");
    setBusy(false);
  };

  return (
    <div className="center-shell">
      <form className="login-panel" onSubmit={submit}>
        <div className="brand" style={{padding:0, marginBottom:22}}>
          <div className="brand-mark"></div>
          <div className="brand-name">Chijie</div>
        </div>
        <h1>Admin Login</h1>
        <p>Enter the admin password from <span className="mono">gateway.yaml</span>.</p>
        <div className="field" style={{marginTop:22}}>
          <label className="field-label">Password</label>
          <input className="input mono" type="password" autoFocus value={password} onChange={e => setPassword(e.target.value)} />
        </div>
        {(error || localError) && <div className="notice error">{error || localError}</div>}
        <button className="btn primary" type="submit" disabled={busy || !password} style={{marginTop:16}}>
          <Ic.check/> {busy ? "Signing in..." : "Sign in"}
        </button>
      </form>
    </div>
  );
}

function App() {
  const [state, baseDispatch] = useReducer(reducer, null, emptyState);
  const [page, setPageState] = useState(() => pageFromPath(window.location.pathname) || "overview");
  const [trafficLimit, setTrafficLimit] = useState(200);
  const [busy, setBusy] = useState(false);
  const toast = useToast();

  const setPage = useCallback((nextPage) => {
    setPageState(nextPage);
    const nextPath = pathForPage(nextPage);
    if (window.location.pathname !== nextPath) {
      window.history.pushState({ page: nextPage }, "", nextPath);
    }
  }, []);

  const refreshData = useCallback(async (quiet = true) => {
    try {
      const next = await window.PG_API.loadState({ trafficLimit });
      baseDispatch({ type: "setData", data: next });
      if (!quiet) toast("Data refreshed");
      return next;
	    } catch (err) {
	      if (err.authRequired || err.status === 401) {
	        baseDispatch({ type: "setAuthRequired", error: err.message });
	      } else {
        baseDispatch({ type: "setError", error: err.message });
      }
      return null;
    }
  }, [toast, trafficLimit]);

  useEffect(() => {
    refreshData(true);
  }, [refreshData]);

  useEffect(() => {
    const onPopState = () => {
      const nextPage = pageFromPath(window.location.pathname);
      if (nextPage) setPageState(nextPage);
    };
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  useEffect(() => {
    if (state.authRequired || !state.booted) return undefined;
    const timer = setInterval(() => refreshData(true), 10000);
    return () => clearInterval(timer);
  }, [state.authRequired, state.booted, refreshData]);

  useEffect(() => {
    if (state.authRequired) {
      replacePath("/login");
      return;
    }
    if (state.booted && window.location.pathname === "/login") {
      replacePath(pathForPage(page));
    }
  }, [state.authRequired, state.booted, page]);

  const dispatch = useCallback(async (action) => {
    const api = window.PG_API;
    setBusy(true);
    try {
      switch (action.type) {
        case "refreshData":
          return await refreshData(false);
        case "toggleNode": {
          const found = findNodeById(state.pools, action.id);
          if (!found) throw new Error("node not found");
          const enabled = !found.node.enabled;
          baseDispatch({ type: "toggleNodeLocal", id: action.id, enabled });
          const result = await api.setNodeEnabled({ pool: found.pool.name, node: found.node.name, enabled });
          await refreshData(true);
          toast(enabled ? `Enabled ${found.node.name}` : `Disabled ${found.node.name}`);
          return result;
        }
        case "togglePool": {
          const pool = findPool(state.pools, action.name);
          if (!pool) throw new Error("pool not found");
          const enabled = !pool.enabled;
          baseDispatch({ type: "togglePoolLocal", name: action.name, enabled });
          const result = await api.setPoolEnabled(pool, enabled);
          await refreshData(true);
          toast(enabled ? `Enabled ${pool.name}` : `Disabled ${pool.name}`);
          return result;
        }
        case "addStaticNode": {
          const pool = findPool(state.pools, action.pool);
          if (!pool) throw new Error("static pool not found");
          const result = await api.addStaticNode(pool, action.node);
          await refreshData(true);
          toast(`Added ${action.node.name}`);
          return result;
        }
        case "deleteNode": {
          const result = await api.deleteNode({ pool: action.pool, node: action.node });
          await refreshData(true);
          toast(`Deleted ${action.node}`);
          return result;
        }
        case "refreshSub": {
          const result = await api.refreshSubscription(action.name);
          await refreshData(true);
          toast(`Refreshed ${action.name}`);
          return result;
        }
        case "addPool": {
          const result = await api.addPool(action.name, action.config);
          await refreshData(true);
          toast(`Created ${action.name}`);
          return result;
        }
        case "updatePoolConfig": {
          const pool = findPool(state.pools, action.pool);
          if (!pool) throw new Error("pool not found");
          const result = await api.updatePoolConfig(pool, action.patch, action.newName);
          await refreshData(true);
          toast(`Saved ${action.newName || action.pool}`);
          return result;
        }
        case "deletePool": {
          const result = await api.deletePool(action.pool);
          await refreshData(true);
          toast(`Deleted ${action.pool}`);
          return result;
        }
        case "updateSubscriptionNode": {
          const result = await api.updateSubscriptionNode(action.payload);
          await refreshData(true);
          toast(`Saved ${action.payload.node}`);
          return result;
        }
        case "updateStaticNode": {
          const result = await api.updateStaticNode({
            pool: action.pool,
            node: action.node,
            updatedNode: action.updatedNode,
          });
          await refreshData(true);
          toast(`Saved ${action.updatedNode.name}`);
          return result;
        }
        case "testNode": {
          const result = await api.testNode({ pool: action.pool, node: action.node });
          await refreshData(true);
          return result;
        }
        case "testTemplate":
          return await api.testTemplate({ pool: action.pool, region: action.region, url: action.url });
        case "loadMoreTraffic": {
          const nextLimit = Math.min(1000, Math.max(200, trafficLimit + 200));
          const traffic = await api.getTraffic(nextLimit);
          setTrafficLimit(nextLimit);
          baseDispatch({ type: "setTraffic", traffic });
          return traffic;
        }
        case "addFingerprint": {
          const result = await api.addFingerprint({ name: action.name, config: action.config, configText: action.configText });
          await refreshData(true);
          toast(`Created ${action.name}`);
          return result;
        }
        case "deleteFingerprint": {
          const result = await api.deleteFingerprint(action.name);
          await refreshData(true);
          toast(`Deleted ${action.name}`);
          return result;
        }
        case "testFingerprint":
          return await api.testFingerprint({ fingerprint: action.fingerprint, url: action.url });
        case "reload": {
          const result = await api.reload();
          await refreshData(true);
          toast("Config reloaded");
          return result;
        }
        case "updateHealthCheck": {
          const result = await api.updateHealthCheck(action.config);
          await refreshData(true);
          toast("Health check saved");
          return result;
        }
        case "updateProxySettings": {
          const result = await api.updateProxySettings(action.config);
          await refreshData(true);
          toast("Proxy settings saved");
          return result;
        }
        default:
          return null;
      }
    } catch (err) {
      if (err.status === 401) {
        baseDispatch({ type: "setAuthRequired", error: err.message });
      } else {
        baseDispatch({ type: "setError", error: err.message });
      }
      toast(err.message);
      await refreshData(true);
      return null;
    } finally {
      setBusy(false);
    }
  }, [state.pools, trafficLimit, refreshData, toast]);

  const login = async (password) => {
    try {
      await window.PG_API.login(password);
      await refreshData(true);
      replacePath(pathForPage(page));
      toast("Signed in");
      return true;
    } catch (err) {
      baseDispatch({ type: "setAuthRequired", error: err.message });
      return false;
    }
  };

  const logout = () => {
    window.PG_API.logout();
    replacePath("/login");
    baseDispatch({ type: "setLoggedOut" });
  };

  if (state.authRequired) {
    return <LoginScreen onLogin={login} error={state.error} />;
  }

  if (state.loading && !state.booted) {
    return <ShellMessage title="Loading Admin API" message="Reading live gateway state." />;
  }

  if (!state.booted && state.error) {
    return <ShellMessage title="Admin API unavailable" message={state.error}
      action={<button className="btn primary" onClick={() => refreshData(false)}><Ic.refresh/> Retry</button>} />;
  }

  return (
    <div className="app">
      <Sidebar page={page} setPage={setPage} state={state} />
      <div className="main">
        <Topbar page={page} state={state} onRefresh={() => dispatch({ type: "refreshData" })} onLogout={logout} busy={busy} />
        {state.error && <div className="notice page-notice">{state.error}</div>}
        {page === "overview" && <window.PageOverview state={state} />}
        {page === "egress" && <window.PageEgress state={state} dispatch={dispatch} />}
        {page === "subscriptions" && <window.PageSubscriptions state={state} dispatch={dispatch} />}
        {page === "templates" && <window.PageTemplates state={state} dispatch={dispatch} />}
        {page === "tls" && <window.PageTLS state={state} dispatch={dispatch} />}
        {page === "traffic" && <window.PageTraffic state={state} dispatch={dispatch} busy={busy} />}
        {page === "system" && <window.PageSystem state={state} dispatch={dispatch} />}
      </div>
    </div>
  );
}

const root = ReactDOM.createRoot(document.getElementById("root"));
root.render(<ToastProvider><App /></ToastProvider>);
