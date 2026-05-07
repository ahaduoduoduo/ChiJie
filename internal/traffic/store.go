package traffic

import (
	"sort"
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
	RequestBytes   int64   `json:"request_bytes"`
	ResponseBytes  int64   `json:"response_bytes"`
	ActiveTunnels  int64   `json:"active_tunnels"`
	RequestsPerMin float64 `json:"requests_per_min"`
	SuccessRate    float64 `json:"success_rate"`
	P95LatencyMS   int64   `json:"p95_latency_ms"`
}

// Bucket is one minute of traffic history.
type Bucket struct {
	Time          time.Time `json:"time"`
	Requests      int64     `json:"requests"`
	Failures      int64     `json:"failures"`
	RequestBytes  int64     `json:"request_bytes"`
	ResponseBytes int64     `json:"response_bytes"`
}

type Snapshot struct {
	Metrics Metrics  `json:"metrics"`
	Traces  []Trace  `json:"traces"`
	Series  []Bucket `json:"series"`
}

type Store struct {
	mu            sync.RWMutex
	capacity      int
	nextID        int64
	traces        []Trace
	buckets       map[int64]*Bucket
	startedAt     time.Time
	activeTunnels int64
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Store{
		capacity:  capacity,
		buckets:   make(map[int64]*Bucket),
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

	key := trace.Timestamp.Truncate(time.Minute).Unix()
	bucket := s.buckets[key]
	if bucket == nil {
		bucket = &Bucket{Time: trace.Timestamp.Truncate(time.Minute)}
		s.buckets[key] = bucket
	}
	bucket.Requests++
	if trace.Status >= 400 || trace.Error != "" {
		bucket.Failures++
	}
	bucket.RequestBytes += trace.RequestBytes
	bucket.ResponseBytes += trace.ResponseBytes

	cutoff := time.Now().Add(-24 * time.Hour).Truncate(time.Minute).Unix()
	for bucketKey := range s.buckets {
		if bucketKey < cutoff {
			delete(s.buckets, bucketKey)
		}
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

	series := make([]Bucket, 0, len(s.buckets))
	for _, bucket := range s.buckets {
		series = append(series, *bucket)
	}
	sort.Slice(series, func(i, j int) bool {
		return series[i].Time.Before(series[j].Time)
	})

	return Snapshot{
		Metrics: s.metricsLocked(),
		Traces:  traces,
		Series:  series,
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
	for _, trace := range s.traces {
		metrics.Requests++
		if trace.Status >= 400 || trace.Error != "" {
			metrics.Failures++
		} else {
			metrics.Success++
		}
		metrics.RequestBytes += trace.RequestBytes
		metrics.ResponseBytes += trace.ResponseBytes
		if trace.LatencyMS > 0 {
			latencies = append(latencies, trace.LatencyMS)
		}
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
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		index := int(float64(len(latencies))*0.95) - 1
		if index < 0 {
			index = 0
		}
		if index >= len(latencies) {
			index = len(latencies) - 1
		}
		metrics.P95LatencyMS = latencies[index]
	}

	return metrics
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
