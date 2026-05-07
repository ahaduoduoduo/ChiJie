package traffic

import (
	"testing"
	"time"
)

func TestStoreRecordsMetricsAndRecentTraces(t *testing.T) {
	store := NewStore(2)

	store.Record(Trace{
		Timestamp:     time.Now().Add(-time.Minute),
		Kind:          "proxy",
		Method:        "GET",
		URL:           "https://example.com/a",
		Target:        "example.com/a",
		EgressGroup:   "DIRECT",
		Status:        200,
		LatencyMS:     40,
		RequestBytes:  100,
		ResponseBytes: 900,
	})
	store.Record(Trace{
		Kind:          "proxy",
		Method:        "POST",
		URL:           "https://example.com/b",
		Target:        "example.com/b",
		EgressGroup:   "HK",
		Status:        502,
		LatencyMS:     120,
		RequestBytes:  200,
		ResponseBytes: 20,
		Error:         "bad gateway",
	})

	store.TunnelOpened()
	snapshot := store.Snapshot(10)

	if snapshot.Metrics.Requests != 2 {
		t.Fatalf("requests: got %d want 2", snapshot.Metrics.Requests)
	}
	if snapshot.Metrics.Success != 1 || snapshot.Metrics.Failures != 1 {
		t.Fatalf("success/failures: got %d/%d want 1/1", snapshot.Metrics.Success, snapshot.Metrics.Failures)
	}
	if snapshot.Metrics.ActiveTunnels != 1 {
		t.Fatalf("active tunnels: got %d want 1", snapshot.Metrics.ActiveTunnels)
	}
	if len(snapshot.Traces) != 2 {
		t.Fatalf("trace count: got %d want 2", len(snapshot.Traces))
	}
	if snapshot.Traces[0].Status != 502 {
		t.Fatalf("traces should be newest first")
	}
	if len(snapshot.Series) == 0 {
		t.Fatalf("expected time series buckets")
	}

	store.TunnelClosed()
	if got := store.Metrics().ActiveTunnels; got != 0 {
		t.Fatalf("active tunnels after close: got %d want 0", got)
	}
}
