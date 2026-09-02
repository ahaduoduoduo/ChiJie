package traffic

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePersistenceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Persistence.RetentionDays = 3

	first := NewStore(10)
	first.UpdateConfig(cfg)
	if err := first.EnablePersistence(dir); err != nil {
		t.Fatalf("enable first persistence: %v", err)
	}
	recorded := first.Record(Trace{
		Kind:        "proxy",
		Method:      "GET",
		URL:         "https://example.com/persisted",
		EgressGroup: "US",
		Status:      200,
	})
	first.Close()
	dailyPath := filepath.Join(dir, traceFilePrefix+recorded.Timestamp.In(time.Local).Format("2006-01-02")+traceFileSuffix)
	info, err := os.Stat(dailyPath)
	if err != nil {
		t.Fatalf("stat daily log: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("daily log permissions = %o want 600", got)
	}

	second := NewStore(10)
	second.UpdateConfig(cfg)
	if err := second.EnablePersistence(dir); err != nil {
		t.Fatalf("enable second persistence: %v", err)
	}
	defer second.Close()

	snapshot := second.Snapshot(10)
	if len(snapshot.Traces) != 1 || snapshot.Traces[0].URL != "https://example.com/persisted" {
		t.Fatalf("unexpected restored traces: %#v", snapshot.Traces)
	}
	if snapshot.Metrics.Requests != 1 || snapshot.Metrics.Success != 1 {
		t.Fatalf("unexpected restored metrics: %#v", snapshot.Metrics)
	}
}

func TestStorePersistencePrunesExpiredDailyFiles(t *testing.T) {
	dir := t.TempDir()
	oldDate := startOfLocalDay(time.Now()).AddDate(0, 0, -3).Format("2006-01-02")
	oldPath := filepath.Join(dir, traceFilePrefix+oldDate+traceFileSuffix)
	if err := os.WriteFile(oldPath, nil, 0600); err != nil {
		t.Fatalf("write old log: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Persistence.RetentionDays = 2
	store := NewStore(10)
	store.UpdateConfig(cfg)
	if err := store.EnablePersistence(dir); err != nil {
		t.Fatalf("enable persistence: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected expired log to be removed, stat error: %v", err)
	}
}
