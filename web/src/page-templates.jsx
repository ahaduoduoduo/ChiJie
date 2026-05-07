// Templates
const { Modal, Toggle, useToast } = window.UI;

function PageTemplates({ state, dispatch }) {
  const { pools } = state;
  const tpls = pools.filter(p => p.source === "template");
  const [addOpen, setAddOpen] = React.useState(false);
  const [editTpl, setEditTpl] = React.useState(null);
  const [test, setTest] = React.useState(null);
  const toast = useToast();

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>Templates</h1>
          <p>Generate per-region accounts on demand. Templates serve any 2-letter region as fallback, never mixing normal and residential traffic.</p>
        </div>
        <div className="page-h-actions">
          <button className="btn primary" onClick={() => setAddOpen(true)}><Ic.plus/> Add template</button>
        </div>
      </div>

      <div className="grid-2">
        {tpls.map(t => (
          <div key={t.name} className="card">
            <div className="card-h bordered">
              <h3 className="mono">{t.name}</h3>
              <span className={`pill ${t.residential ? "res" : ""}`} style={t.residential ? {} : {fontFamily:"Inter",fontSize:10.5,letterSpacing:0,padding:"0 8px"}}>{t.residential ? "residential" : "normal"}</span>
              <div className="right">
                <Toggle on={t.enabled} onChange={() => dispatch({type:"togglePool", name: t.name})}/>
              </div>
            </div>
            <div className="card-body">
              <div className="kv" style={{rowGap:14}}>
                <div className="k">Type</div><div className="v mono">{t.type}</div>
                <div className="k">Server</div><div className="v mono">{t.server}:{t.port}</div>
                <div className="k">Username</div>
                <div className="v mono" style={{fontSize:11.5, wordBreak:"break-all", lineHeight:1.5}}>{t.username_template}</div>
                <div className="k">Password</div><div className="v mono">{t.password}</div>
                <div className="k">Coverage</div>
                <div className="v">
                  <span className="pill mono">any ISO-2</span>
                  <div className="muted-2" style={{marginTop:6, fontSize:11.5}}>{t.residential ? "serves *-RES groups" : "serves normal groups"}</div>
                </div>
              </div>
              <div style={{marginTop:20}} className="row gap-12">
                <button className="btn sm" onClick={() => setTest(t)}><Ic.test/> Test region</button>
                <button className="btn sm ghost" onClick={() => setEditTpl(t)}><Ic.edit/> Edit</button>
                <button className="btn sm ghost danger ml-auto" onClick={() => dispatch({type:"deletePool", pool:t.name})}><Ic.trash/> Remove</button>
              </div>
            </div>
          </div>
        ))}
      </div>

      <Modal open={!!test} onClose={() => setTest(null)} title={test ? `Test ${test.name}` : ""}
        footer={<><button className="btn" onClick={() => setTest(null)}>Close</button></>}>
        <TemplateTest tpl={test} dispatch={dispatch}/>
      </Modal>

      <AddTemplateModal open={addOpen} onClose={() => setAddOpen(false)} dispatch={dispatch} toast={toast}/>
      <EditTemplateModal tpl={editTpl} onClose={() => setEditTpl(null)} dispatch={dispatch} toast={toast}/>
    </div>
  );
}

function AddTemplateModal({ open, onClose, dispatch, toast }) {
  const [form, setForm] = React.useState({
    name: "",
    type: "http_proxy",
    server: "",
    port: 33335,
    username_template: "",
    password: "",
    residential: false,
  });
  const set = (k, v) => setForm(f => ({ ...f, [k]: v }));
  const submit = async () => {
    if (!form.name.trim() || !form.server.trim() || !form.type.trim()) {
      toast("Name, type and server are required");
      return;
    }
    const ok = await dispatch({type:"addPool", name: form.name.trim(), config:{
      source: "template",
      enabled: true,
      type: form.type,
      server: form.server.trim(),
      port: Number(form.port || 0),
      username_template: form.username_template,
      password: form.password,
      residential: form.residential,
    }});
    if (ok) onClose();
  };
  return (
    <Modal open={open} onClose={onClose} title="Add template pool"
      footer={<>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit}><Ic.check/> Add</button>
      </>}>
      <div className="col gap-12">
        <div className="field-row">
          <div className="field"><label className="field-label">Pool name</label><input className="input mono" value={form.name} onChange={e => set("name", e.target.value)} placeholder="brightdata-eu"/></div>
          <div className="field"><label className="field-label">Type</label>
            <select className="select" value={form.type} onChange={e => set("type", e.target.value)}><option>http_proxy</option><option>socks5</option></select></div>
        </div>
        <div className="field-row">
          <div className="field"><label className="field-label">Server</label><input className="input mono" value={form.server} onChange={e => set("server", e.target.value)} placeholder="brd.superproxy.io"/></div>
          <div className="field"><label className="field-label">Port</label><input className="input mono" value={form.port} onChange={e => set("port", e.target.value)}/></div>
        </div>
        <div className="field"><label className="field-label">Username template</label>
          <input className="input mono" value={form.username_template} onChange={e => set("username_template", e.target.value)} placeholder="brd-customer-xxx-zone-yyy-country-{region}"/>
          <div className="field-hint"><span className="mono">{`{region}`}</span> → lowercase · <span className="mono">{`{REGION}`}</span> → uppercase</div></div>
        <div className="field"><label className="field-label">Password</label><input className="input mono" type="password" value={form.password} onChange={e => set("password", e.target.value)}/></div>
        <div className="field" style={{flexDirection:"row", justifyContent:"space-between", alignItems:"flex-start"}}>
          <div><div className="field-label">Class</div><div className="field-hint" style={{maxWidth:280}}>Normal templates only serve normal requests; residential only serves residential.</div></div>
          <Toggle on={form.residential} onChange={v => set("residential", v)} label="Residential"/>
        </div>
      </div>
    </Modal>
  );
}

