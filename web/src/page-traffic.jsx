// Traffic — request log + drawer
const { Drawer, Seg, RegionPill, StatusCode, RequestDetailContent, fmtBytes, fmtAgo, BarChart, Sparkline } = window.UI;

function PageTraffic({ state }) {
  const { traffic } = state;
  const [filter, setFilter] = React.useState("all");
  const [search, setSearch] = React.useState("");
  const [open, setOpen] = React.useState(null);

  const rows = traffic.requests.filter(r => {
    if (filter === "errors") { if (!(r.status === 0 || r.status >= 400)) return false; }
    if (filter === "tunnel" && r.type !== "tunnel") return false;
    if (filter === "template" && !r.template) return false;
    if (search) {
      const q = search.toLowerCase();
      if (!r.url.toLowerCase().includes(q) && !r.pool.toLowerCase().includes(q) && !r.region.toLowerCase().includes(q)) return false;
    }
    return true;
  });

  const total = traffic.requests.length;
  const errs = traffic.requests.filter(r => r.status === 0 || r.status >= 400).length;
  const tunnels = traffic.requests.filter(r => r.type === "tunnel").length;
  const exportCSV = () => {
    const header = ["time","type","method","url","region","pool","node","status","duration_ms","bytes","error"];
    const lines = rows.map(r => [
      new Date(r.ts).toISOString(), r.type, r.method, r.url, r.group, r.pool, r.node,
      r.status, r.duration_ms, r.bytes, r.error || "",
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
          <p>Per-request log of HTTP and tunnel egress. Click any row to inspect routing, TLS profile and replay payload.</p>
        </div>
        <div className="page-h-actions">
          <button className="btn" onClick={exportCSV}><Ic.download/> Export CSV</button>
        </div>
      </div>

      <div className="stat-row">
        <div className="stat">
          <div className="stat-label">Requests</div>
          <div className="stat-value">{total}</div>
          <div style={{marginTop:14}}><BarChart series={traffic.series} width={240} height={36}/></div>
        </div>
        <div className="stat">
          <div className="stat-label">Errors</div>
          <div className="stat-value">{errs}<span className="unit">{total ? ` · ${(errs/total*100).toFixed(1)}%` : ""}</span></div>
          <div className="stat-delta">{errs > 0 ? "investigating" : "clean window"}</div>
        </div>
        <div className="stat">
          <div className="stat-label">Active tunnels</div>
          <div className="stat-value">{tunnels}</div>
          <div style={{marginTop:14}}><Sparkline data={traffic.series.map(s => s.success)} width={240} height={36}/></div>
        </div>
        <div className="stat">
          <div className="stat-label">Avg ms</div>
          <div className="stat-value">{Math.round(traffic.requests.reduce((a,b)=>a+b.duration_ms,0)/total)||0}</div>
          <div className="stat-delta">last {total} requests</div>
        </div>
      </div>

      <div className="card card-pad-0">
        <div className="card-h bordered">
          <h3>Request log</h3>
          <span className="sub">{rows.length} of {total}</span>
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
              <th>Status</th><th className="num">ms</th><th className="num">bytes</th>
            </tr></thead>
            <tbody>
              {rows.map(r => (
                <tr key={r.id} onClick={() => setOpen(r)} style={{cursor:"pointer"}}>
                  <td className="mono muted-2">{fmtAgo(r.ts)}</td>
                  <td><span className="muted-2 mono" style={{fontSize:11}}>{r.type === "tunnel" ? "WS" : "HTTP"}</span></td>
                  <td className="mono" style={{color:"var(--fg-1)"}}>{r.method}</td>
                  <td className="mono truncate" style={{maxWidth:240, fontSize:11.5}}>{r.url}</td>
                  <td><RegionPill code={r.group} residential={r.residential}/></td>
                  <td className="mono" style={{fontSize:11.5}}>
                    <div>{r.pool}{r.template && <span className="muted-2" style={{marginLeft:6, fontSize:10}}>tpl</span>}</div>
                    <div className="muted-2 truncate" style={{maxWidth:180, fontSize:10.5, marginTop:2}}>{r.node}</div>
                  </td>
                  <td className="mono muted-2" style={{fontSize:11.5}}>{r.tls || "—"}</td>
                  <td><StatusCode code={r.status}/></td>
                  <td className="num mono">{r.duration_ms}</td>
                  <td className="num mono muted-2">{fmtBytes(r.bytes)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      <Drawer open={!!open} onClose={() => setOpen(null)} title="Request detail">
        <RequestDetailContent request={open}/>
      </Drawer>
    </div>
  );
}

window.PageTraffic = PageTraffic;
