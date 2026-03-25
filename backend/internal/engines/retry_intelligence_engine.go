package engines

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// RetryIntelligenceEngine handles smart retry with exponential backoff and circuit breaker
type RetryIntelligenceEngine struct {
	BaseEngine
	config          *RetryConfig
	circuitBreaker  *CircuitBreaker
	retryPolicies   map[string]*RetryPolicy
	stats           *RetryStats
	mu              sync.RWMutex
}

type RetryConfig struct {
	Enabled             bool
	MaxRetries          int
	InitialDelayMs      int
	MaxDelayMs          int
	BackoffMultiplier   float64
	EnableCircuitBreaker bool
	CircuitBreakerThreshold int
	CircuitBreakerTimeout time.Duration
}

type CircuitBreaker struct {
	State             string // closed, open, half-open
	FailureCount      int
	SuccessCount      int
	LastFailureTime    time.Time
	FailureThreshold  int
	SuccessThreshold  int
	Timeout           time.Duration
	mu                sync.RWMutex
}

type RetryPolicy struct {
	Name         string
	MatchPattern string
	MaxRetries   int
	DelayMs      int
	BackoffMultiplier float64
}

type RetryStats struct {
	TotalRetries      int64
	SuccessfulRetries int64
	CircuitBreakerOpens int64
	CircuitBreakerCloses int64
	AvgRetryLatencyMs float64
	mu               sync.RWMutex
}

// NewRetryIntelligenceEngine creates a new Retry Intelligence Engine
func NewRetryIntelligenceEngine(config *RetryConfig) *RetryIntelligenceEngine {
	if config == nil {
		config = &RetryConfig{
			Enabled:             true,
			MaxRetries:          3,
			InitialDelayMs:      100,
			MaxDelayMs:          10000,
			BackoffMultiplier:   2.0,
			EnableCircuitBreaker: true,
			CircuitBreakerThreshold: 5,
			CircuitBreakerTimeout: time.Minute,
		}
	}

	engine := &RetryIntelligenceEngine{
		BaseEngine:    BaseEngine{name: "retry_intelligence"},
		config:        config,
		circuitBreaker: &CircuitBreaker{
			State:            "closed",
			FailureThreshold: config.CircuitBreakerThreshold,
			SuccessThreshold: 3,
			Timeout:          config.CircuitBreakerTimeout,
		},
		retryPolicies: make(map[string]*RetryPolicy),
		stats:         &RetryStats{},
	}

	return engine
}

// Process handles retry logic - this runs before query execution
func (e *RetryIntelligenceEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	// Check circuit breaker
	if e.config.EnableCircuitBreaker {
		if !e.isCircuitClosed() {
			e.stats.mu.Lock()
			e.stats.CircuitBreakerOpens++
			e.stats.mu.Unlock()

			return types.EngineResult{
				Continue: false,
				Error:    fmt.Errorf("circuit breaker is open"),
			}
		}
	}

	// Store retry metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["retry_enabled"] = true
	qc.Metadata["max_retries"] = e.config.MaxRetries

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles retry response - records success/failure
func (e *RetryIntelligenceEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response == nil {
		// Query failed - record failure for circuit breaker
		e.recordFailure()
	} else if qc.Response.Error == nil {
		// Query succeeded - record success
		e.recordSuccess()

		// Track retry stats
		if retryCount, ok := qc.Metadata["retry_count"].(int); ok && retryCount > 0 {
			e.stats.mu.Lock()
			e.stats.SuccessfulRetries += int64(retryCount)
			e.stats.TotalRetries += int64(retryCount)
			e.stats.mu.Unlock()
		}
	}

	return types.EngineResult{Continue: true}
}

// isCircuitClosed checks if circuit breaker allows requests
func (e *RetryIntelligenceEngine) isCircuitClosed() bool {
	e.circuitBreaker.mu.RLock()
	defer e.circuitBreaker.mu.RUnlock()

	if e.circuitBreaker.State == "closed" {
		return true
	}

	if e.circuitBreaker.State == "open" {
		// Check if timeout has passed
		if time.Since(e.circuitBreaker.LastFailureTime) > e.circuitBreaker.Timeout {
			e.circuitBreaker.State = "half-open"
			return true
		}
		return false
	}

	// half-open - allow one request
	return true
}

// recordFailure records a failure for circuit breaker
func (e *RetryIntelligenceEngine) recordFailure() {
	e.circuitBreaker.mu.Lock()
	defer e.circuitBreaker.mu.Unlock()

	e.circuitBreaker.FailureCount++
	e.circuitBreaker.LastFailureTime = time.Now()

	if e.circuitBreaker.FailureCount >= e.circuitBreaker.FailureThreshold {
		e.circuitBreaker.State = "open"
	}
}

// recordSuccess records a success for circuit breaker
func (e *RetryIntelligenceEngine) recordSuccess() {
	e.circuitBreaker.mu.Lock()
	defer e.circuitBreaker.mu.Unlock()

	e.circuitBreaker.SuccessCount++

	if e.circuitBreaker.State == "half-open" && e.circuitBreaker.SuccessCount >= e.circuitBreaker.SuccessThreshold {
		e.circuitBreaker.State = "closed"
		e.circuitBreaker.FailureCount = 0
		e.circuitBreaker.SuccessCount = 0
	}
}

// CalculateBackoff calculates the delay for a retry attempt
func (e *RetryIntelligenceEngine) CalculateBackoff(attempt int) time.Duration {
	delay := float64(e.config.InitialDelayMs) * math.Pow(e.config.BackoffMultiplier, float64(attempt))
	
	// Cap at max delay
	if delay > float64(e.config.MaxDelayMs) {
		delay = float64(e.config.MaxDelayMs)
	}
	
	return time.Duration(int(delay)) * time.Millisecond
}

// AddRetryPolicy adds a custom retry policy
func (e *RetryIntelligenceEngine) AddRetryPolicy(policy *RetryPolicy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.retryPolicies[policy.Name] = policy
}

// GetRetryStats returns retry statistics
func (e *RetryIntelligenceEngine) GetRetryStats() RetryStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	e.circuitBreaker.mu.RLock()
	state := e.circuitBreaker.State
	e.circuitBreaker.mu.RUnlock()

	return RetryStatsResponse{
		TotalRetries:          e.stats.TotalRetries,
		SuccessfulRetries:     e.stats.SuccessfulRetries,
		CircuitBreakerOpens:   e.stats.CircuitBreakerOpens,
		CircuitBreakerCloses:  e.stats.CircuitBreakerCloses,
		AvgRetryLatencyMs:     e.stats.AvgRetryLatencyMs,
		CircuitBreakerState:   state,
	}
}

type RetryStatsResponse struct {
	TotalRetries        int64   `json:"total_retries"`
	SuccessfulRetries   int64   `json:"successful_retries"`
	CircuitBreakerOpens int64   `json:"circuit_breaker_opens"`
	CircuitBreakerCloses int64   `json:"circuit_breaker_closes"`
	AvgRetryLatencyMs   float64 `json:"avg_retry_latency_ms"`
	CircuitBreakerState string  `json:"circuit_breaker_state"`
}