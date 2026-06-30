// Shared UI — refined monochrome
const { useState, useEffect, useCallback } = React;

const ToastCtx = React.createContext({ push: () => {} });
function ToastProvider({ children }) {
  const [items, setItems] = useState([]);
  const push = useCallback((msg) => {
    const id = Math.random().toString(36).slice(2);
    setItems(s => [...s, { id, msg }]);
    setTimeout(() => setItems(s => s.filter(i => i.id !== id)), 2600);
  }, []);
  return (
    <ToastCtx.Provider value={{ push }}>
      {children}
      <div className="toast-stack">
        {items.map(t => (
          <div key={t.id} className="toast">
            <span style={{width:6,height:6,borderRadius:"50%",background:"#fafafa",boxShadow:"0 0 6px rgba(255,255,255,0.6)"}}/>
            {t.msg}
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  );
}
const useToast = () => React.useContext(ToastCtx).push;

function Modal({ open, onClose, title, children, footer, className }) {
  if (!open) return null;
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className={`modal ${className || ""}`.trim()} onClick={e => e.stopPropagation()}>
        <div className="modal-h"><h2>{title}</h2><button className="btn-icon ml-auto" onClick={onClose}><Ic.x /></button></div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-foot">{footer}</div>}
      </div>
    </div>
  );
}

function Drawer({ open, onClose, title, children }) {
  if (!open) return null;
  return (
    <>
      <div className="drawer-backdrop" onClick={onClose} />
      <div className="drawer">
        <div className="drawer-h">
          <h3 style={{margin:0,fontSize:16,fontWeight:500,letterSpacing:"-0.01em"}}>{title}</h3>
          <button className="btn-icon ml-auto" onClick={onClose}><Ic.x /></button>
        </div>
        <div className="drawer-body">{children}</div>
      </div>
    </>
  );
}

function Toggle({ on, onChange, label }) {
  return (
    <div className={`toggle ${on ? "on" : ""}`} onClick={() => onChange(!on)}>
      <div className="toggle-track" />
      {label && <div className="toggle-label">{label}</div>}
    </div>
  );
}

function Seg({ options, value, onChange }) {
  return (
    <div className="seg">
      {options.map(o => (
        <button key={o.value} className={value === o.value ? "on" : ""} onClick={() => onChange(o.value)}>{o.label}</button>
      ))}
    </div>
  );
}

function RegionPill({ code, residential, flag }) {
  // derive flag from code prefix if not given
  const baseCode = window.PG?.baseRegionCode ? window.PG.baseRegionCode(code) : String(code || "").replace(/-RES/g, "").replace(/-PREM/g, "");
  const flagEmoji = flag || window.PG?.regionFlag?.(baseCode) || null;
  return (
    <span className={`pill region ${residential ? "res" : ""}`}>
      {flagEmoji && <span style={{fontSize:11, lineHeight:1, marginRight:1, filter:"saturate(0.85)"}}>{flagEmoji}</span>}
      {code}
    </span>
  );
}

function PremiumDot({ label = false }) {
  return (
    <span className={`premium-dot ${label ? "with-label" : ""}`} title="Premium node" aria-label="Premium node">
      {label && <span>premium</span>}
    </span>
  );
}

function StatusDot({ alive, enabled, fail }) {
  if (!enabled) return <span className="dot disabled">disabled</span>;
  if (!alive) return <span className="dot down">offline</span>;
  if (fail > 0) return <span className="dot flaky">flaky</span>;
  return <span className="dot alive">live</span>;
}

function LatencyBar({ ms }) {
  if (!ms || ms <= 0) return <span className="muted-2 mono">—</span>;
  const pct = Math.min(100, (ms / 500) * 100);
  let tier = "mid";
  if (ms < 100) tier = "fast";
  else if (ms < 250) tier = "mid";
  else if (ms < 500) tier = "slow";
  else tier = "bad";
  return (
    <span className={`lat ${tier}`}>
      <span className="lat-track"><span className="lat-fill" style={{width: `${pct}%`}}/></span>
      <span>{ms}<span className="ms">ms</span></span>
    </span>
  );
}

// Smooth line + soft area glow
function Sparkline({ data, width = 240, height = 56 }) {
  if (!data || !data.length) return null;
  const max = Math.max(...data, 1);
  const min = Math.min(...data, 0);
  const span = max - min || 1;
  const dx = width / (data.length - 1 || 1);
  const pts = data.map((v, i) => [i * dx, height - ((v - min) / span) * (height - 6) - 3]);
  const line = pts.map(([x,y], i) => `${i?'L':'M'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ');
  const fill = `${line} L${width} ${height} L0 ${height} Z`;
  const id = "sg" + Math.random().toString(36).slice(2,8);
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} style={{display:"block"}}>
      <defs>
        <linearGradient id={id} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor="rgba(255,255,255,0.18)"/>
          <stop offset="100%" stopColor="rgba(255,255,255,0)"/>
        </linearGradient>
      </defs>
      <path d={fill} fill={`url(#${id})`}/>
      <path d={line} fill="none" stroke="rgba(255,255,255,0.85)" strokeWidth="1.4" strokeLinejoin="round" strokeLinecap="round"/>
    </svg>
  );
}

// Bars — pure tonal
function BarChart({ series, width = 540, height = 80 }) {
  if (!series?.length) return null;
  const max = Math.max(...series.map(s => s.total), 1);
  const bw = width / series.length;
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} style={{display:"block"}}>
      {series.map((s, i) => {
        const h = (s.total / max) * (height - 4);
        const eh = (s.errors / max) * (height - 4);
        return (
          <g key={i} transform={`translate(${i * bw}, 0)`}>
            <rect x="0.5" y={height - h} width={Math.max(1, bw - 2)} height={h} fill="rgba(255,255,255,0.18)" rx="1"/>
            {eh > 0 && <rect x="0.5" y={height - eh} width={Math.max(1, bw - 2)} height={eh} fill="rgba(255,255,255,0.7)" rx="1"/>}
          </g>
        );
      })}
    </svg>
  );
}

function fmtBytes(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024*1024) return `${(n/1024).toFixed(1)} KB`;
  return `${(n/1024/1024).toFixed(2)} MB`;
}
function fmtAgo(ts) {
  const sec = Math.round((Date.now() - ts) / 1000);
  if (sec < 60) return `${sec}s`;
  if (sec < 3600) return `${Math.round(sec/60)}m`;
  return `${Math.round(sec/3600)}h`;
}
function fmtUTC8(ts) {
  const d = new Date(Number(ts || Date.now()) + 8 * 60 * 60 * 1000);
  const pad = n => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())} UTC+8`;
}
// Status code rendered tonally — no color, only weight
function StatusCode({ code }) {
  const ok = code >= 200 && code < 400;
  return <span className="mono" style={{color: ok ? "var(--fg-0)" : "var(--fg-2)", fontWeight: ok ? 500 : 400}}>{code || "ERR"}</span>;
}

function parseURL(value) {
  try {
    return new URL(value);
  } catch {
    return null;
  }
}

function isRequestError(request) {
  return request && (request.status === 0 || request.status >= 400 || !!request.error);
}

function queryEntries(parsed) {
  if (!parsed) return [];
  const byKey = new Map();
  parsed.searchParams.forEach((value, key) => {
    const item = byKey.get(key);
    if (item) {
      item.count += 1;
      return;
    }
    byKey.set(key, { key, value, count: 1 });
  });
  return Array.from(byKey.values());
}

function initialSelectedQueryKeys(entries) {
  const volatile = /(^|[_-])(v?hash|sig(nature)?|token|auth|expires?|exp|timestamp|time|ts|vendtime)([_-]|$)/i;
  const selected = entries.filter(item => volatile.test(item.key)).map(item => item.key);
  return selected.length ? selected : [];
}

function splitURLParts(parsed) {
  return {
    host: parsed.hostname.split(".").filter(Boolean).map(value => ({ value, any: false })),
    path: parsed.pathname.split("/").filter(Boolean).map(value => ({ value, any: false })),
  };
}

function patternFromParts(parts, separator, prefix = "") {
  return prefix + parts.map(part => part.any ? "*" : part.value).join(separator);
}

function ruleNameFromPatterns(hostPattern, pathPattern) {
  const tail = pathPattern.split("/").filter(Boolean).slice(-1)[0] || "url";
  return `${hostPattern}-${tail}`.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "") || "traffic-url-rule";
}

function URLPartButtons({ title, parts, onToggle }) {
  return (
    <div className="url-rule-section">
      <div className="url-rule-label">{title}</div>
      <div className="url-rule-parts">
        {parts.map((part, index) => (
          <React.Fragment key={`${title}-${index}`}>
            {index > 0 && <span className="url-rule-sep">{title === "Host" ? "." : "/"}</span>}
            <button
              type="button"
              className={`url-rule-part mono ${part.any ? "any" : ""}`}
              onClick={() => onToggle(index)}
              title={part.any ? "Match any segment" : "Use exact segment"}
            >
              {part.any ? "*" : part.value}
            </button>
          </React.Fragment>
        ))}
      </div>
    </div>
  );
}

function TrafficRuleBuilder({ request, onSave }) {
  const parsed = React.useMemo(() => parseURL(request?.url), [request?.url]);
  const entries = React.useMemo(() => queryEntries(parsed), [parsed]);
  const [hostParts, setHostParts] = useState([]);
  const [pathParts, setPathParts] = useState([]);
  const [selectedKeys, setSelectedKeys] = useState([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!parsed) return;
    const parts = splitURLParts(parsed);
    setHostParts(parts.host);
    setPathParts(parts.path);
    setSelectedKeys(initialSelectedQueryKeys(entries));
  }, [parsed, entries]);

  const hostPattern = patternFromParts(hostParts, ".", "");
  const pathPattern = patternFromParts(pathParts, "/", "/");
  const previewURL = React.useMemo(() => {
    if (!parsed) return "";
    const clone = new URL(parsed.toString());
    selectedKeys.forEach(key => clone.searchParams.delete(key));
    clone.hash = "";
    clone.search = clone.searchParams.toString();
    return clone.toString();
  }, [parsed, selectedKeys]);

  if (!parsed || entries.length === 0) return null;

  const toggleHost = (index) => setHostParts(parts => parts.map((part, i) => i === index ? { ...part, any: !part.any } : part));
  const togglePath = (index) => setPathParts(parts => parts.map((part, i) => i === index ? { ...part, any: !part.any } : part));
  const toggleKey = (key) => setSelectedKeys(keys => keys.includes(key) ? keys.filter(item => item !== key) : [...keys, key].sort());

  const save = async () => {
    if (!selectedKeys.length || saving) return;
    setSaving(true);
    try {
      return await onSave?.({
        name: ruleNameFromPatterns(hostPattern, pathPattern),
        match: {
          host_pattern: hostPattern,
          path_pattern: pathPattern,
        },
        query: {
          drop_keys: selectedKeys,
          sort: true,
        },
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="url-rule-builder">
      <div className="url-rule-head">
        <div>
          <div className="url-rule-title">Error merge rule</div>
          <div className="url-rule-copy">Set path segments to * and choose query keys to ignore.</div>
        </div>
      </div>

      <URLPartButtons title="Host" parts={hostParts} onToggle={toggleHost}/>
      <URLPartButtons title="Path" parts={pathParts} onToggle={togglePath}/>

      <div className="url-rule-section">
        <div className="url-rule-label">Drop query keys</div>
        <div className="url-rule-query-list">
          {entries.map(item => (
            <label className="url-rule-query" key={item.key}>
              <input type="checkbox" checked={selectedKeys.includes(item.key)} onChange={() => toggleKey(item.key)} />
              <span className="mono url-rule-query-key">{item.key}</span>
              <span className="mono url-rule-query-value">{item.value}{item.count > 1 ? ` +${item.count - 1}` : ""}</span>
            </label>
          ))}
        </div>
      </div>

      <div className="url-rule-preview">
        <div className="url-rule-label">Rule</div>
        <div className="mono">host: {hostPattern}</div>
        <div className="mono">path: {pathPattern}</div>
        <div className="url-rule-label" style={{marginTop:10}}>Grouped target</div>
        <div className="mono url-rule-preview-url">{previewURL}</div>
      </div>

      <div className="row" style={{justifyContent:"flex-end", marginTop:12}}>
        <button className="btn primary" disabled={!selectedKeys.length || saving} onClick={save}>
          <Ic.check/> {saving ? "Saving..." : "Save rule"}
        </button>
      </div>
    </div>
  );
}

function RequestDetailContent({ request, onSaveGroupingRule }) {
  if (!request) return null;
  const replayRegion = request.region || (window.PG?.baseRegionCode ? window.PG.baseRegionCode(request.group) : String(request.group || "").replace(/-RES/g, "").replace(/-PREM/g, ""));
  const [ruleOpen, setRuleOpen] = useState(false);
  const parsed = parseURL(request.url);
  const canEditRule = !!onSaveGroupingRule && isRequestError(request) && queryEntries(parsed).length > 0;
  const saveGroupingRule = async (rule) => {
    const result = await onSaveGroupingRule(rule);
    if (result !== null) setRuleOpen(false);
    return result;
  };
  return (
    <div className="request-detail">
      <div className="row" style={{gap:8, flexWrap:"wrap", marginBottom:24}}>
        <span className="pill mono"><StatusCode code={request.status}/></span>
        <span className="pill mono">{request.method}</span>
        {request.type === "tunnel" && <span className="pill res">WS tunnel</span>}
        <RegionPill code={request.group} residential={request.residential}/>
        {request.group_count > 1 && <span className="pill mono">x{request.group_count}</span>}
        {request.template && <span className="pill mono">template</span>}
        {canEditRule && (
          <button className="btn sm" onClick={() => setRuleOpen(open => !open)} style={{marginLeft:"auto"}}>
            <Ic.filter/> Ignore URL params
          </button>
        )}
      </div>
      <div className="kv" style={{rowGap:14}}>
        <div className="k">Time</div><div className="v mono">{fmtUTC8(request.ts)}</div>
        <div className="k">URL</div><div className="v mono" style={{wordBreak:"break-all", fontSize:11.5, lineHeight:1.5}}>{request.url}</div>
        {request.group_target && <><div className="k">Group target</div><div className="v mono" style={{wordBreak:"break-all", fontSize:11.5, lineHeight:1.5}}>{request.group_target}</div></>}
        <div className="k">Strategy</div><div className="v mono">{request.strategy}</div>
        <div className="k">Pool</div><div className="v mono">{request.pool}</div>
        <div className="k">Node</div><div className="v mono" style={{fontSize:11.5}}>{request.node}</div>
        <div className="k">TLS</div><div className="v mono">{request.tls || "default"}</div>
        <div className="k">Duration</div><div className="v mono">{request.duration_ms} ms</div>
        <div className="k">Bytes</div><div className="v mono">{fmtBytes(request.bytes)}</div>
        {request.error && <><div className="k">Error</div><div className="v mono" style={{fontSize:11.5}}>{request.error}</div></>}
      </div>
      {ruleOpen && <TrafficRuleBuilder request={request} onSave={saveGroupingRule}/>}
      <div className="sep-h"/>
      <div className="muted-2" style={{fontSize:10.5, textTransform:"uppercase", letterSpacing:"0.06em", marginBottom:10, fontWeight:500}}>Replay payload</div>
      <pre className="mono request-replay" style={{margin:0, padding:14, background:"var(--bg-2)", border:"1px solid var(--line-2)", borderRadius:6, fontSize:11.5, overflow:"auto", lineHeight:1.55, color:"var(--fg-1)"}}>{`POST /proxy
Authorization: Bearer <proxy_token>

{
  "url": "${request.url}",
  "method": "${request.method}",
  "egress": {
    "region": "${replayRegion}",
    "strategy": "${request.strategy}",
    "residential": ${request.residential},
    "premium": ${request.premium},
    "tls_fingerprint": "${request.tls}"
  }
}`}</pre>
    </div>
  );
}

window.UI = { ToastProvider, useToast, Modal, Drawer, Toggle, Seg, RegionPill, PremiumDot, StatusDot, LatencyBar, Sparkline, BarChart, fmtBytes, fmtAgo, fmtUTC8, StatusCode, RequestDetailContent };
