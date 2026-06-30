// System
const { Modal, Seg, useToast } = window.UI;

function PageSystem({ state, dispatch }) {
  const [logLevel, setLogLevel] = React.useState("info");
  const [tokenOpen, setTokenOpen] = React.useState(false);
  const [savingLog, setSavingLog] = React.useState(false);
  const [savingHealth, setSavingHealth] = React.useState(false);
  const [savingProxy, setSavingProxy] = React.useState(false);
  const [healthDirty, setHealthDirty] = React.useState(false);
  const [proxyDirty, setProxyDirty] = React.useState(false);
  const [healthForm, setHealthForm] = React.useState({
    interval: "30s",
    timeout: "5s",
    url: "https://www.google.com/generate_204",
    max_fail: 3,
  });
  const [proxyForm, setProxyForm] = React.useState({
    max_attempts: 5,
    max_redirects: 5,
    template_fallback_after_attempts: true,
    response_header_timeout: "3s",
    total_timeout: "30s",
  });
  const stats = state.stats || {};
  const traffic = stats.traffic || {};
  const runtime = stats.runtime || {};
  const health = stats.health_check || {};
  const proxy = stats.proxy || {};
  const tokenHours = state.auth?.seconds ? Math.max(1, Math.round(state.auth.seconds / 3600)) : 0;
  const toast = useToast();

  React.useEffect(() => {
    if (runtime.log_level) setLogLevel(runtime.log_level);
  }, [runtime.log_level]);

  React.useEffect(() => {
    if (healthDirty) return;
    setHealthForm({
      interval: health.interval || "30s",
      timeout: health.timeout || "5s",
      url: health.url || "https://www.google.com/generate_204",
      max_fail: health.max_fail || 3,
    });
  }, [health.interval, health.timeout, health.url, health.max_fail, healthDirty]);

  React.useEffect(() => {
    if (proxyDirty) return;
    setProxyForm({
      max_attempts: proxy.max_attempts || 5,
      max_redirects: proxy.max_redirects || 5,
      template_fallback_after_attempts: proxy.template_fallback_after_attempts !== false,
      response_header_timeout: proxy.response_header_timeout || "3s",
      total_timeout: proxy.total_timeout || proxy.request_timeout || "30s",
    });
  }, [proxy.max_attempts, proxy.max_redirects, proxy.template_fallback_after_attempts, proxy.response_header_timeout, proxy.total_timeout, proxy.request_timeout, proxyDirty]);

  const setHealthField = (key, value) => {
    setHealthDirty(true);
    setHealthForm(v => ({ ...v, [key]: value }));
  };

  const setProxyField = (key, value) => {
    setProxyDirty(true);
    setProxyForm(v => ({ ...v, [key]: value }));
  };

  const saveLogLevel = async (level) => {
    setLogLevel(level);
    setSavingLog(true);
    try {
      const next = await window.PG_API.updateLogging({ level });
      setLogLevel(next.log_level || level);
      toast("Log level saved");
      await dispatch({ type: "refreshData" });
    } catch (err) {
      toast(err.message);
    } finally {
      setSavingLog(false);
    }
  };

  const exportConfig = async () => {
    try {
      const data = await window.PG_API.exportConfig();
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      const stamp = new Date().toISOString().replace(/[:.]/g, "-");
      a.href = url;
      a.download = `chijie-config-${stamp}.json`;
      a.click();
      URL.revokeObjectURL(url);
      toast("Config exported");
    } catch (err) {
      toast(err.message);
    }
  };

  const saveHealthCheck = async () => {
    setSavingHealth(true);
    try {
      const result = await dispatch({ type: "updateHealthCheck", config: {
        interval: healthForm.interval.trim(),
        timeout: healthForm.timeout.trim(),
        url: healthForm.url.trim(),
        max_fail: Number(healthForm.max_fail || 0),
      }});
      if (result) setHealthDirty(false);
    } finally {
      setSavingHealth(false);
    }
  };

  const saveProxySettings = async () => {
    setSavingProxy(true);
    try {
      const result = await dispatch({ type: "updateProxySettings", config: {
        max_attempts: Number(proxyForm.max_attempts || 0),
        max_redirects: Number(proxyForm.max_redirects || 0),
        template_fallback_after_attempts: !!proxyForm.template_fallback_after_attempts,
        response_header_timeout: proxyForm.response_header_timeout.trim(),
        total_timeout: proxyForm.total_timeout.trim(),
      }});
      if (result) setProxyDirty(false);
    } finally {
      setSavingProxy(false);
    }
  };

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>System</h1>
          <p>Listen ports, admin auth, hot reload and runtime status.</p>
        </div>
        <div className="page-h-actions">
          <button className="btn" onClick={() => dispatch({type:"reload"})}><Ic.refresh/> Reload config</button>
        </div>
      </div>

      <div className="grid-2" style={{marginBottom:24}}>
        <div className="card">
          <div className="card-h bordered"><h3>Proxy API</h3><span className="sub">Bearer-protected</span></div>
          <div className="card-body">
	            <div className="kv" style={{rowGap:14}}>
	              <div className="k">Listen</div><div className="v mono">{runtime.proxy_listen || "—"}</div>
	              <div className="k">TLS</div><div className="v dot">{runtime.proxy_tls ? "enabled" : "disabled"}</div>
	              <div className="k">Auth</div><div className="v"><span className="pill mono">Authorization: Bearer</span></div>
	              <div className="k">Token</div><div className="v"><button className="btn primary" onClick={() => setTokenOpen(true)}><Ic.plus/> Create token</button></div>
            </div>
          </div>
        </div>

        <div className="card">
          <div className="card-h bordered"><h3>Admin API</h3><span className="sub">JWT-protected</span></div>
          <div className="card-body">
	            <div className="kv" style={{rowGap:14}}>
	              <div className="k">Listen</div><div className="v mono">{runtime.admin_listen || "—"}</div>
	              <div className="k">Auth</div><div className="v"><span className="pill mono">{runtime.auth_enabled === false ? "disabled" : "Password + JWT"}</span></div>
	              <div className="k">JWT</div><div className="v mono">{tokenHours ? `${tokenHours}h remaining` : "active"}</div>
	              <div className="k">Pools</div><div className="v mono">{stats.pools_count ?? state.pools.length}</div>
            </div>
          </div>
        </div>
      </div>

      <div className="card" style={{marginBottom:24}}>
        <div className="card-h bordered"><h3>Proxy settings</h3><span className="sub">timeout and failover</span></div>
        <div className="card-body">
          <div className="field-row">
            <div className="field"><label className="field-label">Max node attempts</label>
              <input className="input mono" type="number" min="1" max="50" value={proxyForm.max_attempts}
                onChange={e => setProxyField("max_attempts", e.target.value)} placeholder="5"/></div>
            <div className="field"><label className="field-label">Max redirects</label>
              <input className="input mono" type="number" min="1" max="50" value={proxyForm.max_redirects}
                onChange={e => setProxyField("max_redirects", e.target.value)} placeholder="5"/></div>
            <div className="field"><label className="field-label">Response header timeout</label>
              <input className="input mono" value={proxyForm.response_header_timeout}
                onChange={e => setProxyField("response_header_timeout", e.target.value)} placeholder="3s"/></div>
            <div className="field"><label className="field-label">Total timeout</label>
              <input className="input mono" value={proxyForm.total_timeout}
                onChange={e => setProxyField("total_timeout", e.target.value)} placeholder="30s"/></div>
            <div className="field">
              <label className="field-label">Template after node failures</label>
              <div className="row gap-12" style={{height:36, alignItems:"center"}}>
                <Toggle on={proxyForm.template_fallback_after_attempts} onChange={() => setProxyField("template_fallback_after_attempts", !proxyForm.template_fallback_after_attempts)}/>
                <span className="field-hint">Use templates after configured node attempts fail.</span>
              </div>
            </div>
          </div>
          <div className="field-hint" style={{marginTop:10}}>Failed static or subscription nodes are marked offline immediately; health checks can bring them back online.</div>
          <div className="row gap-12" style={{marginTop:16}}>
            <button className="btn primary" disabled={savingProxy} onClick={saveProxySettings}><Ic.check/> Save proxy settings</button>
            {savingProxy && <span className="field-hint">Saving…</span>}
          </div>
        </div>
      </div>

      <div className="card" style={{marginBottom:24}}>
        <div className="card-h bordered"><h3>Health check</h3><span className="sub">runtime defaults</span></div>
        <div className="card-body">
          <div className="field-row">
            <div className="field"><label className="field-label">Check interval</label>
              <input className="input mono" value={healthForm.interval} onChange={e => setHealthField("interval", e.target.value)} placeholder="30s"/></div>
            <div className="field"><label className="field-label">Timeout</label>
              <input className="input mono" value={healthForm.timeout} onChange={e => setHealthField("timeout", e.target.value)} placeholder="5s"/></div>
            <div className="field"><label className="field-label">Max failures</label>
              <input className="input mono" type="number" min="1" value={healthForm.max_fail} onChange={e => setHealthField("max_fail", e.target.value)} placeholder="3"/></div>
          </div>
          <div className="field" style={{marginTop:16}}><label className="field-label">Test URL</label>
            <input className="input mono" value={healthForm.url} onChange={e => setHealthField("url", e.target.value)} placeholder="https://www.google.com/generate_204"/></div>
          <div className="row gap-12" style={{marginTop:16}}>
            <button className="btn primary" disabled={savingHealth} onClick={saveHealthCheck}><Ic.check/> Save health check</button>
            {savingHealth && <span className="field-hint">Saving…</span>}
          </div>
        </div>
      </div>

      <div className="grid-2" style={{marginBottom:24}}>
        <div className="card">
          <div className="card-h bordered"><h3>Runtime</h3></div>
          <div className="card-body">
	            <div className="kv" style={{rowGap:14}}>
	              <div className="k">Version</div><div className="v mono">{runtime.version || "chijie"}</div>
	              <div className="k">Go</div><div className="v mono">{runtime.go_version || "—"}</div>
              <div className="k">Uptime</div><div className="v mono">{stats.uptime || "—"}</div>
              <div className="k">Fingerprints</div><div className="v mono">{stats.fingerprints ?? state.fingerprints.length}</div>
              <div className="k">Requests</div><div className="v mono">{traffic.requests ?? 0}</div>
              <div className="k">Success rate</div><div className="v mono">{traffic.requests ? `${(traffic.success_rate * 100).toFixed(1)}%` : "—"}</div>
            </div>
          </div>
        </div>

        <div className="card">
          <div className="card-h bordered"><h3>Logging</h3></div>
          <div className="card-body">
	            <div className="field">
	              <label className="field-label">Level</label>
	              <Seg value={logLevel} onChange={saveLogLevel}
	                options={[{value:"debug",label:"debug"},{value:"info",label:"info"},{value:"warn",label:"warn"},{value:"error",label:"error"}]}/>
	            </div>
	            <div className="field" style={{marginTop:16}}><label className="field-label">Output</label>
	              <input className="input mono" readOnly value={runtime.log_output || "stdout"}/></div>
	            {savingLog && <div className="field-hint" style={{marginTop:10}}>Saving log level…</div>}
	          </div>
        </div>
      </div>

      <div className="card">
        <div className="card-h bordered"><h3>Hot reload</h3><span className="sub">re-read configs without restarting</span></div>
        <div className="card-body">
	          <div className="row gap-12">
	            <button className="btn primary" onClick={() => dispatch({type:"reload"})}><Ic.refresh/> Reload now</button>
	            <button className="btn" onClick={exportConfig}><Ic.download/> Export current config</button>
	            <span className="muted-2 ml-auto" style={{fontSize:11.5}}>Runtime uptime: <span className="mono">{stats.uptime || "—"}</span></span>
	          </div>
        </div>
      </div>

      <CreateProxyTokenModal open={tokenOpen} onClose={() => setTokenOpen(false)} />
    </div>
  );
}

