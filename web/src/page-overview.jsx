// Overview — minimal, focused on what matters at a glance
const { Drawer, RequestDetailContent, Sparkline, BarChart, fmtAgo, StatusCode, RegionPill } = window.UI;

function isTrafficSuccess(row) {
  return row.status > 0 && row.status < 400 && !row.error;
}

function isTrafficError(row) {
  return row.status === 0 || row.status >= 400 || !!row.error;
}

function RecentTrafficCard({ title, rows, empty, detail, onSelect }) {
  return (
    <div className="card card-pad-0" style={{marginBottom: 20}}>
      <div className="card-h bordered"><h3>{title}</h3><span className="sub">{rows.length} in window</span></div>
      {rows.length === 0
        ? <div className="empty">{empty}</div>
        : <table className="table">
            <thead><tr><th>Time</th><th>Region</th><th>Pool</th><th>Status</th><th>Detail</th></tr></thead>
            <tbody>{rows.map(r => (
              <tr key={r.id} onClick={() => onSelect?.(r)} style={{cursor: onSelect ? "pointer" : "default"}}>
                <td className="mono muted-2">{fmtAgo(r.ts)} ago</td>
                <td><RegionPill code={r.group} residential={r.residential}/></td>
                <td className="mono">{r.pool}</td>
                <td><StatusCode code={r.status}/></td>
                <td className="muted truncate" style={{maxWidth:360}}>{detail(r)}</td>
              </tr>
            ))}</tbody>
          </table>}
    </div>
  );
}

function PageOverview({ state }) {
  const { pools, traffic, regionGroups, stats } = state;
  const [open, setOpen] = React.useState(null);
  const totalNodes = pools.flatMap(p => p.nodes || []).length;
  const onlineNodes = pools.flatMap(p => p.nodes || []).filter(n => n.alive && n.enabled).length;
  const recent = traffic.requests;
  const ok = recent.filter(isTrafficSuccess).length;
  const successRate = traffic.metrics?.requests ? (traffic.metrics.success_rate * 100).toFixed(1) : (recent.length ? (ok / recent.length * 100).toFixed(1) : "100.0");
  const p95 = traffic.metrics?.p95_latency_ms || Math.round(traffic.series.slice(-10).reduce((a,b) => a + b.p95, 0) / 10);
  const successes = recent.filter(isTrafficSuccess).slice(0, 5);
  const errs = recent.filter(isTrafficError).slice(0, 5);
  const uptime = stats?.uptime ? stats.uptime.replace(/(\.\d+)?s$/, "s") : "—";

  return (
    <div className="page">
      <div className="page-h">
        <div>
          <h1>Overview</h1>
          <p>Real-time signals from the egress fleet.</p>
        </div>
      </div>

      <div className="stat-row">
        <div className="stat">
          <div className="stat-label">Uptime</div>
          <div className="stat-value" style={{fontSize:22}}>{uptime}</div>
          <div className="stat-delta">since last reload</div>
        </div>
        <div className="stat">
          <div className="stat-label">Online nodes</div>
          <div className="stat-value">{onlineNodes}<span className="unit">/ {totalNodes}</span></div>
          <div className="stat-delta">across {pools.length} pools</div>
        </div>
        <div className="stat">
          <div className="stat-label">Success rate</div>
          <div className="stat-value">{successRate}<span className="unit">%</span></div>
          <div className="stat-delta">last 60 minutes</div>
        </div>
        <div className="stat">
          <div className="stat-label">P95 latency</div>
          <div className="stat-value">{p95}<span className="unit">ms</span></div>
          <div className="stat-delta">all egress</div>
        </div>
      </div>

      <div className="grid-2" style={{marginBottom: 20}}>
        <div className="card">
          <div className="card-h"><h3>Requests</h3><span className="sub">last 60m</span></div>
          <div className="card-body" style={{paddingTop:0}}>
            <BarChart series={traffic.series} width={500} height={120}/>
          </div>
        </div>
        <div className="card">
          <div className="card-h"><h3>P95 latency</h3><span className="sub">last 60m</span></div>
          <div className="card-body" style={{paddingTop:0}}>
            <Sparkline data={traffic.series.map(s => s.p95)} width={500} height={120}/>
          </div>
        </div>
      </div>

      <div className="card" style={{marginBottom: 20}}>
        <div className="card-h bordered"><h3>Region groups</h3><span className="sub">{regionGroups.length} active</span></div>
        <div className="card-body" style={{display:"grid",gridTemplateColumns:"repeat(auto-fit, minmax(180px, 1fr))",gap:1, padding: 0, background: "var(--line)"}}>
          {regionGroups.map(g => (
            <div key={g.code} style={{padding:"18px 20px", background:"var(--bg-1)"}}>
              <RegionPill code={g.code} residential={g.residential}/>
              <div style={{marginTop:14, fontSize:24, fontWeight:400, letterSpacing:"-0.02em", fontFamily:"'JetBrains Mono', monospace"}}>
                {g.online}<span style={{color:"var(--fg-3)", fontSize:13}}>/{g.count}</span>
              </div>
              <div style={{marginTop:6, color:"var(--fg-3)", fontSize:11.5}} className="mono">
                {g.minLatency ? `${g.minLatency}ms · ` : ""}{g.templateBackup ? "tpl backup" : "no fallback"}
              </div>
            </div>
          ))}
        </div>
      </div>

      <RecentTrafficCard
        title="Recent success"
        rows={successes}
        empty="No successful requests in the current window."
        detail={r => r.url || r.node || "—"}
        onSelect={setOpen}
      />

      <RecentTrafficCard
        title="Recent errors"
        rows={errs}
        empty="No errors. Egress is clean."
        detail={r => r.error || r.url || r.node || "—"}
        onSelect={setOpen}
      />

      <Drawer open={!!open} onClose={() => setOpen(null)} title="Request detail">
        <RequestDetailContent request={open}/>
      </Drawer>
    </div>
  );
}
window.PageOverview = PageOverview;
