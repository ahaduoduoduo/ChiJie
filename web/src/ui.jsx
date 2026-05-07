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

function Modal({ open, onClose, title, children, footer }) {
  if (!open) return null;
  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
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
  const baseCode = code?.replace(/-RES$/, "");
  const flagEmoji = flag || (window.PG?.REGION_FLAGS?.[baseCode]) || null;
  return (
    <span className={`pill region ${residential ? "res" : ""}`}>
      {flagEmoji && <span style={{fontSize:11, lineHeight:1, marginRight:1, filter:"saturate(0.85)"}}>{flagEmoji}</span>}
      {code}
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
// Status code rendered tonally — no color, only weight
function StatusCode({ code }) {
  const ok = code >= 200 && code < 400;
  return <span className="mono" style={{color: ok ? "var(--fg-0)" : "var(--fg-2)", fontWeight: ok ? 500 : 400}}>{code || "ERR"}</span>;
}

window.UI = { ToastProvider, useToast, Modal, Drawer, Toggle, Seg, RegionPill, StatusDot, LatencyBar, Sparkline, BarChart, fmtBytes, fmtAgo, StatusCode };
