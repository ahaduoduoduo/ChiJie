package admin

import (
	"sync"
	"time"
)

// loginLimiter 是按客户端 IP 维度的失败计数 + 锁定窗口。
// 内存实现，单进程使用即可。
type loginLimiter struct {
	mu          sync.Mutex
	maxFailures int
	window      time.Duration
	lockout     time.Duration
	entries     map[string]*loginAttempt
}

type loginAttempt struct {
	failures   int
	firstFail  time.Time
	lockedTill time.Time
}

func newLoginLimiter(maxFailures int, window, lockout time.Duration) *loginLimiter {
	if maxFailures <= 0 {
		maxFailures = 5
	}
	if window <= 0 {
		window = 60 * time.Second
	}
	if lockout <= 0 {
		lockout = 5 * time.Minute
	}
	return &loginLimiter{
		maxFailures: maxFailures,
		window:      window,
		lockout:     lockout,
		entries:     make(map[string]*loginAttempt),
	}
}

// allow 返回 (allowed, retryAfter)。
// 当前 IP 处于锁定窗口内时返回 false 和剩余等待时间。
func (l *loginLimiter) allow(ip string) (bool, time.Duration) {
	if ip == "" {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[ip]
	if entry == nil {
		return true, 0
	}
	if entry.lockedTill.After(now) {
		return false, entry.lockedTill.Sub(now)
	}
	// 窗口过期则重置
	if !entry.firstFail.IsZero() && now.Sub(entry.firstFail) > l.window {
		entry.failures = 0
		entry.firstFail = time.Time{}
	}
	return true, 0
}

// recordFailure 累加失败次数，达到阈值则锁定。
func (l *loginLimiter) recordFailure(ip string) {
	if ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	entry := l.entries[ip]
	if entry == nil {
		entry = &loginAttempt{}
		l.entries[ip] = entry
	}
	if entry.firstFail.IsZero() || now.Sub(entry.firstFail) > l.window {
		entry.firstFail = now
		entry.failures = 0
	}
	entry.failures++
	if entry.failures >= l.maxFailures {
		entry.lockedTill = now.Add(l.lockout)
	}
	// 简单清理：超过窗口 + 锁定时长仍未活动的条目移除
	for key, e := range l.entries {
		if e.lockedTill.Before(now.Add(-l.lockout)) && e.firstFail.Before(now.Add(-l.window)) {
			delete(l.entries, key)
		}
	}
}

// recordSuccess 登录成功后清除该 IP 的失败计数。
func (l *loginLimiter) recordSuccess(ip string) {
	if ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, ip)
}
