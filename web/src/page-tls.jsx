// TLS Profiles
const { Modal, useToast } = window.UI;

function PageTLS({ state, dispatch }) {
  const { fingerprints } = state;
  const [addOpen, setAddOpen] = React.useState(false);
  const [test, setTest] = React.useState(null);
  const toast = useToast();

  const presets = fingerprints.filter(f => !f.custom);
  const customs = fingerprints.filter(f => f.custom);

  const StatusPill = ({ tested }) => (
    <span className="dot" style={{color: tested ? "var(--fg-1)" : "var(--fg-3)"}}>
      <span style={{width:6,height:6,borderRadius:"50%",background: tested ? "#fafafa" : "var(--fg-3)", boxShadow: tested ? "0 0 5px rgba(255,255,255,0.5)" : "none"}}/>
      {tested ? "verified" : "untested"}
    </span>
  );

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>TLS Profiles</h1>
          <p>Fingerprints applied per request via <span className="mono">egress.tls_fingerprint</span>. Built-ins use uTLS; custom profiles accept raw JA3, JA4, Akamai, or YAML/JSON specs.</p>
        </div>
        <div className="page-h-actions">
          <button className="btn primary" onClick={() => setAddOpen(true)}><Ic.plus/> Add profile</button>
        </div>
      </div>

      <div className="card card-pad-0" style={{marginBottom:24}}>
        <div className="card-h bordered"><h3>Built-in presets</h3><span className="sub">{presets.length} profiles · backed by uTLS</span></div>
        <table className="table">
          <thead><tr><th>Name</th><th>Preset</th><th>Last test</th><th>Status</th><th></th></tr></thead>
          <tbody>
            {presets.map(f => (
              <tr key={f.name}>
                <td className="mono strong" style={{fontWeight:450}}>{f.name}</td>
                <td><span className="pill mono">{f.preset}</span></td>
                <td className="mono muted-2">{f.lastTest}</td>
                <td><StatusPill tested={f.tested}/></td>
                <td><div className="row" style={{justifyContent:"flex-end"}}>
          <button className="btn-icon" onClick={() => setTest(f)}><Ic.test/></button>
                </div></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="card card-pad-0">
        <div className="card-h bordered"><h3>Custom profiles</h3><span className="sub">raw strings & spec-driven</span></div>
        {customs.length === 0 ? <div className="empty">No custom fingerprints yet.</div> : (
          <table className="table">
            <thead><tr><th>Name</th><th>Raw input</th><th>Last test</th><th>Status</th><th></th></tr></thead>
            <tbody>
              {customs.map(f => (
                <tr key={f.name}>
                  <td className="mono strong" style={{fontWeight:450}}>{f.name}</td>
                  <td className="mono muted-2 truncate" style={{maxWidth:420, fontSize:11.5}}>{f.ja3 || f.ja4 || f.akamai}</td>
                  <td className="mono muted-2">{f.lastTest}</td>
                  <td><StatusPill tested={f.tested}/></td>
                  <td><div className="row" style={{justifyContent:"flex-end", gap:2}}>
                    <button className="btn-icon" onClick={() => setTest(f)}><Ic.test/></button>
                    <button className="btn-icon"><Ic.edit/></button>
                    <button className="btn-icon" onClick={() => dispatch({type:"deleteFingerprint", name:f.name})}><Ic.trash/></button>
                  </div></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <Modal open={!!test} onClose={() => setTest(null)} title={test ? `Test ${test.name}` : ""}
        footer={<button className="btn" onClick={() => setTest(null)}>Close</button>}>
        <TLSTest fp={test} dispatch={dispatch}/>
      </Modal>

      <AddTLSModal open={addOpen} onClose={() => setAddOpen(false)} dispatch={dispatch} toast={toast}/>
    </div>
  );
}

function AddTLSModal({ open, onClose, dispatch, toast }) {
  const [form, setForm] = React.useState({
    name: "",
    source: "preset",
    preset: "chrome",
    ja3: "",
    ja4: "",
    akamai: "",
    spec: `ja3: "771,4865-4866-4867-49196-49195-52393-49200-49199-52392-49162-49161-49172-49171,0-23-65281-10-11-16-5-13-18-51-45-43-27-21,29-23-24-25,0"
ja4: "t13d2014h2_000a,002f,0035,009c,009d,1301,1302,1303,c008,c009,c00a,c012,c013,c014,c02b,c02c,c02f,c030,cca8,cca9_0000,0005,000a,000b,000d,0012,0015,0017,001b,002b,002d,0033,ff01_0403,0804,0401,0503,0805,0805,0501,0806,0601,0201"
akamai: "HEADER_TABLE_SIZE=65536;ENABLE_PUSH=0;INITIAL_WINDOW_SIZE=6291456;MAX_HEADER_LIST_SIZE=262144|15663105|method,authority,scheme,path"
extra_fp:
  tls_signature_algorithms:
    - ecdsa_secp256r1_sha256
    - rsa_pss_rsae_sha256
    - rsa_pkcs1_sha256
    - ecdsa_secp384r1_sha384
    - rsa_pss_rsae_sha384
    - rsa_pkcs1_sha384
    - rsa_pss_rsae_sha512
    - rsa_pkcs1_sha512
    - rsa_pkcs1_sha1
  tls_cert_compression: zlib
  tls_grease: true`,
  });
  const set = (k, v) => setForm(f => ({ ...f, [k]: v }));
  const submit = async () => {
    if (!form.name.trim()) { toast("Profile name is required"); return; }
    let config = null;
    let configText = "";
    if (form.source === "preset") {
      config = { preset: form.preset };
    } else if (form.source === "raw") {
      config = {};
      if (form.ja3.trim()) config.ja3 = form.ja3.trim();
      if (form.ja4.trim()) config.ja4 = form.ja4.trim();
      if (form.akamai.trim()) config.akamai = form.akamai.trim();
      if (!config.ja3 && !config.ja4) { toast("JA3 or JA4 raw is required"); return; }
    } else {
      configText = form.spec.trim();
      if (!configText) { toast("JSON/YAML spec is required"); return; }
    }
    const ok = await dispatch({type:"addFingerprint", name: form.name.trim(), config, configText});
    if (ok) onClose();
  };
  return (
    <Modal open={open} onClose={onClose} title="Add TLS profile"
      footer={<>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit}><Ic.check/> Save</button>
      </>}>
      <div className="col gap-12">
        <div className="field"><label className="field-label">Name</label>
          <input className="input mono" value={form.name} onChange={e => set("name", e.target.value)} placeholder="chrome-131-strict"/></div>
        <div className="field"><label className="field-label">Source</label>
          <select className="select" value={form.source} onChange={e => set("source", e.target.value)}>
            <option value="preset">Preset</option>
            <option value="raw">Raw strings</option>
            <option value="spec">JSON / YAML spec</option>
          </select></div>
        {form.source === "preset" ? (
          <div className="field"><label className="field-label">Preset</label>
            <select className="select" value={form.preset} onChange={e => set("preset", e.target.value)}>
              {["chrome","firefox","safari","ios","edge","360","qq","random"].map(p => <option key={p}>{p}</option>)}
            </select></div>
        ) : (
          form.source === "raw" ? (
            <>
              <div className="field"><label className="field-label">JA3</label>
                <textarea className="input mono" value={form.ja3} onChange={e => set("ja3", e.target.value)} placeholder="771,4865-4866-4867-49195-...,0-23-65281-...,29-23-24,0" style={{minHeight:120}}></textarea></div>
              <div className="field"><label className="field-label">JA4</label>
                <textarea className="input mono" value={form.ja4} onChange={e => set("ja4", e.target.value)} placeholder="t13d2014h2_000a,002f,..._0000,0005,..._0403,0804,..." style={{minHeight:96}}></textarea>
              </div>
              <div className="field"><label className="field-label">Akamai</label>
                <textarea className="input mono" value={form.akamai} onChange={e => set("akamai", e.target.value)} placeholder="HEADER_TABLE_SIZE=65536;ENABLE_PUSH=0;INITIAL_WINDOW_SIZE=6291456;MAX_HEADER_LIST_SIZE=262144|15663105|method,authority,scheme,path" style={{minHeight:86}}></textarea></div>
            </>
          ) : (
            <div className="field"><label className="field-label">JSON / YAML spec</label>
              <textarea className="input mono" value={form.spec} onChange={e => set("spec", e.target.value)} spellCheck="false" style={{minHeight:260}}></textarea>
              <div className="field-hint">Supports raw <span className="mono">ja3</span>, <span className="mono">ja4</span>, <span className="mono">akamai</span>, captured JSON with <span className="mono">tls/http2</span>, and curl_cffi <span className="mono">extra_fp</span>.</div></div>
          )
        )}
      </div>
    </Modal>
  );
}

function TLSTest({ fp, dispatch }) {
  const [target, setTarget] = React.useState("https://tls.browserleaks.com/json");
  const [busy, setBusy] = React.useState(false);
  const [out, setOut] = React.useState(null);
  if (!fp) return null;
  const observed = out?.observed || {};
  const observedRows = flattenObservedFields(observed);
  const observedJA3 = observed.tls?.ja3 || observed.ja3 || observed.ja3_text || observed.ja3n_text || observed.ja3_hash || observed.ja3n_hash || "—";
  const observedJA4 = observed.tls?.ja4 || observed.tls?.ja4_r || observed.ja4 || observed.ja4_r || observed.ja4_o || "—";
  const observedAkamai = observed.http2?.akamai_fingerprint || observed.akamai || observed.akamai_text || observed.akamai_hash || "—";
  const observedIP = observed.ip || observed.origin || observed.remote_addr || observed.remote_ip || "—";
  const observedRegion = observed.country_code || observed.country || observed.region || observed.geoip?.country_code || observed.geo?.country_code || "—";
  return (
    <div className="col gap-12">
      <div className="field"><label className="field-label">Target URL</label>
        <input className="input mono" value={target} onChange={e => setTarget(e.target.value)}/></div>
      <button className="btn primary" disabled={busy} onClick={async () => {
        setBusy(true); setOut(null);
        const result = await dispatch({type:"testFingerprint", fingerprint: fp.name, url: target});
        setBusy(false);
        if (result) setOut(result);
      }}><Ic.bolt/> {busy ? "Probing…" : "Probe handshake"}</button>
      {out && (
        <div style={{padding:"14px 16px", background:"var(--bg-2)", border:"1px solid var(--line-2)", borderRadius:6}}>
          <span className="dot" style={{color:out.status === "ok" ? "var(--fg-1)" : "var(--sig-err)"}}>
            <span style={{width:6,height:6,borderRadius:"50%",background:out.status === "ok" ? "#fafafa" : "var(--sig-err)", boxShadow:out.status === "ok" ? "0 0 6px rgba(255,255,255,0.6)" : "none"}}/>
            {out.status === "ok" ? "handshake ok" : "handshake failed"}
          </span>
          <div className="kv" style={{marginTop:14, rowGap:10}}>
            <div className="k">Type</div><div className="v mono">{out.type || "preset"}</div>
            <div className="k">Preset</div><div className="v mono">{out.preset || "—"}</div>
            <div className="k">HTTP</div><div className="v mono">{out.http_status || "—"}</div>
            <div className="k">HTTP version</div><div className="v mono">{out.http_proto || out.http_version || "—"}</div>
            <div className="k">Latency</div><div className="v mono">{out.latency_ms ?? "—"}ms</div>
            <div className="k">IP</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{observedIP}</div>
            <div className="k">Region</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{observedRegion}</div>
            <div className="k">Observed JA3</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{observedJA3}</div>
            <div className="k">Observed JA4</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{observedJA4}</div>
            <div className="k">Observed Akamai</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{observedAkamai}</div>
            {out.error && <><div className="k">Error</div><div className="v mono" style={{fontSize:11, wordBreak:"break-all", lineHeight:1.5}}>{out.error}</div></>}
          </div>
          {observedRows.length > 0 && (
            <div style={{marginTop:18, borderTop:"1px solid var(--line)", paddingTop:14}}>
              <div className="field-label" style={{marginBottom:10}}>Observed JSON</div>
              <div style={{border:"1px solid var(--line)", borderRadius:6, overflow:"hidden", maxHeight:360, overflowY:"auto"}}>
                {observedRows.map(row => (
                  <div key={row.path} style={{display:"grid", gridTemplateColumns:"170px minmax(0, 1fr)", borderBottom:"1px solid var(--line)", alignItems:"stretch"}}>
                    <div className="mono" style={{padding:"9px 10px", color:"var(--fg-3)", fontSize:10.5, borderRight:"1px solid var(--line)", wordBreak:"break-all"}}>{row.path}</div>
                    <pre className="mono" style={{margin:0, padding:"9px 10px", whiteSpace:"pre-wrap", wordBreak:"break-word", color:"var(--fg-1)", fontSize:10.5, lineHeight:1.45}}>{row.value}</pre>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function flattenObservedFields(value, prefix = "") {
  if (!value || typeof value !== "object") return [];
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    if (child && typeof child === "object" && !Array.isArray(child)) {
      const nested = flattenObservedFields(child, path);
      return nested.length ? nested : [{ path, value: "{}" }];
    }
    return [{ path, value: formatObservedValue(child) }];
  });
}

function formatObservedValue(value) {
  if (value === null || value === undefined || value === "") return "—";
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

window.PageTLS = PageTLS;
