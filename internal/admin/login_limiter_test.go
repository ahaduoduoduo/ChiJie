package admin

import (
	"testing"
	"time"
)

func TestLoginLimiterAllowsByDefault(t *testing.T) {
	l := newLoginLimiter(3, time.Second, time.Second)
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Errorf("first attempt should be allowed")
	}
}

func TestLoginLimiterLocksAfterMaxFailures(t *testing.T) {
	l := newLoginLimiter(3, time.Minute, time.Minute)
	for i := 0; i < 3; i++ {
		l.recordFailure("1.2.3.4")
	}
	ok, retryAfter := l.allow("1.2.3.4")
	if ok {
		t.Errorf("should be locked after 3 failures")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter should be positive, got %v", retryAfter)
	}
}

func TestLoginLimiterResetsOnSuccess(t *testing.T) {
	l := newLoginLimiter(3, time.Minute, time.Minute)
	l.recordFailure("1.2.3.4")
	l.recordFailure("1.2.3.4")
	l.recordSuccess("1.2.3.4")
	if ok, _ := l.allow("1.2.3.4"); !ok {
		t.Errorf("should be allowed after success")
	}
}

func TestLoginLimiterEmptyIPAllowed(t *testing.T) {
	l := newLoginLimiter(3, time.Minute, time.Minute)
	if ok, _ := l.allow(""); !ok {
		t.Errorf("empty IP should not be limited")
	}
}

func TestLoginLimiterPerIPIsolation(t *testing.T) {
	l := newLoginLimiter(2, time.Minute, time.Minute)
	l.recordFailure("1.1.1.1")
	l.recordFailure("1.1.1.1")
	if ok, _ := l.allow("2.2.2.2"); !ok {
		t.Errorf("different IP should not be locked")
	}
}
