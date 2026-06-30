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

func TestStoreCoalescesRepeatedFailuresAndUsesSuccessfulLatency(t *testing.T) {
	store := NewStore(10)
	base := time.Now().Add(-2 * time.Minute).Truncate(time.Second)

	store.Record(Trace{
		Timestamp:    base,
		Kind:         "proxy",
		Method:       "GET",
		URL:          "https://example.com/ok",
		Target:       "example.com/ok",
		EgressGroup:  "US",
		Status:       200,
		LatencyMS:    80,
		RequestBytes: 20,
	})
	store.Record(Trace{
		Timestamp:    base.Add(time.Second),
		Kind:         "proxy",
		Method:       "GET",
		URL:          "https://example.com/down",
		Target:       "example.com/down",
		EgressGroup:  "US",
		EgressNode:   "us-1",
		Status:       502,
		LatencyMS:    30000,
		RequestBytes: 30,
		Error:        "bad gateway",
	})
	store.Record(Trace{
		Timestamp:    base.Add(2 * time.Second),
		Kind:         "proxy",
		Method:       "GET",
		URL:          "https://example.com/down",
		Target:       "example.com/down",
		EgressGroup:  "US",
		EgressNode:   "us-2",
		Status:       0,
		LatencyMS:    30000,
		RequestBytes: 40,
		Error:        "timeout",
	})
	store.Record(Trace{
		Timestamp:    base.Add(3 * time.Second),
		Kind:         "proxy",
		Method:       "GET",
		URL:          "https://example.com/down",
		Target:       "example.com/down",
		EgressGroup:  "HK",
		EgressNode:   "hk-1",
		Status:       502,
		LatencyMS:    30000,
		RequestBytes: 50,
		Error:        "bad gateway",
	})

	snapshot := store.Snapshot(10)
	if snapshot.Metrics.RawRequests != 4 || snapshot.Metrics.RawFailures != 3 {
		t.Fatalf("raw metrics: got requests=%d failures=%d want 4/3", snapshot.Metrics.RawRequests, snapshot.Metrics.RawFailures)
	}
	if snapshot.Metrics.Requests != 3 || snapshot.Metrics.Success != 1 || snapshot.Metrics.Failures != 2 {
		t.Fatalf("effective metrics: got requests=%d success=%d failures=%d want 3/1/2", snapshot.Metrics.Requests, snapshot.Metrics.Success, snapshot.Metrics.Failures)
	}
	if snapshot.Metrics.AvgLatencyMS != 80 || snapshot.Metrics.P95LatencyMS != 80 {
		t.Fatalf("latency metrics should use successful requests only: avg=%d p95=%d", snapshot.Metrics.AvgLatencyMS, snapshot.Metrics.P95LatencyMS)
	}
	if snapshot.Metrics.SuccessRate != float64(1)/float64(3) {
		t.Fatalf("success rate: got %f want %f", snapshot.Metrics.SuccessRate, float64(1)/float64(3))
	}

	if len(snapshot.Traces) != 4 {
		t.Fatalf("raw traces should be preserved: got %d want 4", len(snapshot.Traces))
	}
	if len(snapshot.DisplayTraces) != 3 {
		t.Fatalf("display traces should merge repeated failures: got %d want 3", len(snapshot.DisplayTraces))
	}
	var usGroup *DisplayTrace
	for i := range snapshot.DisplayTraces {
		if snapshot.DisplayTraces[i].URL == "https://example.com/down" && snapshot.DisplayTraces[i].EgressGroup == "US" {
			usGroup = &snapshot.DisplayTraces[i]
			break
		}
	}
	if usGroup == nil {
		t.Fatalf("expected merged US failure group")
	}
	if usGroup.GroupCount != 2 || len(usGroup.Children) != 2 {
		t.Fatalf("merged group: count=%d children=%d want 2/2", usGroup.GroupCount, len(usGroup.Children))
	}
}
