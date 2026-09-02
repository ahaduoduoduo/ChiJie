package traffic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultRetentionDays = 7
	MaxRetentionDays     = 3650
	traceFilePrefix      = "traffic-"
	traceFileSuffix      = ".jsonl"
)

type PersistenceConfig struct {
	Enabled       *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	RetentionDays int   `yaml:"retention_days,omitempty" json:"retention_days,omitempty"`
}

type tracePersistence struct {
	directory string
	queue     chan Trace
	retention atomic.Int64
	closeMu   sync.RWMutex
	closed    bool
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func newTracePersistence(directory string, retentionDays, capacity int) (*tracePersistence, []Trace, error) {
	directory = filepath.Clean(directory)
	if strings.TrimSpace(directory) == "" || directory == "." {
		return nil, nil, fmt.Errorf("traffic persistence directory is required")
	}
	if err := validateRetentionDays(retentionDays); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, nil, fmt.Errorf("create traffic persistence directory: %w", err)
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, nil, fmt.Errorf("secure traffic persistence directory: %w", err)
	}

	p := &tracePersistence{
		directory: directory,
		queue:     make(chan Trace, 4096),
	}
	p.retention.Store(int64(retentionDays))
	if err := p.prune(time.Now()); err != nil {
		return nil, nil, err
	}
	traces, err := p.loadRecent(capacity)
	if err != nil {
		return nil, nil, err
	}
	p.wg.Add(1)
	go p.run()
	return p, traces, nil
}

func validateRetentionDays(days int) error {
	if days < 1 || days > MaxRetentionDays {
		return fmt.Errorf("retention_days must be between 1 and %d", MaxRetentionDays)
	}
	return nil
}

func persistenceEnabled(cfg Config) bool {
	if cfg.Persistence.Enabled == nil {
		return true
	}
	return *cfg.Persistence.Enabled
}

func (p *tracePersistence) enqueue(trace Trace) {
	if p == nil {
		return
	}
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()
	if p.closed {
		return
	}
	p.queue <- trace
}

func (p *tracePersistence) updateRetention(days int) error {
	if p == nil {
		return nil
	}
	if err := validateRetentionDays(days); err != nil {
		return err
	}
	p.retention.Store(int64(days))
	return p.prune(time.Now())
}

func (p *tracePersistence) close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		p.closeMu.Lock()
		p.closed = true
		close(p.queue)
		p.closeMu.Unlock()
		p.wg.Wait()
	})
}

func (p *tracePersistence) run() {
	defer p.wg.Done()

	var file *os.File
	var writer *bufio.Writer
	var currentDate string
	flushTicker := time.NewTicker(time.Second)
	defer flushTicker.Stop()
	defer func() {
		if writer != nil {
			_ = writer.Flush()
		}
		if file != nil {
			_ = file.Close()
		}
	}()

	writeTrace := func(trace Trace) {
		date := trace.Timestamp.In(time.Local).Format("2006-01-02")
		if date != currentDate {
			if writer != nil {
				_ = writer.Flush()
			}
			if file != nil {
				_ = file.Close()
			}
			path := filepath.Join(p.directory, traceFilePrefix+date+traceFileSuffix)
			var err error
			file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				log.Printf("traffic persistence: open %s: %v", filepath.Base(path), err)
				file = nil
				writer = nil
				currentDate = ""
				return
			}
			if err := file.Chmod(0600); err != nil {
				log.Printf("traffic persistence: secure %s: %v", filepath.Base(path), err)
				_ = file.Close()
				file = nil
				writer = nil
				currentDate = ""
				return
			}
			writer = bufio.NewWriterSize(file, 64*1024)
			currentDate = date
			if err := p.prune(trace.Timestamp); err != nil {
				log.Printf("traffic persistence: prune: %v", err)
			}
		}
		if writer == nil {
			return
		}
		data, err := json.Marshal(trace)
		if err != nil {
			log.Printf("traffic persistence: marshal trace: %v", err)
			return
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			log.Printf("traffic persistence: write trace: %v", err)
		}
	}

	for {
		select {
		case trace, ok := <-p.queue:
			if !ok {
				return
			}
			writeTrace(trace)
		case <-flushTicker.C:
			if writer != nil {
				if err := writer.Flush(); err != nil {
					log.Printf("traffic persistence: flush: %v", err)
				}
			}
		}
	}
}

func (p *tracePersistence) loadRecent(capacity int) ([]Trace, error) {
	entries, err := os.ReadDir(p.directory)
	if err != nil {
		return nil, fmt.Errorf("read traffic persistence directory: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !validTraceFileName(entry.Name()) {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	traces := make([]Trace, 0, capacity)
	for _, name := range files {
		path := filepath.Join(p.directory, name)
		file, err := os.Open(path)
		if err != nil {
			log.Printf("traffic persistence: read %s: %v", name, err)
			continue
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			var trace Trace
			if err := json.Unmarshal(scanner.Bytes(), &trace); err != nil {
				log.Printf("traffic persistence: skip invalid record in %s: %v", name, err)
				continue
			}
			if trace.Timestamp.IsZero() {
				continue
			}
			traces = append(traces, trace)
			if len(traces) > capacity {
				copy(traces, traces[len(traces)-capacity:])
				traces = traces[:capacity]
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("traffic persistence: scan %s: %v", name, err)
		}
		_ = file.Close()
	}
	sort.SliceStable(traces, func(i, j int) bool {
		return traces[i].Timestamp.Before(traces[j].Timestamp)
	})
	return traces, nil
}

func (p *tracePersistence) prune(now time.Time) error {
	entries, err := os.ReadDir(p.directory)
	if err != nil {
		return fmt.Errorf("read traffic persistence directory: %w", err)
	}
	days := int(p.retention.Load())
	cutoff := startOfLocalDay(now).AddDate(0, 0, -(days - 1))
	for _, entry := range entries {
		if entry.IsDir() || !validTraceFileName(entry.Name()) {
			continue
		}
		date, err := traceFileDate(entry.Name())
		if err != nil || !date.Before(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(p.directory, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove expired traffic log %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func validTraceFileName(name string) bool {
	_, err := traceFileDate(name)
	return err == nil
}

func traceFileDate(name string) (time.Time, error) {
	if !strings.HasPrefix(name, traceFilePrefix) || !strings.HasSuffix(name, traceFileSuffix) {
		return time.Time{}, fmt.Errorf("not a traffic log file")
	}
	value := strings.TrimSuffix(strings.TrimPrefix(name, traceFilePrefix), traceFileSuffix)
	return time.ParseInLocation("2006-01-02", value, time.Local)
}

func startOfLocalDay(value time.Time) time.Time {
	local := value.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}
