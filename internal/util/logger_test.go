package util

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldOut := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOut)
		log.SetFlags(oldFlags)
	}()
	fn()
	return buf.String()
}

func TestLogLevelDefault(t *testing.T) {
	SetLogLevel("")
	out := captureLog(t, func() {
		Debugf("d")
		Infof("i")
	})
	if strings.Contains(out, "d") {
		t.Errorf("debug should be suppressed at default level, got %q", out)
	}
	if !strings.Contains(out, "i") {
		t.Errorf("info should be visible at default level, got %q", out)
	}
}

func TestLogLevelDebugEnables(t *testing.T) {
	SetLogLevel("debug")
	defer SetLogLevel("info")
	out := captureLog(t, func() {
		Debugf("hello debug")
	})
	if !strings.Contains(out, "[DEBUG] hello debug") {
		t.Errorf("expected debug output, got %q", out)
	}
}

func TestLogLevelErrorOnly(t *testing.T) {
	SetLogLevel("error")
	defer SetLogLevel("info")
	out := captureLog(t, func() {
		Infof("info should be hidden")
		Warnf("warn should be hidden")
		Errorf("error visible")
	})
	if strings.Contains(out, "info should be hidden") {
		t.Errorf("info leaked: %q", out)
	}
	if strings.Contains(out, "warn should be hidden") {
		t.Errorf("warn leaked: %q", out)
	}
	if !strings.Contains(out, "[ERROR] error visible") {
		t.Errorf("error suppressed: %q", out)
	}
}

func TestLogLevelInvalidFallsBackToInfo(t *testing.T) {
	SetLogLevel("nonsense-level")
	defer SetLogLevel("info")
	if got := CurrentLogLevel(); got != LogLevelInfo {
		t.Errorf("expected info fallback, got %v", got)
	}
}
