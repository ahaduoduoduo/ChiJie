// System
const { Modal, Seg, useToast } = window.UI;

function PageSystem({ state, dispatch }) {
  const [logLevel, setLogLevel] = React.useState("info");
  const [tokenOpen, setTokenOpen] = React.useState(false);
  const [savingLog, setSavingLog] = React.useState(false);
  const stats = state.stats || {};
  const traffic = stats.traffic || {};
  const runtime = stats.runtime || {};
  const tokenHours = state.auth?.seconds ? Math.max(1, Math.round(state.auth.seconds / 3600)) : 0;
  const toast = useToast();

  React.useEffect(() => {
    if (runtime.log_level) setLogLevel(runtime.log_level);
  }, [runtime.log_level]);

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