function EditTemplateModal({ tpl, onClose, dispatch, toast }) {
  const [form, setForm] = React.useState({
    type: "http_proxy",
    server: "",
    port: 33335,
    username_template: "",
    password: "",
    residential: false,
  });
  React.useEffect(() => {
    if (!tpl) return;
    setForm({
      type: tpl.type || "http_proxy",
      server: tpl.server || "",
      port: tpl.port || 33335,
      username_template: tpl.username_template || "",
      password: tpl.config?.password || "",
      residential: !!tpl.residential,
    });
  }, [tpl]);
  if (!tpl) return null;
  const set = (k, v) => setForm(f => ({ ...f, [k]: v }));
  const submit = async () => {
    if (!form.server.trim() || !form.type.trim()) {
      toast("Type and server are required");
      return;
    }
    const ok = await dispatch({type:"updatePoolConfig", pool: tpl.name, patch:{
      source: "template",
      enabled: tpl.enabled,
      type: form.type,
      server: form.server.trim(),
      port: Number(form.port || 0),
      username_template: form.username_template,
      password: form.password,
      residential: form.residential,
    }});
    if (ok) onClose();
  };
  return (
    <Modal open={!!tpl} onClose={onClose} title={`Edit ${tpl.name}`}
      footer={<>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit}><Ic.check/> Save</button>
      </>}>
      <div className="col gap-12">
        <div className="field"><label className="field-label">Pool name</label>
          <input className="input mono" value={tpl.name} disabled /></div>
        <div className="field-row">
          <div className="field"><label className="field-label">Type</label>
            <select className="select" value={form.type} onChange={e => set("type", e.target.value)}><option>http_proxy</option><option>socks5</option></select></div>
          <div className="field"><label className="field-label">Port</label>
            <input className="input mono" value={form.port} onChange={e => set("port", e.target.value)}/></div>
        </div>
        <div className="field"><label className="field-label">Server</label>
          <input className="input mono" value={form.server} onChange={e => set("server", e.target.value)} placeholder="brd.superproxy.io"/></div>
        <div className="field"><label className="field-label">Username template</label>
          <input className="input mono" value={form.username_template} onChange={e => set("username_template", e.target.value)} placeholder="brd-customer-xxx-zone-yyy-country-{region}"/>
          <div className="field-hint"><span className="mono">{`{region}`}</span> → lowercase · <span className="mono">{`{REGION}`}</span> → uppercase</div></div>
        <div className="field"><label className="field-label">Password</label>
          <input className="input mono" type="password" value={form.password} onChange={e => set("password", e.target.value)}/></div>
        <div className="field" style={{flexDirection:"row", justifyContent:"space-between", alignItems:"flex-start"}}>
          <div><div className="field-label">Class</div><div className="field-hint" style={{maxWidth:280}}>Normal templates only serve normal requests; residential only serves residential.</div></div>
          <Toggle on={form.residential} onChange={v => set("residential", v)} label="Residential"/>
        </div>
      </div>
    </Modal>
  );
}

