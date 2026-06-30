// Traffic — request log + drawer
const { Drawer, Seg, RegionPill, StatusCode, RequestDetailContent, fmtBytes, fmtAgo, BarChart, Sparkline } = window.UI;

function PageTraffic({ state, dispatch, busy }) {
  const { traffic } = state;
  const [filter, setFilter] = React.useState("all");
  const [search, setSearch] = React.useState("");
  const [open, setOpen] = React.useState(null);
  const [expanded, setExpanded] = React.useState({});

  const metrics = traffic.metrics || {};
  const effectiveTotal = Number(metrics.requests ?? traffic.requests.length);
  const rawLoaded = Number(traffic.rawLoaded || traffic.requests.length);
  const rawTotal = Number(traffic.rawTotal || rawLoaded);
  const errorTotal = Number(metrics.failures ?? traffic.requests.filter(isTrafficError).length);
  const rawFailures = Number(metrics.raw_failures || errorTotal);
  const avgLatency = Number(metrics.avg_latency_ms || 0) || averageSuccessfulLatency(traffic.requests);

  const rowMatchesSearch = (r) => {
    if (!search) return true;
    const q = search.toLowerCase();
    const haystack = [r.url, r.pool, r.region, r.group, r.node, r.error]
      .concat((r.children || []).flatMap(c => [c.url, c.pool, c.region, c.group, c.node, c.error]))
      .join(" ")
      .toLowerCase();
    return haystack.includes(q);
  };

  const rows = traffic.requests.filter(r => {
    if (filter === "errors" && !isTrafficError(r)) return false;
    if (filter === "tunnel" && r.type !== "tunnel") return false;
    if (filter === "template" && !r.template) return false;
    return rowMatchesSearch(r);
  });

  const canLoadMore = rawLoaded > 0 && rawLoaded < Math.min(rawTotal || 1000, 1000) && rawLoaded % 200 === 0;
  const tunnels = traffic.requests.filter(r => r.type === "tunnel").length;
  const toggleExpanded = (id) => setExpanded(s => ({ ...s, [id]: !s[id] }));
  const exportCSV = () => {
    const header = ["time","type","method","url","region","pool","node","status","duration_ms","bytes","count","error"];
    const lines = rows.map(r => [
      new Date(r.ts).toISOString(), r.type, r.method, r.url, r.group, r.pool, r.node,
      r.status, r.duration_ms, r.bytes, r.group_count || 1, r.error || "",
    ].map(v => `"${String(v).replaceAll('"', '""')}"`).join(","));
    const blob = new Blob([[header.join(","), ...lines].join("\n")], { type: "text/csv" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "chijie-traffic.csv";
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>Traffic</h1>
          <p>Effective request log for HTTP and tunnel egress. Repeated failures with the same URL and region are merged into one row.</p>
        </div>
        <div className="page-h-actions">
          <button className="btn" onClick={exportCSV}><Ic.download/> Export CSV</button>
        </div>
      </div>

      <div className="stat-row">
        <div className="stat">
          <div className="stat-label">Effective requests</div>
          <div className="stat-value">{effectiveTotal}</div>
          <div className="stat-delta">{rawTotal !== effectiveTotal ? `${rawTotal} raw traces` : "no merged retries"}</div>
          <div style={{marginTop:14}}><BarChart series={traffic.series} width={240} height={36}/></div>
        </div>
        <div className="stat">
          <div className="stat-label">Effective errors</div>
          <div className="stat-value">{errorTotal}<span className="unit">{effectiveTotal ? ` · ${(errorTotal/effectiveTotal*100).toFixed(1)}%` : ""}</span></div>
          <div className="stat-delta">{rawFailures !== errorTotal ? `${rawFailures} raw failures` : (errorTotal > 0 ? "error window" : "clean window")}</div>
        </div>
        <div className="stat">
          <div className="stat-label">Tunnel rows</div>
          <div className="stat-value">{tunnels}</div>
          <div style={{marginTop:14}}><Sparkline data={traffic.series.map(s => s.success)} width={240} height={36}/></div>
        </div>
        <div className="stat">
          <div className="stat-label">Avg ms</div>
          <div className="stat-value">{avgLatency}</div>
          <div className="stat-delta">successful requests only</div>
        </div>
      </div>

      <div className="card card-pad-0">
        <div className="card-h bordered">
          <h3>Request log</h3>
          <span className="sub">{rows.length} of {traffic.requests.length} rows · {rawLoaded} raw loaded</span>
          <div className="right">
            <Seg value={filter} onChange={setFilter} options={[
              {value:"all",label:"All"},{value:"errors",label:"Errors"},{value:"tunnel",label:"Tunnel"},{value:"template",label:"Template"}
            ]}/>
            <div style={{position:"relative"}}>
              <Ic.search style={{position:"absolute",left:10,top:9,width:13,height:13,color:"var(--fg-3)"}}/>
              <input className="input" placeholder="url / pool / region…" value={search} onChange={e => setSearch(e.target.value)} style={{paddingLeft:30, width:220}}/>
            </div>
          </div>
        </div>
        <div style={{maxHeight: 560, overflow:"auto"}}>
          <table className="table">
            <thead><tr>
              <th>Time</th><th>Type</th><th>Method</th><th>URL</th>
              <th>Region</th><th>Pool</th><th>TLS</th>
              <th>Status</th><th>Count</th><th className="num">ms</th><th className="num">bytes</th>
            </tr></thead>
            <tbody>
              {rows.map(r => (
                <React.Fragment key={r.id}>
                  <TrafficRow row={r} openDetail={setOpen} expanded={!!expanded[r.id]} toggleExpanded={toggleExpanded}/>
                  {!!expanded[r.id] && (r.children || []).map(child => (
                    <TrafficRow key={`${r.id}-${child.id}`} row={child} openDetail={setOpen} child/>
                  ))}
                </React.Fragment>
              ))}
            </tbody>
          </table>
        </div>
        <div className="row" style={{justifyContent:"center", padding:"16px", borderTop:"1px solid var(--line-1)"}}>
          <button className="btn" disabled={busy || !canLoadMore} onClick={() => dispatch?.({type:"loadMoreTraffic"})}>
            <Ic.plus/> {rawLoaded >= 1000 ? "Loaded 1000" : canLoadMore ? "Load more" : "All loaded"}
          </button>
          <span className="muted-2 mono" style={{fontSize:11}}>memory window max 1000</span>
        </div>
      </div>

      <Drawer open={!!open} onClose={() => setOpen(null)} title="Request detail">
        <RequestDetailContent request={open}/>
      </Drawer>
    </div>
  );
}

function isTrafficError(row) {
  return row.status === 0 || row.status >= 400 || !!row.error;
}

function isTrafficSuccess(row) {
  return row.status > 0 && row.status < 400 && !row.error;
}

function averageSuccessfulLatency(rows) {
  const values = rows.filter(isTrafficSuccess).map(r => r.duration_ms).filter(Boolean);
  if (!values.length) return 0;
  return Math.round(values.reduce((sum, value) => sum + value, 0) / values.length);
}

function TrafficRow({ row, openDetail, expanded, toggleExpanded, child }) {
  const count = row.group_count || 1;
  const canExpand = !child && count > 1;
  const rowStyle = {
    cursor: "pointer",
    background: child ? "rgba(255,255,255,0.018)" : undefined,
  };
  return (
    <tr onClick={() => openDetail(row)} style={rowStyle}>
      <td className="mono muted-2" style={{paddingLeft: child ? 28 : undefined}}>{child ? "↳ " : ""}{fmtAgo(row.ts)}</td>
      <td><span className="muted-2 mono" style={{fontSize:11}}>{row.type === "tunnel" ? "WS" : "HTTP"}</span></td>
      <td className="mono" style={{color:"var(--fg-1)"}}>{row.method}</td>
      <td className="mono truncate" style={{maxWidth:240, fontSize:11.5}}>
        {row.url}
        {canExpand && <span className="muted-2" style={{marginLeft:8, fontSize:10}}>merged failures</span>}
      </td>
      <td><RegionPill code={row.group} residential={row.residential}/></td>
      <td className="mono" style={{fontSize:11.5}}>
        <div>{row.pool}{row.template && <span className="muted-2" style={{marginLeft:6, fontSize:10}}>tpl</span>}</div>
        <div className="muted-2 truncate" style={{maxWidth:180, fontSize:10.5, marginTop:2}}>{row.node}</div>
      </td>
      <td className="mono muted-2" style={{fontSize:11.5}}>{row.tls || "—"}</td>
      <td><StatusCode code={row.status}/></td>
      <td>
        {canExpand
          ? <button className="btn sm mono" onClick={(e) => { e.stopPropagation(); toggleExpanded(row.id); }} style={{height:24, minWidth:48, justifyContent:"center"}}>
              {`x${count}`}
            </button>
          : <span className="mono muted-2">{child ? "raw" : "1"}</span>}
      </td>
      <td className="num mono">{row.duration_ms}</td>
      <td className="num mono muted-2">{fmtBytes(row.bytes)}</td>
    </tr>
  );
}

window.PageTraffic = PageTraffic;
