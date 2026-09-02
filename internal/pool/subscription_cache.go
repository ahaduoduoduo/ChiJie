package pool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"chijie/internal/dialer"
)

const subscriptionCacheVersion = 1

type subscriptionCache struct {
	path string
	mu   sync.Mutex
}

type subscriptionCacheFile struct {
	Version int                               `json:"version"`
	Entries map[string]subscriptionCacheEntry `json:"entries"`
}

type subscriptionCacheEntry struct {
	FetchedAt time.Time     `json:"fetched_at"`
	Nodes     []dialer.Node `json:"nodes"`
}

func newSubscriptionCache(path string) *subscriptionCache {
	return &subscriptionCache{path: filepath.Clean(path)}
}

func (c *subscriptionCache) load(rawURL string) ([]dialer.Node, time.Time, error) {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil, time.Time{}, fmt.Errorf("subscription cache is disabled")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cache, err := c.readLocked()
	if err != nil {
		return nil, time.Time{}, err
	}
	entry, ok := cache.Entries[subscriptionCacheKey(rawURL)]
	if !ok || len(entry.Nodes) == 0 {
		return nil, time.Time{}, fmt.Errorf("cached subscription not found")
	}
	nodes := append([]dialer.Node(nil), entry.Nodes...)
	return nodes, entry.FetchedAt, nil
}

func (c *subscriptionCache) save(rawURL string, nodes []dialer.Node) error {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil
	}
	if len(nodes) == 0 {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cache, err := c.readLocked()
	if err != nil {
		cache = subscriptionCacheFile{
			Version: subscriptionCacheVersion,
			Entries: make(map[string]subscriptionCacheEntry),
		}
	}
	if cache.Entries == nil {
		cache.Entries = make(map[string]subscriptionCacheEntry)
	}
	cache.Version = subscriptionCacheVersion
	cache.Entries[subscriptionCacheKey(rawURL)] = subscriptionCacheEntry{
		FetchedAt: time.Now().UTC(),
		Nodes:     append([]dialer.Node(nil), nodes...),
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("marshal subscription cache: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return fmt.Errorf("create subscription cache directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(c.path), 0700); err != nil {
		return fmt.Errorf("secure subscription cache directory: %w", err)
	}
	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write subscription cache: %w", err)
	}
	if err := os.Chmod(tmpPath, 0600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("secure subscription cache: %w", err)
	}
	if err := os.Rename(tmpPath, c.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace subscription cache: %w", err)
	}
	return nil
}

func (c *subscriptionCache) readLocked() (subscriptionCacheFile, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return subscriptionCacheFile{}, err
	}
	var cache subscriptionCacheFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return subscriptionCacheFile{}, fmt.Errorf("parse subscription cache: %w", err)
	}
	if cache.Version != subscriptionCacheVersion {
		return subscriptionCacheFile{}, fmt.Errorf("unsupported subscription cache version: %d", cache.Version)
	}
	if cache.Entries == nil {
		cache.Entries = make(map[string]subscriptionCacheEntry)
	}
	return cache, nil
}

func subscriptionCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(sum[:])
}