function TemplateTest({ tpl, dispatch }) {
  const [region, setRegion] = React.useState("US");
  const [targetMode, setTargetMode] = React.useState("https://api.ipify.org?format=json");
  const [customTarget, setCustomTarget] = React.useState("");
  const [result, setResult] = React.useState(null);
  const [busy, setBusy] = React.useState(false);
  if (!tpl) return null;
  const target = targetMode === "custom" ? customTarget : targetMode;
  const resolved = (tpl.username_template || "").replaceAll("{region}", region.toLowerCase()).replaceAll("{REGION}", region.toUpperCase());
  const phaseLabel = {
    proxy_tcp: "proxy tcp",
    proxy_connect: "proxy connect",
    tls_handshake: "tls handshake",
    target_http: "target http",
    timeout: "timeout",
    request: "request",
  }[result?.phase] || result?.phase || "—";
  const hint = result?.phase === "proxy_connect"
    ? "Proxy rejected CONNECT before target response. Check Bright Data zone permission, country parameter, password, whitelist and target policy."
    : result?.phase === "target_http"
      ? "Target returned an HTTP response through the proxy."
      : "";
  const countryMismatch = result?.country_code && result?.region && result.country_code !== result.region;
  const ipSignals = [
    result?.ip_proxy ? "proxy" : "",
    result?.ip_vpn ? "vpn" : "",
    result?.ip_tor ? "tor" : "",
    result?.ip_hosting ? "hosting" : "",
  ].filter(Boolean).join(", ");
  return (
    <div className="col gap-12">
      <div className="field"><label className="field-label">Test region (ISO 2)</label>
        <input className="input mono" value={region} onChange={e => setRegion(e.target.value.toUpperCase().slice(0,2))} maxLength={2}/></div>
      <div className="field"><label className="field-label">Test URL</label>
        <select className="select" value={targetMode} onChange={e => setTargetMode(e.target.value)}>
          <option value="https://api.ipify.org?format=json">api.ipify.org JSON</option>
          <option value="https://httpbin.org/ip">httpbin.org/ip</option>
          <option value="https://www.google.com/generate_204">Google generate_204</option>
          <option value="custom">Custom URL</option>
        </select></div>
      {targetMode === "custom" && (
        <div className="field"><label className="field-label">Custom URL</label>
          <input className="input mono" value={customTarget} onChange={e => setCustomTarget(e.target.value)} placeholder="https://example.com/"/></div>
      )}
      <div className="field"><label className="field-label">Resolved username</label>
        <div className="mono" style={{padding:"10px 12px", background:"var(--bg-2)", border:"1px solid var(--line-2)", borderRadius:6, fontSize:11.5, wordBreak:"break-all", lineHeight:1.5}}>{resolved}</div></div>
      <button className="btn primary" disabled={busy} onClick={async () => {
        setBusy(true); setResult(null);
        const out = await dispatch({type:"testTemplate", pool: tpl.name, region, url: target});
        setBusy(false);
        if (out) setResult(out);
      }}><Ic.bolt/> {busy ? "Probing…" : "Connect & probe"}</button>
      {result && (
        <div style={{padding:"14px 16px", background:"var(--bg-2)", border:"1px solid var(--line-2)", borderRadius:6}}>
          <div className="row" style={{justifyContent:"space-between"}}>
            <span className="dot" style={{color:"var(--fg-1)"}}>
              <span style={{width:6,height:6,borderRadius:"50%",background: result.ok ? "#fafafa" : "var(--fg-3)", boxShadow: result.ok ? "0 0 6px rgba(255,255,255,0.6)" : "none"}}/>
              {result.ok ? "connected" : "failed"}
            </span>
            {result.ok && <span className="mono">{result.latency_ms}ms</span>}
          </div>
          <div style={{marginTop:14}} className="kv">
            <div className="k">Node</div><div className="v mono">{result.node || "—"}</div>
            <div className="k">Requested region</div><div className="v mono">{result.region || region}</div>
            <div className="k">Egress IP</div><div className="v mono">{result.observed_ip || "—"}</div>
            <div className="k">IP country</div><div className="v mono" style={{color:countryMismatch ? "var(--sig-warn)" : "var(--fg-0)"}}>{result.country_code || "—"}</div>
            <div className="k">IP type</div><div className="v mono">{result.ip_type || "—"}{result.ip_version ? ` · ${result.ip_version}` : ""}</div>
            <div className="k">ISP</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{result.ip_isp || "—"}</div>
            <div className="k">ASN</div><div className="v mono">{result.ip_asn ? `AS${result.ip_asn}` : "—"}</div>
            <div className="k">Organization</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{result.ip_org || "—"}</div>
            <div className="k">IP flags</div><div className="v mono">{ipSignals || "—"}</div>
            <div className="k">Test URL</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{result.test_url || target}</div>
            <div className="k">Phase</div><div className="v mono">{phaseLabel}</div>
            <div className="k">HTTP</div><div className="v mono">{result.http_status || "—"}</div>
            <div className="k">Username</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{result.resolved_username || resolved}</div>
            {result.error && <><div className="k">Error</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{result.error}</div></>}
            {result.geo_error && <><div className="k">Geo error</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{result.geo_error}</div></>}
            {hint && <><div className="k">Meaning</div><div className="v muted" style={{fontSize:11.5, lineHeight:1.5}}>{hint}</div></>}
          </div>
        </div>
      )}
    </div>
  );
}

window.PageTemplates = PageTemplates;
