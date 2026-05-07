// Subscriptions
const { Modal, Drawer, Toggle, RegionPill, useToast } = window.UI;

function PageSubscriptions({ state, dispatch }) {
  const { pools } = state;
  const subs = pools.filter(p => p.source === "subscription");
  const [addOpen, setAddOpen] = React.useState(false);
  const [openSub, setOpenSub] = React.useState(null);
  const toast = useToast();

  return (
    <div className="page">
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
          const regions = Array.from(new Set(s.nodes.map(n => n.region))).filter(Boolean);
          return (
            <div key={s.name} className="card">
              <div className="card-h bordered">
                <div className="row gap-12">
                  <h3 className="mono">{s.name}</h3>
                  {s.residential && <span className="pill res">residential</span>}
                </div>
                <div className="right">
                  <span className="muted-2 mono" style={{fontSize:11, marginRight:8}}>refresh {s.update_interval} · last {s.last_updated}</span>
                  <button className="btn sm ghost" onClick={() => dispatch({type:"refreshSub", name: s.name})}><Ic.refresh/> Refresh</button>
                  <button className="btn sm ghost" onClick={() => setOpenSub(s)}><Ic.edit/> Edit</button>
                  <Toggle on={s.enabled} onChange={() => dispatch({type:"togglePool", name:s.name})}/>
                </div>
              </div>

              <div className="card-body" style={{display:"grid", gridTemplateColumns:"1.4fr 1fr 1fr 1fr", gap:32}}>
                <div>
                  <div className="muted-2" style={{fontSize:10.5, textTransform:"uppercase", letterSpacing:"0.06em", fontWeight:500}}>Source</div>
                  <div className="mono truncate" style={{fontSize:11.5, marginTop:8, color:"var(--fg-1)"}}>{s.url}</div>
                  {s.last_error && <div style={{marginTop:10, fontSize:11.5, color:"var(--fg-2)", display:"flex",alignItems:"center",gap:6}}><span style={{width:5,height:5,borderRadius:"50%",background:"var(--fg-1)"}}/>{s.last_error}</div>}
                </div>
                <div>
                  <div className="muted-2" style={{fontSize:10.5, textTransform:"uppercase", letterSpacing:"0.06em", fontWeight:500}}>Nodes online</div>
                  <div className="mono" style={{fontSize:24, fontWeight:400, letterSpacing:"-0.02em", marginTop:10, lineHeight:1}}>
                    {online}<span className="muted-2" style={{fontSize:13}}>/{total}</span>
                  </div>
                  <div className="progress" style={{marginTop:10}}><div style={{width: `${(online/total)*100||0}%`}}/></div>
                </div>
                <div>
                  <div className="muted-2" style={{fontSize:10.5, textTransform:"uppercase", letterSpacing:"0.06em", fontWeight:500}}>Region groups</div>
                  <div className="row" style={{gap:4, flexWrap:"wrap", marginTop:10}}>
                    {regions.map(r => <RegionPill key={r} code={r}/>)}
                  </div>
                </div>
                <div>
                  <div className="muted-2" style={{fontSize:10.5, textTransform:"uppercase", letterSpacing:"0.06em", fontWeight:500}}>Reject filters</div>
                  <div className="col" style={{gap:4, marginTop:10}}>
                    {(s.reject_regex || []).slice(0,3).map((r,i) => <span key={i} className="mono muted truncate" style={{fontSize:11}}>{r}</span>)}
                    {!(s.reject_regex || []).length && <span className="muted-2" style={{fontSize:11}}>none</span>}
                  </div>
                </div>
              </div>

              <div style={{borderTop:"1px solid var(--line)", padding:"14px 20px", display:"flex", gap:6, flexWrap:"wrap", alignItems:"center"}}>
                <span className="muted-2" style={{fontSize:10.5, textTransform:"uppercase", letterSpacing:"0.06em", marginRight:6, fontWeight:500}}>Nodes</span>
                {s.nodes.slice(0, 14).map(n => (
                  <span key={n.id} className="pill mono" style={{maxWidth:200, opacity: n.alive && n.enabled ? 1 : 0.45}}>
                    <span style={{display:"inline-block",width:5,height:5,borderRadius:"50%",background: n.alive && n.enabled ? "#fafafa" : "var(--fg-3)", boxShadow: n.alive && n.enabled ? "0 0 4px rgba(255,255,255,0.5)" : "none"}}/>
                    <span className="truncate" style={{maxWidth:160}}>{n.name}</span>
                  </span>
                ))}
                {s.nodes.length > 14 && <span className="muted-2 mono" style={{fontSize:11}}>+{s.nodes.length - 14}</span>}
              </div>
            </div>
          );
        })}
      </div>

      <Drawer open={!!openSub} onClose={() => setOpenSub(null)} title={openSub?.name || ""}>
        {openSub && <SubEditor sub={openSub} dispatch={dispatch} onClose={() => setOpenSub(null)} toast={toast}/>}
      </Drawer>

      <AddSubscriptionModal open={addOpen} onClose={() => setAddOpen(false)} dispatch={dispatch} toast={toast}/>
    </div>
  );
}

function SubEditor({ sub, dispatch, onClose, toast }) {
  const sourceForm = (pool) => ({
    name: pool.name || "",
    url: pool.url || "",
    update_interval: pool.update_interval || "1h",
    residential: !!pool.residential,
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
          <div className="field-row">
            <div className="field"><label className="field-label">Refresh interval</label>
              <input className="input mono" value={source.update_interval} onChange={e => setSourceField("update_interval", e.target.value)} placeholder="1h"/></div>
            <div className="field"><label className="field-label">Class</label>
              <Toggle on={source.residential} onChange={v => setSourceField("residential", v)} label="Residential pool"/></div>
          </div>
          <div className="row gap-12" style={{marginTop:4, flexWrap:"wrap"}}>
            <button className="btn primary" onClick={async () => {
              const nextName = source.name.trim();
              if (!nextName) { toast("Display name is required"); return; }
              if (!source.url.trim()) { toast("Subscription URL is required"); return; }
              const ok = await dispatch({type:"updatePoolConfig", pool: sub.name, patch:{
                url: source.url.trim(),
                update_interval: source.update_interval.trim(),
                residential: source.residential,
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
          <div className="field-hint">Manually tag nodes — including the special <span className="mono">residential</span> tag.</div>
          {sub.nodes.slice(0, 12).map(n => (
            <div key={n.id} className="row gap-12">
              <div className="mono truncate" style={{flex:1, fontSize:11.5}}>{n.name}</div>
              <input className="input mono" value={tagDraft[n.name] || ""} onChange={e => setTagDraft(v => ({...v, [n.name]: e.target.value}))} placeholder="streaming, residential" style={{width:200}}/>
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
      residential: form.residential,
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
        <div className="field-row">
          <div className="field"><label className="field-label">Refresh interval</label>
            <select className="select" value={form.update_interval} onChange={e => set("update_interval", e.target.value)}>
              <option>15m</option><option>30m</option><option>1h</option><option>6h</option><option>24h</option>
            </select></div>
          <div className="field"><label className="field-label">Class</label>
            <Toggle on={form.residential} onChange={v => set("residential", v)} label="Residential pool"/></div>
        </div>
        <div className="field"><label className="field-label">Reject regex (one per line)</label>
          <textarea className="input mono" value={form.reject_regex} onChange={e => set("reject_regex", e.target.value)} placeholder={"流量|套餐|官网\n到期|剩余"}></textarea></div>
      </div>
    </Modal>
  );
}

window.PageSubscriptions = PageSubscriptions;
