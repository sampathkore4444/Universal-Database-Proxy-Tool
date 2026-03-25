package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

type RetryPolicy struct {
	MaxAttempts      int
	InitialDelay     time.Duration
	MaxDelay         time.Duration
	Multiplier       float64
	Jitter           bool
	RetryableErrors  []string
	NonRetryableErrs []string
}

type RetryableError struct {
	Message string
}

func (e *RetryableError) Error() string {
	return e.Message
}

var DefaultRetryPolicy = &RetryPolicy{
	MaxAttempts:  3,
	InitialDelay: 100 * time.Millisecond,
	MaxDelay:     5 * time.Second,
	Multiplier:   2.0,
	Jitter:       true,
}

func NewRetryPolicy(maxAttempts int, initialDelay, maxDelay time.Duration) *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:  maxAttempts,
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   2.0,
		Jitter:       true,
	}
}

func (rp *RetryPolicy) Execute(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 1; attempt <= rp.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		if !rp.isRetryable(lastErr) {
			return lastErr
		}

		if attempt == rp.MaxAttempts {
			break
		}

		sleepDuration := rp.calculateDelay(attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDuration):
		}
	}

	return fmt.Errorf("max retries exceeded, last error: %w", lastErr)
}

func (rp *RetryPolicy) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	for _, nonRetryable := range rp.NonRetryableErrs {
		if contains(errMsg, nonRetryable) {
			return false
		}
	}

	for _, retryable := range rp.RetryableErrors {
		if contains(errMsg, retryable) {
			return true
		}
	}

	return isCommonRetryableError(errMsg)
}

func (rp *RetryPolicy) calculateDelay(attempt int) time.Duration {
	delay := float64(rp.InitialDelay) * math.Pow(rp.Multiplier, float64(attempt-1))
	if delay > float64(rp.MaxDelay) {
		delay = float64(rp.MaxDelay)
	}

	if rp.Jitter {
		jitter := rand.Float64() * 0.5 * delay
		delay = delay + jitter
	}

	return time.Duration(delay)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func isCommonRetryableError(errMsg string) bool {
	retryablePatterns := []string{
		"connection refused",
		"timeout",
		"temporary failure",
		"network",
		"ECONNREFUSED",
		"ETIMEDOUT",
		"i/o timeout",
		"server closed connection",
		"too many connections",
		"deadlock",
		"resource governor",
	}

	lowerErr := errMsg
	for _, pattern := range retryablePatterns {
		if contains(lowerErr, pattern) {
			return true
		}
	}
	return false
}

type BackoffStrategy string

const (
	BackoffLinear      BackoffStrategy = "linear"
	BackoffExponential BackoffStrategy = "exponential"
	BackoffFibonacci   BackoffStrategy = "fibonacci"
)

type RetryManager struct {
	policies      map[string]*RetryPolicy
	defaultPolicy *RetryPolicy
}

func NewRetryManager() *RetryManager {
	return &RetryManager{
		policies:      make(map[string]*RetryPolicy),
		defaultPolicy: DefaultRetryPolicy,
	}
}

func (rm *RetryManager) RegisterPolicy(name string, policy *RetryPolicy) {
	rm.policies[name] = policy
}

func (rm *RetryManager) GetPolicy(name string) *RetryPolicy {
	if policy, ok := rm.policies[name]; ok {
		return policy
	}
	return rm.defaultPolicy
}

func (rm *RetryManager) ExecuteWithPolicy(ctx context.Context, policyName string, fn func() error) error {
	policy := rm.GetPolicy(policyName)
	return policy.Execute(ctx, fn)
}

func (rm *RetryManager) SetDefaultPolicy(policy *RetryPolicy) {
	rm.defaultPolicy = policy
}

type CircuitState int

const (
	CircuitStateClosed CircuitState = iota
	CircuitStateOpen
	CircuitStateHalfOpen
)

type CircuitBreaker struct {
	policy          *RetryPolicy
	failureCount    int
	successCount    int
	threshold       int
	timeout         time.Duration
	state           CircuitState
	lastFailureTime time.Time
	mu              int
}

func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     CircuitStateClosed,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu++
	defer func() { cb.mu-- }()

	switch cb.state {
	case CircuitStateOpen:
		if time.Since(cb.lastFailureTime) > cb.timeout {
			cb.state = CircuitStateHalfOpen
			cb.successCount = 0
		} else {
			return fmt.Errorf("circuit breaker is open")
		}

	case CircuitStateHalfOpen:
		if cb.successCount >= 3 {
			cb.state = CircuitStateClosed
			cb.failureCount = 0
		}
	}

	err := fn()
	if err != nil {
		cb.recordFailure()
		return err
	}

	cb.recordSuccess()
	return nil
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.failureCount >= cb.threshold {
		cb.state = CircuitStateOpen
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.successCount++

	if cb.state == CircuitStateHalfOpen && cb.successCount >= 3 {
		cb.state = CircuitStateClosed
		cb.failureCount = 0
	}
}

func (cb *CircuitBreaker) GetState() CircuitState {
	return cb.state
}

func (cb *CircuitBreaker) Reset() {
	cb.state = CircuitStateClosed
	cb.failureCount = 0
	cb.successCount = 0
}
