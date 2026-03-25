package middleware

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	mu           sync.RWMutex
	limiters     map[string]*rate.Limiter
	config       *RateLimitConfig
	cleanupTimer *time.Ticker
	stopCleanup  chan struct{}
}

type RateLimitConfig struct {
	RequestsPerSecond float64
	BurstSize         int
	ByUser            bool
	ByDatabase        bool
	ByIP              bool
	WindowSize        time.Duration
}

type RateLimitKey struct {
	User      string
	Database  string
	IP        string
	Operation string
}

func NewRateLimiter(config *RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		limiters:    make(map[string]*rate.Limiter),
		config:      config,
		stopCleanup: make(chan struct{}),
	}

	if config.RequestsPerSecond > 0 {
		rl.cleanupTimer = time.NewTicker(5 * time.Minute)
		go rl.cleanup()
	}

	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.RLock()
	limiter, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		limiter, exists = rl.limiters[key]
		if !exists {
			limiter = rate.NewLimiter(rate.Limit(rl.config.RequestsPerSecond), rl.config.BurstSize)
			rl.limiters[key] = limiter
		}
		rl.mu.Unlock()
	}

	return limiter.Allow()
}

func (rl *RateLimiter) AllowN(key string, n int) bool {
	rl.mu.RLock()
	limiter, exists := rl.limiters[key]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		limiter, exists = rl.limiters[key]
		if !exists {
			limiter = rate.NewLimiter(rate.Limit(rl.config.RequestsPerSecond), rl.config.BurstSize)
			rl.limiters[key] = limiter
		}
		rl.mu.Unlock()
	}

	return limiter.AllowN(time.Now(), n)
}

func (rl *RateLimiter) BuildKey(ctx context.Context, user, database, ip, operation string) string {
	keyParts := []string{}

	if rl.config.ByUser && user != "" {
		keyParts = append(keyParts, "user:"+user)
	}
	if rl.config.ByDatabase && database != "" {
		keyParts = append(keyParts, "db:"+database)
	}
	if rl.config.ByIP && ip != "" {
		keyParts = append(keyParts, "ip:"+ip)
	}
	if operation != "" {
		keyParts = append(keyParts, "op:"+operation)
	}

	if len(keyParts) == 0 {
		return "default"
	}

	return fmt.Sprintf("%s", keyParts)
}

func (rl *RateLimiter) GetLimiter(key string) (*rate.Limiter, bool) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	limiter, ok := rl.limiters[key]
	return limiter, ok
}

func (rl *RateLimiter) SetLimit(key string, rps float64, burst int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.limiters[key] = rate.NewLimiter(rate.Limit(rps), burst)
}

func (rl *RateLimiter) RemoveLimiter(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.limiters, key)
}

func (rl *RateLimiter) cleanup() {
	for {
		select {
		case <-rl.cleanupTimer.C:
			rl.mu.Lock()
			for key, limiter := range rl.limiters {
				if !limiter.Allow() {
					delete(rl.limiters, key)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCleanup:
			return
		}
	}
}

func (rl *RateLimiter) Stop() {
	if rl.cleanupTimer != nil {
		rl.cleanupTimer.Stop()
	}
	close(rl.stopCleanup)
}

func GetClientIP(conn net.Conn) string {
	if conn == nil {
		return ""
	}
	addr := conn.RemoteAddr()
	if addr != nil {
		return addr.String()
	}
	return ""
}

type IPRateLimiter struct {
	mu           sync.RWMutex
	limiters     map[string]*clientLimiter
	rate         rate.Limit
	burst        int
	cleanupTimer *time.Ticker
	stopCleanup  chan struct{}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewIPRateLimiter(rps float64, burst int) *IPRateLimiter {
	irl := &IPRateLimiter{
		limiters:    make(map[string]*clientLimiter),
		rate:        rate.Limit(rps),
		burst:       burst,
		stopCleanup: make(chan struct{}),
	}

	irl.cleanupTimer = time.NewTicker(1 * time.Minute)
	go irl.cleanup()

	return irl
}

func (irl *IPRateLimiter) Allow(ip string) bool {
	irl.mu.Lock()
	defer irl.mu.Unlock()

	cl, exists := irl.limiters[ip]
	if !exists {
		cl = &clientLimiter{
			limiter:  rate.NewLimiter(irl.rate, irl.burst),
			lastSeen: time.Now(),
		}
		irl.limiters[ip] = cl
	}

	cl.lastSeen = time.Now()
	return cl.limiter.Allow()
}

func (irl *IPRateLimiter) cleanup() {
	for {
		select {
		case <-irl.cleanupTimer.C:
			irl.mu.Lock()
			for ip, cl := range irl.limiters {
				if time.Since(cl.lastSeen) > 10*time.Minute {
					delete(irl.limiters, ip)
				}
			}
			irl.mu.Unlock()
		case <-irl.stopCleanup:
			return
		}
	}
}

func (irl *IPRateLimiter) Stop() {
	if irl.cleanupTimer != nil {
		irl.cleanupTimer.Stop()
	}
	close(irl.stopCleanup)
}
