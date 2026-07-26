// Subscriptions
const { Modal, Drawer, Toggle, Seg, RegionPill, PremiumDot, useToast } = window.UI;

function subscriptionNodeGroup(node) {
  return window.PG.egressGroupCode(node.region, node.residential);
}

function PageSubscriptions({ state, dispatch }) {
  const { pools } = state;
  const subs = pools.filter(p => p.source === "subscription");
  const [addOpen, setAddOpen] = React.useState(false);
  const [openSub, setOpenSub] = React.useState(null);
  const [errorSub, setErrorSub] = React.useState(null);
  const [expandedNodes, setExpandedNodes] = React.useState({});
  const toast = useToast();

  return (
    <div className="page subscriptions-page">
      <div className="page-h">
        <div>
          <h1>Subscriptions</h1>
          <p>Auto-fetched node sources. Region is detected from each node name; override or reject as needed.</p>
        </div>
        <div className="page-h-actions">
          <button className="btn primary" onClick={() => setAddOpen(true)}><Ic.plus/> Add subscription</button>
        </div>
      </div>

      <div className="col gap-16">
        {subs.map(s => {
          const total = s.nodes.length;
          const online = s.nodes.filter(n => n.alive && n.enabled).length;
          const groups = Array.from(new Set(s.nodes.map(subscriptionNodeGroup))).filter(Boolean);
          const expanded = !!expandedNodes[s.name];
          const visibleNodes = expanded ? s.nodes : s.nodes.slice(0, 14);
          const errorSummary = subscriptionErrorSummary(s.last_error);
          return (
            <div key={s.name} className="card subscription-card">
              <div className="card-h bordered subscription-card-header">
                <div className="row gap-12 subscription-title">
                  <h3 className="mono">{s.name}</h3>
                  {s.residential && <span className="pill res">residential</span>}
                  {s.try_offline && <span className="pill mono">try offline</span>}
                  {s.allow_private_host && <span className="pill mono subscription-local-pill">local source</span>}
                </div>
                <div className="right subscription-actions">
                  <span className="muted-2 mono subscription-runtime">refresh {s.update_interval || "manual"} · last {s.last_updated}</span>
                  <button className="btn sm ghost" onClick={() => dispatch({type:"refreshSub", name: s.name})}><Ic.refresh/> Refresh</button>
                  <button className="btn sm ghost" onClick={() => setOpenSub(s)}><Ic.edit/> Edit</button>
                  <Toggle on={s.enabled} onChange={() => dispatch({type:"togglePool", name:s.name})}/>
                </div>
              </div>

              <div className="card-body subscription-summary-grid">
                <div className="subscription-summary-item">
                  <div className="subscription-section-label">Source</div>
                  <div className="mono truncate subscription-url">{s.url}</div>
                  {s.last_error && (
                    <button className="subscription-error-button" onClick={() => setErrorSub(s)}>
                      <Ic.alert/>
                      <span className="subscription-error-summary">{errorSummary}</span>
                      <span className="subscription-error-action">View</span>
                    </button>
                  )}
                </div>
                <div className="subscription-summary-item">
                  <div className="subscription-section-label">Nodes online</div>
                  <div className="mono" style={{fontSize:24, fontWeight:400, letterSpacing:"-0.02em", marginTop:10, lineHeight:1}}>
                    {online}<span className="muted-2" style={{fontSize:13}}>/{total}</span>
                  </div>
                  <div className="progress" style={{marginTop:10}}><div style={{width: `${(online/total)*100||0}%`}}/></div>
                </div>
                <div className="subscription-summary-item">
                  <div className="subscription-section-label">Region groups</div>
                  <div className="row" style={{gap:4, flexWrap:"wrap", marginTop:10}}>
                    {groups.map(code => {
                      const node = s.nodes.find(n => subscriptionNodeGroup(n) === code) || {};
                      return <RegionPill key={code} code={code} residential={node.residential}/>;
                    })}
                  </div>
                </div>
                <div className="subscription-summary-item">
                  <div className="subscription-section-label">Reject filters</div>
                  <div className="col" style={{gap:4, marginTop:10}}>
                    {(s.reject_regex || []).slice(0,3).map((r,i) => <span key={i} className="mono muted truncate" style={{fontSize:11}}>{r}</span>)}
                    {!(s.reject_regex || []).length && <span className="muted-2" style={{fontSize:11}}>none</span>}
                  </div>
                </div>
              </div>

              <div className="subscription-node-strip">
                <span className="subscription-section-label subscription-node-label">Nodes</span>
                {visibleNodes.map(n => (
                  <span key={n.id} className="pill mono subscription-node-pill" style={{opacity: n.alive && n.enabled ? 1 : 0.45}}>
                    <span style={{display:"inline-block",width:5,height:5,borderRadius:"50%",background: n.alive && n.enabled ? "#fafafa" : "var(--fg-3)", boxShadow: n.alive && n.enabled ? "0 0 4px rgba(255,255,255,0.5)" : "none"}}/>
                    <span className="truncate subscription-node-name">{n.name}</span>
                    {n.premium && <PremiumDot/>}
                  </span>
                ))}
                {s.nodes.length > 14 && (
                  <button className="btn sm ghost mono" style={{height:26, padding:"0 9px", fontSize:11}}
                    onClick={() => setExpandedNodes(v => ({ ...v, [s.name]: !expanded }))}>
                    {expanded ? "Show less" : `+${s.nodes.length - 14}`}
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      <Drawer open={!!openSub} onClose={() => setOpenSub(null)} title={openSub?.name || ""}>
        {openSub && <SubEditor sub={openSub} dispatch={dispatch} onClose={() => setOpenSub(null)} toast={toast}/>}
      </Drawer>

      <SubscriptionErrorModal sub={errorSub} onClose={() => setErrorSub(null)} />
      <AddSubscriptionModal open={addOpen} onClose={() => setAddOpen(false)} dispatch={dispatch} toast={toast}/>
    </div>
  );
}

function subscriptionErrorSummary(value) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (!text) return "";
  return text.length > 96 ? `${text.slice(0, 93)}...` : text;
}

function SubscriptionErrorModal({ sub, onClose }) {
  return (
    <Modal open={!!sub} onClose={onClose} title={sub ? `${sub.name} error` : "Subscription error"} className="wide">
      <div className="col gap-12">
        <div>
          <div className="subscription-section-label">Source</div>
          <div className="mono subscription-url" style={{overflowWrap:"anywhere", whiteSpace:"normal"}}>{sub?.url}</div>
        </div>
        <pre className="subscription-error-detail">{sub?.last_error || ""}</pre>
      </div>
    </Modal>
  );
}

function parseSubscriptionInterval(value) {
  const raw = String(value || "").trim().toLowerCase();
  if (!raw) return { mode: "manual", amount: 1 };
  const match = raw.match(/^([\d.]+)\s*(m|h|d)$/);
  if (!match) return { mode: "hours", amount: 1 };
  const amount = Math.max(1, Number(match[1]) || 1);
  if (match[2] === "d") return { mode: "days", amount };
  if (match[2] === "h") return { mode: "hours", amount };
  return { mode: "minutes", amount };
}

function formatSubscriptionInterval(mode, amount) {
  if (mode === "manual") return "";
  const n = Math.max(1, Number(amount) || 1);
  if (mode === "days") return `${n}d`;
  if (mode === "hours") return `${n}h`;
  return `${n}m`;
}

function SubscriptionIntervalControl({ value, onChange }) {
  const parsed = parseSubscriptionInterval(value);
  const setMode = (mode) => onChange(formatSubscriptionInterval(mode, parsed.amount));
  const setAmount = (amount) => onChange(formatSubscriptionInterval(parsed.mode, amount));
  return (
    <div className="field">
      <label className="field-label">Refresh interval</label>
      <div className="col gap-12">
        <Seg value={parsed.mode} onChange={setMode}
          options={[
            {value:"manual", label:"Manual"},
            {value:"minutes", label:"Minutes"},
            {value:"hours", label:"Hours"},
            {value:"days", label:"Days"},
          ]}/>
        {parsed.mode !== "manual" && (
          <input className="input mono" type="number" min="1" step="1"
            value={parsed.amount} onChange={e => setAmount(e.target.value)} />
        )}
      </div>
    </div>
  );
}

function SubEditor({ sub, dispatch, onClose, toast }) {
  const sourceForm = (pool) => ({
    name: pool.name || "",
    url: pool.url || "",
    update_interval: pool.update_interval == null ? "1h" : pool.update_interval,
    residential: !!pool.residential,
    premium: !!pool.premium,
    try_offline: !!pool.try_offline,
    allow_private_host: !!pool.allow_private_host,
  });
  const regionsFromNodes = (nodes) => Object.fromEntries((nodes || []).map(n => [n.name, n.region || ""]));
  const tagsFromNodes = (nodes) => Object.fromEntries((nodes || []).map(n => [n.name, (n.tags || []).join(", ")]));
  const [tab, setTab] = React.useState("source");
  const [source, setSource] = React.useState(() => sourceForm(sub));
  const [regionDraft, setRegionDraft] = React.useState(() => regionsFromNodes(sub.nodes));
  const [tagDraft, setTagDraft] = React.useState(() => tagsFromNodes(sub.nodes));
  const [aliasNode, setAliasNode] = React.useState(sub.nodes[0]?.name || "");
  const [alias, setAlias] = React.useState(sub.nodes[0]?.alias || "");
  const [reject, setReject] = React.useState((sub.reject_regex || []).join("\n"));
  React.useEffect(() => {
    setTab("source");
    setSource(sourceForm(sub));
    setRegionDraft(regionsFromNodes(sub.nodes));
    setTagDraft(tagsFromNodes(sub.nodes));
    setAliasNode(sub.nodes[0]?.name || "");
    setAlias(sub.nodes[0]?.alias || "");
    setReject((sub.reject_regex || []).join("\n"));
  }, [sub.name]);
  const setSourceField = (key, value) => setSource(v => ({ ...v, [key]: value }));
  const tabs = [
    {value:"source", label:"Source"},
    {value:"regions", label:"Region overrides"},
    {value:"aliases", label:"Aliases"},
    {value:"tags", label:"Tags"},
    {value:"reject", label:"Reject regex"},
  ];
  return (
    <>
      <div className="row" style={{gap:0, marginBottom:20, borderBottom:"1px solid var(--line)"}}>
        {tabs.map(t => (
          <button key={t.value} onClick={() => setTab(t.value)}
            style={{
              background:"transparent", border:"none",
              padding:"10px 14px", fontSize:12.5, fontWeight:450,
              color: tab === t.value ? "var(--fg-0)" : "var(--fg-3)",
              borderBottom: tab === t.value ? "1.5px solid var(--fg-0)" : "1.5px solid transparent",
              marginBottom:-1, cursor:"pointer", fontFamily:"inherit",
              transition:"all 200ms var(--ease)",
            }}>{t.label}</button>
        ))}
      </div>
      {tab === "source" && (
        <div className="col gap-12">
          <div className="field"><label className="field-label">Display name</label>
            <input className="input mono" value={source.name} onChange={e => setSourceField("name", e.target.value)} placeholder="airport-a"/></div>
          <div className="field"><label className="field-label">Subscription URL(s)</label>
            <textarea className="input mono" value={source.url} onChange={e => setSourceField("url", e.target.value)} style={{minHeight:120}} spellCheck="false"></textarea>
            <div className="field-hint">Multiple URLs separated by newline, comma or |.</div></div>
          <div className={`subscription-network-access ${source.allow_private_host ? "enabled" : ""}`}>
            <div>
              <div className="field-label">Local network access</div>
              <div className="field-hint">Allow literal private or loopback IPs and domains that resolve to local addresses.</div>
            </div>
            <Toggle on={source.allow_private_host} onChange={v => setSourceField("allow_private_host", v)} label="Allow local addresses"/>
          </div>
          <div className="field-row">
            <SubscriptionIntervalControl value={source.update_interval} onChange={v => setSourceField("update_interval", v)}/>
            <div className="field"><label className="field-label">Class</label>
              <Toggle on={source.residential} onChange={v => setSourceField("residential", v)} label="Residential pool"/></div>
          </div>
          <div className="field"><label className="field-label">Premium</label>
            <Toggle on={source.premium} onChange={v => setSourceField("premium", v)} label="Premium pool"/></div>
          <div className="field"><label className="field-label">Fallback</label>
            <Toggle on={source.try_offline} onChange={v => setSourceField("try_offline", v)} label="Try offline singleton"/></div>
          <div className="row gap-12" style={{marginTop:4, flexWrap:"wrap"}}>
            <button className="btn primary" onClick={async () => {
              const nextName = source.name.trim();
              if (!nextName) { toast("Display name is required"); return; }
              if (!source.url.trim()) { toast("Subscription URL is required"); return; }
              const ok = await dispatch({type:"updatePoolConfig", pool: sub.name, patch:{
                url: source.url.trim(),
                update_interval: source.update_interval.trim(),
                allow_private_host: source.allow_private_host,
                residential: source.residential,
                premium: source.premium,
                try_offline: source.try_offline,
              }, newName: nextName});
              if (ok) onClose();
            }}><Ic.check/> Save & fetch</button>
            <button className="btn danger ml-auto" onClick={async () => {
              if (!window.confirm(`Delete subscription pool "${sub.name}"?`)) return;
              const ok = await dispatch({type:"deletePool", pool: sub.name});
              if (ok) onClose();
            }}><Ic.trash/> Delete subscription</button>
          </div>
        </div>
      )}
      {tab === "regions" && (
        <div className="col gap-12">
          <div className="field-hint">Override auto-detected region codes for mis-tagged nodes.</div>
          {sub.nodes.slice(0, 12).map(n => (
            <div key={n.id} className="row gap-12">
              <div className="mono truncate" style={{flex:1, fontSize:11.5}}>{n.name}</div>
              <input className="input mono" value={regionDraft[n.name] || ""} onChange={e => setRegionDraft(v => ({...v, [n.name]: e.target.value.toUpperCase().slice(0,2)}))} maxLength={2} style={{width:64}}/>
            </div>
          ))}
          <button className="btn primary" style={{alignSelf:"flex-start"}} onClick={async () => {
            for (const n of sub.nodes.slice(0, 12)) {
              await dispatch({type:"updateSubscriptionNode", payload:{
                pool: sub.name, node: n.name, region: regionDraft[n.name] || "", alias: n.alias || "", tags: n.tags || []
              }});
            }
            onClose();
          }}><Ic.check/> Save</button>
        </div>
      )}
      {tab === "aliases" && (
        <div className="col gap-12">
          <div className="field-hint">Map an alias to a specific node so callers can pin to it.</div>
          <div className="row gap-12">
            <input className="input mono" placeholder="alias" value={alias} onChange={e => setAlias(e.target.value)}/>
            <span className="muted-2">→</span>
            <select className="select" value={aliasNode} onChange={e => {
              const node = sub.nodes.find(n => n.name === e.target.value);
              setAliasNode(e.target.value);
              setAlias(node?.alias || "");
            }}>{sub.nodes.map(n => <option key={n.id}>{n.name}</option>)}</select>
          </div>
          <button className="btn primary" style={{alignSelf:"flex-start"}} onClick={async () => {
            const node = sub.nodes.find(n => n.name === aliasNode);
            if (!node) return;
            const ok = await dispatch({type:"updateSubscriptionNode", payload:{
              pool: sub.name, node: node.name, region: node.region || "", alias, tags: node.tags || []
            }});
            if (ok) onClose();
          }}><Ic.check/> Save</button>
        </div>
      )}
      {tab === "tags" && (
        <div className="col gap-12">
          <div className="field-hint">Manually tag nodes — including special <span className="mono">residential</span> and <span className="mono">premium</span> tags.</div>
          {sub.nodes.slice(0, 12).map(n => (
            <div key={n.id} className="row gap-12">
              <div className="mono truncate" style={{flex:1, fontSize:11.5}}>{n.name}</div>
              <input className="input mono" value={tagDraft[n.name] || ""} onChange={e => setTagDraft(v => ({...v, [n.name]: e.target.value}))} placeholder="streaming, residential, premium" style={{width:200}}/>
            </div>
          ))}
          <button className="btn primary" style={{alignSelf:"flex-start"}} onClick={async () => {
            for (const n of sub.nodes.slice(0, 12)) {
              await dispatch({type:"updateSubscriptionNode", payload:{
                pool: sub.name, node: n.name, region: n.region || "", alias: n.alias || "",
                tags: (tagDraft[n.name] || "").split(",").map(s => s.trim()).filter(Boolean)
              }});
            }
            onClose();
          }}><Ic.check/> Save</button>
        </div>
      )}
      {tab === "reject" && (
        <div className="col gap-12">
          <div className="field-hint">Nodes whose names match any expression are excluded from the pool.</div>
          <textarea className="input mono" value={reject} onChange={e => setReject(e.target.value)} style={{minHeight:140}}></textarea>
          <button className="btn primary" style={{alignSelf:"flex-start"}} onClick={async () => {
            const ok = await dispatch({type:"updatePoolConfig", pool: sub.name, patch:{
              reject_regex: reject.split("\n").map(s => s.trim()).filter(Boolean)
            }});
            if (ok) onClose();
          }}><Ic.check/> Save</button>
        </div>
      )}
    </>
  );
}

function AddSubscriptionModal({ open, onClose, dispatch, toast }) {
  const [form, setForm] = React.useState({
    name: "",
    url: "",
    update_interval: "1h",
    residential: false,
    premium: false,
    try_offline: false,
    allow_private_host: false,
    reject_regex: "",
  });
  const set = (k, v) => setForm(f => ({ ...f, [k]: v }));
  const submit = async () => {
    if (!form.name.trim() || !form.url.trim()) { toast("Pool name and URL are required"); return; }
    const ok = await dispatch({type:"addPool", name: form.name.trim(), config:{
      source: "subscription",
      enabled: true,
      url: form.url.trim(),
      update_interval: form.update_interval,
      allow_private_host: form.allow_private_host,
      residential: form.residential,
      premium: form.premium,
      try_offline: form.try_offline,
      reject_regex: form.reject_regex.split("\n").map(s => s.trim()).filter(Boolean),
    }});
    if (ok) onClose();
  };
  return (
    <Modal open={open} onClose={onClose} title="Add subscription"
      footer={<>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit}><Ic.check/> Add & fetch</button>
      </>}>
      <div className="col gap-12">
        <div className="field"><label className="field-label">Pool name</label>
          <input className="input mono" value={form.name} onChange={e => set("name", e.target.value)} placeholder="airport-c"/></div>
        <div className="field"><label className="field-label">Subscription URL(s)</label>
          <textarea className="input mono" value={form.url} onChange={e => set("url", e.target.value)} placeholder="https://provider/sub?token=..."></textarea>
          <div className="field-hint">Multiple URLs separated by newline, comma or |. Failures don't block working sources.</div></div>
        <div className={`subscription-network-access ${form.allow_private_host ? "enabled" : ""}`}>
          <div>
            <div className="field-label">Local network access</div>
            <div className="field-hint">Allow literal private or loopback IPs and domains that resolve to local addresses.</div>
          </div>
          <Toggle on={form.allow_private_host} onChange={v => set("allow_private_host", v)} label="Allow local addresses"/>
        </div>
        <div className="field-row">
          <SubscriptionIntervalControl value={form.update_interval} onChange={v => set("update_interval", v)}/>
          <div className="field"><label className="field-label">Class</label>
            <Toggle on={form.residential} onChange={v => set("residential", v)} label="Residential pool"/></div>
        </div>
        <div className="field"><label className="field-label">Premium</label>
          <Toggle on={form.premium} onChange={v => set("premium", v)} label="Premium pool"/></div>
        <div className="field"><label className="field-label">Fallback</label>
          <Toggle on={form.try_offline} onChange={v => set("try_offline", v)} label="Try offline singleton"/></div>
        <div className="field"><label className="field-label">Reject regex (one per line)</label>
          <textarea className="input mono" value={form.reject_regex} onChange={e => set("reject_regex", e.target.value)} placeholder={"流量|套餐|官网\n到期|剩余"}></textarea></div>
      </div>
    </Modal>
  );
}

window.PageSubscriptions = PageSubscriptions;
