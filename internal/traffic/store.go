package traffic

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Trace is one completed proxy or tunnel request.
type Trace struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Kind           string    `json:"kind"`
	Method         string    `json:"method"`
	URL            string    `json:"url"`
	Target         string    `json:"target"`
	Region         string    `json:"region,omitempty"`
	EgressGroup    string    `json:"egress_group,omitempty"`
	Strategy       string    `json:"strategy,omitempty"`
	Residential    bool      `json:"residential,omitempty"`
	EgressPool     string    `json:"egress_pool,omitempty"`
	EgressNode     string    `json:"egress_node,omitempty"`
	EgressSource   string    `json:"egress_source,omitempty"`
	EgressTemplate bool      `json:"egress_template,omitempty"`
	TLSFingerprint string    `json:"tls_fingerprint,omitempty"`
	Status         int       `json:"status"`
	LatencyMS      int64     `json:"latency_ms"`
	RequestBytes   int64     `json:"request_bytes"`
	ResponseBytes  int64     `json:"response_bytes"`
	Error          string    `json:"error,omitempty"`
}

// Metrics is the aggregate view exposed to the admin UI.
type Metrics struct {
	Requests       int64   `json:"requests"`
	Success        int64   `json:"success"`
	Failures       int64   `json:"failures"`
	RawRequests    int64   `json:"raw_requests"`
	RawFailures    int64   `json:"raw_failures"`
	RequestBytes   int64   `json:"request_bytes"`
	ResponseBytes  int64   `json:"response_bytes"`
	ActiveTunnels  int64   `json:"active_tunnels"`
	RequestsPerMin float64 `json:"requests_per_min"`
	SuccessRate    float64 `json:"success_rate"`
	AvgLatencyMS   int64   `json:"avg_latency_ms"`
	P95LatencyMS   int64   `json:"p95_latency_ms"`
}

// Bucket is one minute of traffic history.
type Bucket struct {
	Time         time.Time `json:"time"`
	Requests     int64     `json:"requests"`
	Failures     int64     `json:"failures"`
	AvgLatencyMS int64     `json:"avg_latency_ms"`
	P95LatencyMS int64     `json:"p95_latency_ms"`
}

type DisplayTrace struct {
	Trace
	GroupKey   string  `json:"group_key,omitempty"`
	GroupCount int     `json:"group_count,omitempty"`
	Children   []Trace `json:"children,omitempty"`
}

type Snapshot struct {
	Metrics       Metrics        `json:"metrics"`
	Traces        []Trace        `json:"traces"`
	DisplayTraces []DisplayTrace `json:"display_traces"`
	Series        []Bucket       `json:"series"`
}

type Store struct {
	mu            sync.RWMutex
	capacity      int
	nextID        int64
	traces        []Trace
	startedAt     time.Time
	activeTunnels int64
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Store{
		capacity:  capacity,
		startedAt: time.Now(),
	}
}

func (s *Store) Record(trace Trace) Trace {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	trace.ID = "trace-" + formatID(s.nextID)
	if trace.Timestamp.IsZero() {
		trace.Timestamp = time.Now()
	}
	s.traces = append(s.traces, trace)
	if len(s.traces) > s.capacity {
		copy(s.traces, s.traces[len(s.traces)-s.capacity:])
		s.traces = s.traces[:s.capacity]
	}

	return trace
}

func (s *Store) TunnelOpened() {
	s.mu.Lock()
	s.activeTunnels++
	s.mu.Unlock()
}

func (s *Store) TunnelClosed() {
	s.mu.Lock()
	if s.activeTunnels > 0 {
		s.activeTunnels--
	}
	s.mu.Unlock()
}

func (s *Store) Snapshot(limit int) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.traces) {
		limit = len(s.traces)
	}

	traces := make([]Trace, 0, limit)
	for i := len(s.traces) - 1; i >= 0 && len(traces) < limit; i-- {
		traces = append(traces, s.traces[i])
	}

	return Snapshot{
		Metrics:       s.metricsLocked(),
		Traces:        traces,
		DisplayTraces: displayTraces(traces),
		Series:        buildSeries(s.traces, time.Now()),
	}
}

func (s *Store) Metrics() Metrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metricsLocked()
}

