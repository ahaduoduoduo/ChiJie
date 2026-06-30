// Egress — region groups + node table
const { Modal, Drawer, Toggle, Seg, RegionPill, PremiumDot, StatusDot, LatencyBar, useToast } = window.UI;

function nodeGroupCode(node) {
  return window.PG.egressGroupCode(node.region, node.residential);
}

function PageEgress({ state, dispatch }) {
  const { pools, regionGroups } = state;
  const [filter, setFilter] = React.useState("all");
  const [search, setSearch] = React.useState("");
  const [groupFilter, setGroupFilter] = React.useState(null);
  const [drawerNode, setDrawerNode] = React.useState(null);
  const [metaNode, setMetaNode] = React.useState(null);
  const [addOpen, setAddOpen] = React.useState(false);
  const toast = useToast();

  const realPools = pools.filter(p => p.source === "static" || p.source === "subscription");
  const allNodes = realPools.flatMap(p => (p.nodes || []).map(n => ({ ...n, pool: p.name, poolSource: p.source })));
  const premiumGroupCodes = new Set(allNodes.filter(n => n.premium).map(nodeGroupCode));

  const filtered = allNodes.filter(n => {
    if (filter === "normal" && n.residential) return false;
    if (filter === "residential" && !n.residential) return false;
    if (filter === "premium" && !n.premium) return false;
    if (groupFilter) {
      const g = nodeGroupCode(n);
      if (g !== groupFilter) return false;
    }
    if (search) {
      const q = search.toLowerCase();
      if (!n.name.toLowerCase().includes(q) && !n.pool.toLowerCase().includes(q) && !(n.region||"").toLowerCase().includes(q) && !(n.server||"").toLowerCase().includes(q) && !(n.alias||"").toLowerCase().includes(q)) return false;
    }
    return true;
  });

  const visibleGroups = regionGroups.filter(g => {
    if (filter === "normal" && g.residential) return false;
    if (filter === "residential" && !g.residential) return false;
    if (filter === "premium" && !premiumGroupCodes.has(g.code)) return false;
    return true;
  });

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>Egress</h1>
          <p>Region groups assemble static and subscription nodes by their region code. Templates back up cold regions automatically.</p>
        </div>
        <div className="page-h-actions">
          <Seg value={filter} onChange={setFilter} options={[
            {value:"all", label:"All"},
            {value:"normal", label:"Normal"},
            {value:"residential", label:"Residential"},
            {value:"premium", label:"Premium"},
          ]}/>
          <button className="btn" onClick={() => dispatch({type:"refreshData"})}><Ic.refresh/> Refresh status</button>
          <button className="btn primary" onClick={() => setAddOpen(true)}><Ic.plus/> Add node</button>
        </div>
      </div>

      <div className="card" style={{marginBottom:24}}>
        <div className="card-h bordered">
          <h3>Region groups</h3>
          <span className="sub">click to filter the table below</span>
          <div className="right">
            {groupFilter && <button className="btn sm ghost" onClick={() => setGroupFilter(null)}><Ic.x/> Clear</button>}
          </div>
        </div>
        <div style={{display:"grid", gridTemplateColumns:"repeat(auto-fit, minmax(176px, 1fr))", gap:1, background:"var(--line)"}}>
          {visibleGroups.map(g => {
            const active = groupFilter === g.code;
            return (
              <div key={g.code}
                onClick={() => setGroupFilter(active ? null : g.code)}
                style={{
                  padding:"16px 18px",
                  background: active ? "rgba(255,255,255,0.04)" : "var(--bg-1)",
                  cursor:"pointer",
                  transition:"background 200ms var(--ease)",
                  position:"relative",
                }}>
                <div className="row" style={{justifyContent:"space-between"}}>
                  <RegionPill code={g.code} residential={g.residential}/>
                  {g.templateBackup && <span style={{fontSize:10, color:"var(--fg-3)"}} className="mono">tpl</span>}
                </div>
                <div style={{marginTop:14, fontSize:22, fontWeight:400, letterSpacing:"-0.02em", fontFamily:"'JetBrains Mono', monospace", lineHeight:1}}>
                  {g.online}<span style={{color:"var(--fg-3)", fontSize:13}}>/{g.count}</span>
                </div>
                <div style={{marginTop:8, color:"var(--fg-3)", fontSize:11}} className="mono">
                  {g.minLatency ? `${g.minLatency}ms min` : "no signal"}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div className="card card-pad-0">
        <div className="card-h bordered">
          <h3>Egress nodes</h3>
          <span className="sub">{filtered.length} of {allNodes.length}</span>
          <div className="right">
            <div style={{position:"relative"}}>
              <Ic.search style={{position:"absolute",left:10,top:9,width:13,height:13,color:"var(--fg-3)"}}/>
              <input className="input" placeholder="Filter nodes…" value={search} onChange={e => setSearch(e.target.value)}
                     style={{paddingLeft:30, width:220}}/>
            </div>
          </div>
        </div>
        <table className="table">
          <thead>
            <tr>
              <th style={{width:48}}></th>
              <th>Node</th>
              <th>Pool</th>
              <th>Region</th>
              <th>Type</th>
              <th>Latency</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {filtered.map(n => (
              <tr key={n.id} onClick={() => setDrawerNode(n)} style={{cursor:"pointer"}}>
                <td onClick={(e) => { e.stopPropagation(); dispatch({type:"toggleNode", id:n.id}); }}>
                  <Toggle on={n.enabled} onChange={() => {}}/>
                </td>
                <td>
                  <div className="row" style={{maxWidth:280, gap:6}}>
                    <div className="strong truncate" style={{fontWeight:450}}>{n.name}</div>
                    {n.premium && <PremiumDot/>}
                  </div>
                  <div className="muted-2 mono" style={{fontSize:10.5, marginTop:2}}>{n.server || "—"}{n.port ? `:${n.port}` : ""}</div>
                </td>
                <td>
                  <div className="mono" style={{fontSize:12}}>{n.pool}</div>
                  <div className="muted-2" style={{fontSize:10.5, marginTop:2}}>{n.poolSource}</div>
                </td>
                <td><RegionPill code={nodeGroupCode(n)} residential={n.residential}/></td>
                <td className="mono muted">{n.type}</td>
                <td><LatencyBar ms={n.latency}/></td>
                <td><StatusDot alive={n.alive} enabled={n.enabled} fail={n.fail_count}/></td>
                <td>
                  <div className="row" style={{justifyContent:"flex-end", gap:2}}>
                    <button className="btn-icon" onClick={async (e) => {
                      e.stopPropagation();
                      const out = await dispatch({type:"testNode", pool:n.pool, node:n.name});
	                      if (out) toast(`${n.name}: ${out.ok ? "connected" : "failed"} · ${out.latency_ms || 0}ms${out.error ? ` · ${out.error}` : ""}`);
                    }} title="Test"><Ic.test/></button>
                    <button className="btn-icon" onClick={(e) => { e.stopPropagation(); setDrawerNode(n); }} title="More"><Ic.dots/></button>
                  </div>
                </td>
              </tr>
            ))}
            {filtered.length === 0 && <tr><td colSpan="8"><div className="empty">No nodes match your filters.</div></td></tr>}
          </tbody>
        </table>
      </div>

      <Drawer open={!!drawerNode} onClose={() => setDrawerNode(null)} title={drawerNode?.name || ""}>
        {drawerNode && (
          <>
            <div className="row" style={{gap:8, marginBottom:24, flexWrap:"wrap"}}>
              <RegionPill code={nodeGroupCode(drawerNode)} residential={drawerNode.residential}/>
              <StatusDot alive={drawerNode.alive} enabled={drawerNode.enabled} fail={drawerNode.fail_count}/>
              <span className="pill mono">{drawerNode.type}</span>
              {drawerNode.premium && <PremiumDot label/>}
              {drawerNode.tags?.map(t => <span key={t} className="pill tag">{t}</span>)}
            </div>
            <div className="kv" style={{rowGap:14}}>
              <div className="k">Pool</div><div className="v mono">{drawerNode.pool}</div>
              <div className="k">Source</div><div className="v">{drawerNode.poolSource}</div>
              <div className="k">Server</div><div className="v mono">{drawerNode.server || "—"}</div>
              <div className="k">Port</div><div className="v mono">{drawerNode.port || "—"}</div>
              <div className="k">Region</div><div className="v mono">{drawerNode.region}</div>
              <div className="k">Residential</div><div className="v">{drawerNode.residential ? "yes" : "no"}</div>
              <div className="k">Premium</div><div className="v">{drawerNode.premium ? "yes" : "no"}</div>
              <div className="k">Enabled</div><div className="v"><Toggle on={drawerNode.enabled} onChange={() => { dispatch({type:"toggleNode", id:drawerNode.id}); setDrawerNode({...drawerNode, enabled: !drawerNode.enabled}); }}/></div>
              <div className="k">Last latency</div><div className="v"><LatencyBar ms={drawerNode.latency}/></div>
              <div className="k">Fail count</div><div className="v mono">{drawerNode.fail_count}</div>
              {drawerNode.alias && <><div className="k">Alias</div><div className="v mono">{drawerNode.alias}</div></>}
            </div>
            <div className="sep-h"/>
            <div className="row" style={{gap:8}}>
              <button className="btn" onClick={async () => {
                const out = await dispatch({type:"testNode", pool:drawerNode.pool, node:drawerNode.name});
	                if (out) toast(`${drawerNode.name}: ${out.ok ? "connected" : "failed"} · ${out.latency_ms || 0}ms${out.error ? ` · ${out.error}` : ""}`);
              }}><Ic.test/> Test connectivity</button>
              <button className="btn" onClick={() => setMetaNode(drawerNode)}><Ic.edit/> Edit metadata</button>
              <button className="btn danger ml-auto" disabled={drawerNode.poolSource !== "static"} onClick={async () => {
                const ok = await dispatch({type:"deleteNode", pool:drawerNode.pool, node:drawerNode.name});
                if (ok) setDrawerNode(null);
              }}><Ic.trash/> Remove</button>
            </div>
          </>
        )}
      </Drawer>

      <NodeMetadataModal
        node={metaNode}
        onClose={() => setMetaNode(null)}
        dispatch={dispatch}
        toast={toast}
        onSaved={(updated) => {
          setMetaNode(null);
          setDrawerNode(updated);
        }}
      />

      <AddStaticModal open={addOpen} onClose={() => setAddOpen(false)} pools={realPools.filter(p => p.source === "static")} dispatch={dispatch} toast={toast}/>
    </div>
  );
}

function AddStaticModal({ open, onClose, pools, dispatch, toast }) {
  const [form, setForm] = React.useState({
    pool: pools[0]?.name || "us-fleet",
    name: "", type: "socks5", server: "", port: 1080,
    region: "US", residential: false, premium: false, username: "", password: "", tags: ""
  });
  const set = (k, v) => setForm(f => ({ ...f, [k]: v }));
  React.useEffect(() => {
    setForm(f => pools.some(p => p.name === f.pool) ? f : { ...f, pool: pools[0]?.name || "" });
  }, [pools]);
	  const submit = async () => {
	    if (!form.name || !form.server) { toast("Name and server are required"); return; }
	    if (!form.pool) { toast("Static pool is required"); return; }
	    const ok = await dispatch({ type:"addStaticNode", node: {
	      name: form.name, type: form.type, server: form.server, port: +form.port,
	      region: form.region.toUpperCase().slice(0,2), residential: form.residential, premium: form.premium,
	      username: form.username,
	      password: form.password,
	      enabled: true,
	      tags: form.tags.split(",").map(s => s.trim()).filter(Boolean),
	    }, pool: form.pool });
    if (ok) onClose();
  };
  return (
    <Modal open={open} onClose={onClose} title="Add static node"
      footer={<>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit}><Ic.check/> Add node</button>
      </>}>
      <div className="col gap-12">
        <div className="field"><label className="field-label">Target pool</label>
          <select className="select" value={form.pool} onChange={e => set("pool", e.target.value)}>
            {pools.map(p => <option key={p.name} value={p.name}>{p.name}</option>)}
          </select></div>
        <div className="field-row">
          <div className="field"><label className="field-label">Name</label>
            <input className="input mono" value={form.name} onChange={e => set("name", e.target.value)} placeholder="us-east-3"/></div>
          <div className="field"><label className="field-label">Type</label>
            <select className="select" value={form.type} onChange={e => set("type", e.target.value)}>
              {["socks5","http_proxy","ss","vmess","vless","trojan","hysteria2"].map(t => <option key={t}>{t}</option>)}
            </select></div>
        </div>
        <div className="field-row">
          <div className="field"><label className="field-label">Server</label>
            <input className="input mono" value={form.server} onChange={e => set("server", e.target.value)} placeholder="proxy.example.com"/></div>
          <div className="field"><label className="field-label">Port</label>
            <input className="input mono" value={form.port} onChange={e => set("port", e.target.value)}/></div>
        </div>
	        <div className="field-row">
	          <div className="field"><label className="field-label">Region (ISO 2)</label>
	            <input className="input mono" value={form.region} onChange={e => set("region", e.target.value)} maxLength={2}/></div>
	          <div className="field"><label className="field-label">Class</label>
	            <Toggle on={form.residential} onChange={v => set("residential", v)} label={form.residential ? "Residential" : "Normal"}/></div>
	        </div>
	        <div className="field">
	          <label className="field-label">Premium</label>
	          <Toggle on={form.premium} onChange={v => set("premium", v)} label={form.premium ? "Premium node" : "Standard node"}/>
	        </div>
	        <div className="field-row">
	          <div className="field"><label className="field-label">Username</label>
	            <input className="input mono" value={form.username} onChange={e => set("username", e.target.value)} placeholder="optional"/></div>
	          <div className="field"><label className="field-label">Password</label>
	            <input className="input mono" type="password" value={form.password} onChange={e => set("password", e.target.value)} placeholder="optional"/></div>
	        </div>
	        <div className="field"><label className="field-label">Tags (comma-separated)</label>
	          <input className="input" value={form.tags} onChange={e => set("tags", e.target.value)} placeholder="primary, streaming"/></div>
      </div>
    </Modal>
  );
}

function NodeMetadataModal({ node, onClose, dispatch, toast, onSaved }) {
  const [form, setForm] = React.useState({ name: "", alias: "", region: "", residential: false, premium: false, tags: "" });
  React.useEffect(() => {
    if (!node) return;
    setForm({
      name: node.name || "",
      alias: node.alias || "",
      region: node.region || "",
      residential: !!node.residential,
      premium: !!node.premium,
      tags: (node.tags || []).join(", "),
    });
  }, [node]);
  if (!node) return null;

  const set = (k, v) => setForm(f => ({ ...f, [k]: v }));
  const tags = () => form.tags.split(",").map(s => s.trim()).filter(Boolean);
  const metadataTags = () => {
    const set = new Set(tags().map(t => t.toLowerCase()));
    if (form.residential) set.add("residential");
    if (!form.residential) set.delete("residential");
    if (form.premium) set.add("premium");
    if (!form.premium) set.delete("premium");
    return Array.from(set);
  };
  const serverKey = `${node.server || "—"}${node.port ? `:${node.port}` : ""}`;

  const submit = async () => {
    const region = form.region.trim().toUpperCase().slice(0, 2);
    if (node.poolSource === "subscription") {
      const ok = await dispatch({ type:"updateSubscriptionNode", payload:{
        pool: node.pool,
        node: node.name,
        server: node.server || "",
        port: node.port || 0,
        region,
        alias: form.alias.trim(),
        tags: metadataTags(),
        residential: !!form.residential,
        premium: !!form.premium,
      }});
      if (ok) onSaved({ ...node, region, alias: form.alias.trim(), tags: metadataTags(), residential: !!form.residential, premium: !!form.premium });
      return;
    }

    const nextName = form.name.trim();
    if (!nextName) {
      toast("Node name is required");
      return;
    }
    const updatedNode = {
      ...node,
      name: nextName,
      region,
      residential: !!form.residential,
      premium: !!form.premium,
      tags: metadataTags(),
      enabled: node.enabled !== false,
    };
    const ok = await dispatch({
      type: "updateStaticNode",
      pool: node.pool,
      node: node.name,
      updatedNode,
    });
    if (ok) onSaved({ ...node, ...updatedNode, id: `${node.pool}:${nextName}` });
  };

  return (
    <Modal open={!!node} onClose={onClose} title={`Edit metadata`}
      footer={<>
        <button className="btn" onClick={onClose}>Cancel</button>
        <button className="btn primary" onClick={submit}><Ic.check/> Save</button>
      </>}>
      <div className="col gap-12">
        <div className="kv" style={{rowGap:10}}>
          <div className="k">Pool</div><div className="v mono">{node.pool}</div>
          <div className="k">Source</div><div className="v mono">{node.poolSource}</div>
          <div className="k">Server key</div><div className="v mono">{serverKey}</div>
        </div>
        {node.poolSource === "static" ? (
          <div className="field">
            <label className="field-label">Node name</label>
            <input className="input mono" value={form.name} onChange={e => set("name", e.target.value)} />
          </div>
        ) : (
          <div className="field">
            <label className="field-label">Alias</label>
            <input className="input mono" value={form.alias} onChange={e => set("alias", e.target.value)} placeholder="hk-streaming-primary" />
          </div>
        )}
        <div className="field-row">
          <div className="field">
            <label className="field-label">Region</label>
            <input className="input mono" value={form.region} onChange={e => set("region", e.target.value.toUpperCase().slice(0, 2))} maxLength={2} />
          </div>
          <div className="field">
            <label className="field-label">Class</label>
            <Toggle on={form.residential} onChange={v => set("residential", v)} label={form.residential ? "Residential" : "Normal"} />
          </div>
        </div>
        <div className="field">
          <label className="field-label">Premium</label>
          <Toggle on={form.premium} onChange={v => set("premium", v)} label={form.premium ? "Premium node" : "Standard node"} />
        </div>
        <div className="field">
          <label className="field-label">Tags</label>
          <input className="input mono" value={form.tags} onChange={e => set("tags", e.target.value)} placeholder="streaming, residential, premium" />
          {node.poolSource === "subscription" && <div className="field-hint">Subscription metadata is saved by node name and server key.</div>}
        </div>
      </div>
    </Modal>
  );
}

window.PageEgress = PageEgress;