function CreateProxyTokenModal({ open, onClose }) {
  const [name, setName] = React.useState("proxy-api");
  const [duration, setDuration] = React.useState("30d");
  const [result, setResult] = React.useState(null);
  const [creating, setCreating] = React.useState(false);
  const toast = useToast();

  React.useEffect(() => {
    if (!open) {
      setResult(null);
      setCreating(false);
    }
  }, [open]);

  const copy = async (text) => {
    try {
      await navigator.clipboard.writeText(text);
      toast("Copied");
    } catch {
      toast("Copy failed");
    }
  };

  const submit = async () => {
    setCreating(true);
    try {
      const data = await window.PG_API.createProxyToken({ name, duration });
      setResult(data);
      toast("Proxy token created");
    } catch (err) {
      toast(err.message);
    } finally {
      setCreating(false);
    }
  };

  return (
    <Modal open={open} onClose={onClose} title="Create proxy token"
      footer={<>
        <button className="btn" onClick={onClose}>Close</button>
        {result?.token && <button className="btn" onClick={() => copy(result.token)}><Ic.copy/> Copy token</button>}
        <button className="btn primary" disabled={creating} onClick={submit}><Ic.check/> Generate</button>
      </>}>
      <div className="col gap-12">
        <div className="field-row">
          <div className="field">
            <label className="field-label">Name</label>
            <input className="input mono" value={name} onChange={e => setName(e.target.value)} placeholder="billing-worker"/>
          </div>
          <div className="field">
            <label className="field-label">Expires</label>
            <select className="select" value={duration} onChange={e => setDuration(e.target.value)}>
              <option value="24h">24h</option>
              <option value="7d">7d</option>
              <option value="30d">30d</option>
              <option value="90d">90d</option>
              <option value="365d">365d</option>
            </select>
          </div>
        </div>
        {result?.token && (
          <>
            <div className="field">
              <label className="field-label">Bearer token</label>
              <textarea className="input mono" readOnly value={result.token} style={{minHeight:140}} onFocus={e => e.target.select()} />
            </div>
            <div className="kv" style={{rowGap:10}}>
              <div className="k">Name</div><div className="v mono">{result.name}</div>
              <div className="k">Expires at</div><div className="v mono">{result.expires_at}</div>
              <div className="k">Header</div><div className="v mono">Authorization: Bearer &lt;token&gt;</div>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}

window.PageSystem = PageSystem;