func (s *Store) metricsLocked() Metrics {
	var metrics Metrics
	latencies := make([]int64, 0, len(s.traces))
	seenFailures := make(map[string]struct{})

	for i := len(s.traces) - 1; i >= 0; i-- {
		trace := s.traces[i]
		metrics.RawRequests++
		if isFailure(trace) {
			metrics.RawFailures++
			key := failureGroupKey(trace)
			if key != "" {
				if _, ok := seenFailures[key]; ok {
					metrics.RequestBytes += trace.RequestBytes
					metrics.ResponseBytes += trace.ResponseBytes
					continue
				}
				seenFailures[key] = struct{}{}
			}
			metrics.Requests++
			metrics.Failures++
		} else {
			metrics.Requests++
			metrics.Success++
			if trace.LatencyMS > 0 {
				latencies = append(latencies, trace.LatencyMS)
			}
		}
		metrics.RequestBytes += trace.RequestBytes
		metrics.ResponseBytes += trace.ResponseBytes
	}

	metrics.ActiveTunnels = s.activeTunnels
	if metrics.Requests > 0 {
		metrics.SuccessRate = float64(metrics.Success) / float64(metrics.Requests)
	}
	minutes := time.Since(s.startedAt).Minutes()
	if minutes < 1 {
		minutes = 1
	}
	metrics.RequestsPerMin = float64(metrics.Requests) / minutes

	if len(latencies) > 0 {
		var sum int64
		for _, latency := range latencies {
			sum += latency
		}
		metrics.AvgLatencyMS = sum / int64(len(latencies))
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		metrics.P95LatencyMS = percentile95(latencies)
	}

	return metrics
}

func displayTraces(traces []Trace) []DisplayTrace {
	rows := make([]DisplayTrace, 0, len(traces))
	indexByKey := make(map[string]int)
	for _, trace := range traces {
		if !isFailure(trace) {
			rows = append(rows, DisplayTrace{Trace: trace, GroupCount: 1})
			continue
		}
		key := failureGroupKey(trace)
		if key == "" {
			rows = append(rows, DisplayTrace{Trace: trace, GroupCount: 1})
			continue
		}
		if index, ok := indexByKey[key]; ok {
			rows[index].GroupCount++
			rows[index].Children = append(rows[index].Children, trace)
			continue
		}
		indexByKey[key] = len(rows)
		rows = append(rows, DisplayTrace{
			Trace:      trace,
			GroupKey:   key,
			GroupCount: 1,
			Children:   []Trace{trace},
		})
	}
	for i := range rows {
		if rows[i].GroupCount <= 1 {
			rows[i].Children = nil
		}
	}
	return rows
}

func buildSeries(traces []Trace, now time.Time) []Bucket {
	cutoff := now.Add(-24 * time.Hour).Truncate(time.Minute)
	buckets := make(map[int64]*Bucket)
	latencies := make(map[int64][]int64)
	seenFailures := make(map[string]struct{})

	for i := len(traces) - 1; i >= 0; i-- {
		trace := traces[i]
		minute := trace.Timestamp.Truncate(time.Minute)
		if minute.Before(cutoff) {
			continue
		}
		key := minute.Unix()
		bucket := buckets[key]
		if bucket == nil {
			bucket = &Bucket{Time: minute}
			buckets[key] = bucket
		}

		if isFailure(trace) {
			groupKey := failureGroupKey(trace)
			if groupKey != "" {
				if _, ok := seenFailures[groupKey]; ok {
					continue
				}
				seenFailures[groupKey] = struct{}{}
			}
			bucket.Requests++
			bucket.Failures++
			continue
		}

		bucket.Requests++
		if trace.LatencyMS > 0 {
			latencies[key] = append(latencies[key], trace.LatencyMS)
		}
	}

	series := make([]Bucket, 0, len(buckets))
	for key, bucket := range buckets {
		values := latencies[key]
		if len(values) > 0 {
			var sum int64
			for _, latency := range values {
				sum += latency
			}
			bucket.AvgLatencyMS = sum / int64(len(values))
			sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
			bucket.P95LatencyMS = percentile95(values)
		}
		series = append(series, *bucket)
	}
	sort.Slice(series, func(i, j int) bool {
		return series[i].Time.Before(series[j].Time)
	})
	return series
}

func isFailure(trace Trace) bool {
	return trace.Status == 0 || trace.Status >= 400 || trace.Error != ""
}

func failureGroupKey(trace Trace) string {
	target := strings.TrimSpace(trace.URL)
	if target == "" {
		target = strings.TrimSpace(trace.Target)
	}
	if target == "" {
		return ""
	}
	return strings.Join([]string{
		nonEmpty(trace.Kind, "proxy"),
		target,
		targetArea(trace),
	}, "\x00")
}

func targetArea(trace Trace) string {
	if trace.EgressGroup != "" {
		return trace.EgressGroup
	}
	if trace.Region != "" {
		if trace.Residential && !strings.HasSuffix(trace.Region, "-RES") {
			return trace.Region + "-RES"
		}
		return trace.Region
	}
	return "DIRECT"
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func percentile95(sorted []int64) int64 {
	index := int(float64(len(sorted))*0.95) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func formatID(value int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	if value == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = digits[int(value%36)]
		value /= 36
	}
	return string(buf[i:])
}
