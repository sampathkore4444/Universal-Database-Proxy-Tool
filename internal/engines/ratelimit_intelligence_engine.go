package engines

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// RateLimitIntelligenceEngine provides adaptive rate limiting with ML-based anomaly detection
type RateLimitIntelligenceEngine struct {
	BaseEngine
	config         *RateLimitConfig
	clientTracker  *ClientTracker
	anomalyDetector *AnomalyDetector
	circuitBreaker *CircuitBreaker
	mu            sync.RWMutex
}

type RateLimitConfig struct {
	Enabled              bool          // Enable the engine
	GlobalLimit          int           // Global requests per window
	PerClientLimit       int           // Per-client requests per window
	WindowSize           time.Duration // Rate limit window
	AnomalyDetection     bool          // Enable ML anomaly detection
	CircuitBreakerEnabled bool         // Enable circuit breaker
	CircuitBreakerThreshold int        // Error threshold for circuit break
	CircuitBreakerTimeout time.Duration // How long circuit stays open
	AdaptiveThrottling   bool         // Enable adaptive rate limiting
	EnableIPWhitelist    bool          // Enable IP whitelist
}

type ClientTracker struct {
	clients    map[string]*ClientState
	windowSize time.Duration
	mu         sync.RWMutex
}

type ClientState struct {
	ClientID        string
	Requests        []time.Time
	TotalRequests   int64
	BlockedCount    int64
	LastBlocked     time.Time
	CurrentLimit    int           // Dynamic limit based on behavior
	TrustScore      float64       // 0-1 trust score
	IsWhitelisted   bool
	AnomalyScore    float64       // 0-1 anomaly score
	BurstAllowance  int           // Extra requests allowed for bursts
}

type AnomalyDetector struct {
	baselineRequestsPerSecond float64
	windowDuration            time.Duration
	history                   []timeSeriesPoint
	mu                        sync.RWMutex
	threshold                 float64 // Z-score threshold for anomaly
}

type timeSeriesPoint struct {
	Timestamp time.Time
	Value     float64
}

type CircuitBreaker struct {
	state          CircuitState // CLOSED, OPEN, HALF_OPEN
	failureCount   int
	lastFailure    time.Time
	threshold      int
	timeout        time.Duration
	mu             sync.RWMutex
}

type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// NewRateLimitIntelligenceEngine creates a new Rate Limit Intelligence Engine
func NewRateLimitIntelligenceEngine(config *RateLimitConfig) *RateLimitIntelligenceEngine {
	if config == nil {
		config = &RateLimitConfig{
			Enabled:               true,
			GlobalLimit:           10000,
			PerClientLimit:        1000,
			WindowSize:            time.Minute,
			AnomalyDetection:      true,
			CircuitBreakerEnabled: true,
			CircuitBreakerThreshold: 50,
			CircuitBreakerTimeout:  30 * time.Second,
			AdaptiveThrottling:     true,
			EnableIPWhitelist:      false,
		}
	}

	engine := &RateLimitIntelligenceEngine{
		BaseEngine:      BaseEngine{name: "rate_limit_intelligence"},
		config:          config,
		clientTracker:   &ClientTracker{clients: make(map[string]*ClientState), windowSize: config.WindowSize},
		anomalyDetector: &AnomalyDetector{
			threshold:      3.0, // 3 standard deviations
			windowDuration: 5 * time.Minute,
			history:        make([]timeSeriesPoint, 0),
		},
		circuitBreaker: &CircuitBreaker{
			state:     CircuitClosed,
			threshold: config.CircuitBreakerThreshold,
			timeout:   config.CircuitBreakerTimeout,
		},
	}

	// Start background monitoring
	go engine.monitorLoop()

	return engine
}

// Process handles rate limiting logic
func (e *RateLimitIntelligenceEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	clientID := e.getClientID(qc)

	// Check circuit breaker
	if e.circuitBreaker.state == CircuitOpen {
		if time.Since(e.circuitBreaker.lastFailure) > e.circuitBreaker.timeout {
			// Try half-open
			e.circuitBreaker.mu.Lock()
			e.circuitBreaker.state = CircuitHalfOpen
			e.circuitBreaker.mu.Unlock()
		} else {
			return types.EngineResult{
				Continue: false,
				Error:    fmt.Errorf("circuit breaker open: service temporarily unavailable"),
				Metadata: map[string]interface{}{"reason": "circuit_breaker_open"},
			}
		}
	}

	// Check whitelist
	if e.config.EnableIPWhitelist {
		if state, exists := e.clientTracker.clients[clientID]; exists && state.IsWhitelisted {
			return types.EngineResult{Continue: true}
		}
	}

	e.clientTracker.mu.Lock()
	defer e.clientTracker.mu.Unlock()

	// Get or create client state
	state, exists := e.clientTracker.clients[clientID]
	if !exists {
		state = &ClientState{
			ClientID:       clientID,
			CurrentLimit:   e.config.PerClientLimit,
			TrustScore:     0.5, // Start neutral
			BurstAllowance: 10,
		}
		e.clientTracker.clients[clientID] = state
	}

	// Clean old requests
	cutoff := time.Now().Add(-e.config.WindowSize)
	state.Requests = filterRequestsAfter(state.Requests, cutoff)

	// Check if client limit exceeded
	if len(state.Requests) >= state.CurrentLimit {
		state.BlockedCount++
		state.LastBlocked = time.Now()

		// Reduce trust score
		state.TrustScore = math.Max(0, state.TrustScore-0.1)

		// Adaptive: reduce limit for suspicious clients
		if e.config.AdaptiveThrottling && state.TrustScore < 0.5 {
			state.CurrentLimit = int(float64(e.config.PerClientLimit) * state.TrustScore)
		}

		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("rate limit exceeded for client: %s", clientID),
			Metadata: map[string]interface{}{
				"reason":        "rate_limit_exceeded",
				"current_limit": state.CurrentLimit,
				"requests":      len(state.Requests),
				"trust_score":   state.TrustScore,
			},
		}
	}

	// Check anomaly detection
	if e.config.AnomalyDetection {
		e.anomalyDetector.mu.Lock()
		anomalyScore := e.calculateAnomalyScore(clientID)
		state.AnomalyScore = anomalyScore

		// If highly anomalous, apply extra throttling
		if anomalyScore > 0.8 && len(state.Requests) > state.CurrentLimit/2 {
			state.CurrentLimit = int(float64(state.CurrentLimit) * 0.5)
			e.anomalyDetector.mu.Unlock()
			return types.EngineResult{
				Continue: false,
				Error:    fmt.Errorf("anomalous traffic detected from: %s", clientID),
				Metadata: map[string]interface{}{
					"reason":       "anomaly_detected",
					"anomaly_score": anomalyScore,
				},
			}
		}
		e.anomalyDetector.mu.Unlock()
	}

	// Record this request
	state.Requests = append(state.Requests, time.Now())
	state.TotalRequests++

	// Gradually restore trust score for good behavior
	if state.TrustScore < 1.0 && len(state.Requests) < state.CurrentLimit/2 {
		state.TrustScore = math.Min(1.0, state.TrustScore+0.01)
		// Restore limit
		state.CurrentLimit = int(float64(e.config.PerClientLimit) * (0.5 + 0.5*state.TrustScore))
	}

	// Add rate limit info to metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["rate_limit_remaining"] = state.CurrentLimit - len(state.Requests)
	qc.Metadata["trust_score"] = state.TrustScore
	qc.Metadata["anomaly_score"] = state.AnomalyScore

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles response-based logic (for circuit breaker)
func (e *RateLimitIntelligenceEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.CircuitBreakerEnabled {
		return types.EngineResult{Continue: true}
	}

	// Track successful responses
	if qc.Error == nil {
		e.circuitBreaker.mu.Lock()
		if e.circuitBreaker.state == CircuitHalfOpen {
			// Success in half-open, close the circuit
			e.circuitBreaker.state = CircuitClosed
			e.circuitBreaker.failureCount = 0
		}
		e.circuitBreaker.mu.Unlock()
	} else {
		// Track failures
		e.circuitBreaker.mu.Lock()
		e.circuitBreaker.failureCount++
		e.circuitBreaker.lastFailure = time.Now()

		if e.circuitBreaker.state == CircuitHalfOpen || e.circuitBreaker.failureCount >= e.circuitBreaker.threshold {
			e.circuitBreaker.state = CircuitOpen
		}
		e.circuitBreaker.mu.Unlock()
	}

	// Update global request rate for anomaly detection
	e.anomalyDetector.mu.Lock()
	e.anomalyDetector.history = append(e.anomalyDetector.history, timeSeriesPoint{
		Timestamp: time.Now(),
		Value:     1.0,
	})
	e.pruneHistory()
	e.anomalyDetector.mu.Unlock()

	return types.EngineResult{Continue: true}
}

// getClientID extracts client identifier from query context
func (e *RateLimitIntelligenceEngine) getClientID(qc *types.QueryContext) string {
	if qc.ClientInfo != nil && qc.ClientInfo.ClientID != "" {
		return qc.ClientInfo.ClientID
	}
	// Fallback to IP
	if qc.ClientInfo != nil && qc.ClientInfo.RemoteAddr != "" {
		return qc.ClientInfo.RemoteAddr
	}
	return "unknown"
}

// calculateAnomalyScore calculates ML-based anomaly score for client
func (e *RateLimitIntelligenceEngine) calculateAnomalyScore(clientID string) float64 {
	e.clientTracker.mu.RLock()
	defer e.clientTracker.mu.RUnlock()

	state, exists := e.clientTracker.clients[clientID]
	if !exists {
		return 0
	}

	score := 0.0

	// Check request rate anomaly
	recentRequests := countRecentRequests(state.Requests, time.Now().Add(-time.Minute))
	avgRequests := e.getAverageRequestsPerMinute()

	if avgRequests > 0 {
		ratio := float64(recentRequests) / avgRequests
		if ratio > 5 {
			score += 0.5
		} else if ratio > 2 {
			score += 0.3
		}
	}

	// Check blocked ratio
	if state.TotalRequests > 0 {
		blockedRatio := float64(state.BlockedCount) / float64(state.TotalRequests)
		if blockedRatio > 0.5 {
			score += 0.3
		}
	}

	// Check burst pattern (many requests in very short time)
	burstCount := countRecentRequests(state.Requests, time.Now().Add(-5*time.Second))
	if burstCount > 50 {
		score += 0.2
	}

	return math.Min(1.0, score)
}

// getAverageRequestsPerMinute calculates global average
func (e *RateLimitIntelligenceEngine) getAverageRequestsPerMinute() float64 {
	e.anomalyDetector.mu.RLock()
	defer e.anomalyDetector.mu.RUnlock()

	if len(e.anomalyDetector.history) < 2 {
		return 0
	}

	// Calculate requests in last minute
	recent := filterPointsAfter(e.anomalyDetector.history, time.Now().Add(-time.Minute))
	return float64(len(recent))
}

// filterRequestsAfter filters requests after a cutoff time
func filterRequestsAfter(requests []time.Time, cutoff time.Time) []time.Time {
	result := make([]time.Time, 0)
	for _, t := range requests {
		if t.After(cutoff) {
			result = append(result, t)
		}
	}
	return result
}

// filterPointsAfter filters time series points after a cutoff
func filterPointsAfter(points []timeSeriesPoint, cutoff time.Time) []timeSeriesPoint {
	result := make([]timeSeriesPoint, 0)
	for _, p := range points {
		if p.Timestamp.After(cutoff) {
			result = append(result, p)
		}
	}
	return result
}

// countRecentRequests counts requests in the last duration
func countRecentRequests(requests []time.Time, since time.Time) int {
	count := 0
	for _, t := range requests {
		if t.After(since) {
			count++
		}
	}
	return count
}

// pruneHistory removes old data points
func (e *AnomalyDetector) pruneHistory() {
	cutoff := time.Now().Add(-e.windowDuration)
	i := 0
	for ; i < len(e.history); i++ {
		if e.history[i].Timestamp.After(cutoff) {
			break
		}
	}
	if i > 0 {
		e.history = e.history[i:]
	}
}

// monitorLoop performs periodic maintenance
func (e *RateLimitIntelligenceEngine) monitorLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Clean up old client data
		e.clientTracker.mu.Lock()
		cutoff := time.Now().Add(-30 * time.Minute)
		for id, state := range e.clientTracker.clients {
			if state.LastBlocked.Before(cutoff) && state.TotalRequests > 0 {
				// Keep good clients longer
				if state.TrustScore > 0.8 {
					continue
				}
			}
			// Remove clients with no recent activity
			if len(state.Requests) == 0 && time.Since(state.LastBlocked) > 10*time.Minute {
				delete(e.clientTracker.clients, id)
			}
		}
		e.clientTracker.mu.Unlock()

		// Check circuit breaker timeout
		e.circuitBreaker.mu.Lock()
		if e.circuitBreaker.state == CircuitOpen && time.Since(e.circuitBreaker.lastFailure) > e.circuitBreaker.timeout {
			e.circuitBreaker.state = CircuitHalfOpen
		}
		e.circuitBreaker.mu.Unlock()
	}
}

// GetClientStats returns stats for a specific client
func (e *RateLimitIntelligenceEngine) GetClientStats(clientID string) (ClientStatsResponse, bool) {
	e.clientTracker.mu.RLock()
	defer e.clientTracker.mu.RUnlock()

	state, exists := e.clientTracker.clients[clientID]
	if !exists {
		return ClientStatsResponse{}, false
	}

	cutoff := time.Now().Add(-e.config.WindowSize)
	recentRequests := filterRequestsAfter(state.Requests, cutoff)

	return ClientStatsResponse{
		ClientID:       clientID,
		RequestsInWindow: len(recentRequests),
		CurrentLimit:   state.CurrentLimit,
		TrustScore:     state.TrustScore,
		AnomalyScore:   state.AnomalyScore,
		BlockedCount:   state.BlockedCount,
		TotalRequests:  state.TotalRequests,
		IsWhitelisted:  state.IsWhitelisted,
	}, true
}

// AddToWhitelist adds a client to the whitelist
func (e *RateLimitIntelligenceEngine) AddToWhitelist(clientID string) {
	e.clientTracker.mu.Lock()
	defer e.clientTracker.mu.Unlock()

	if state, exists := e.clientTracker.clients[clientID]; exists {
		state.IsWhitelisted = true
		state.TrustScore = 1.0
	} else {
		e.clientTracker.clients[clientID] = &ClientState{
			ClientID:      clientID,
			IsWhitelisted: true,
			TrustScore:    1.0,
			CurrentLimit:  e.config.PerClientLimit * 10, // Higher limit for whitelist
		}
	}
}

// GetCircuitBreakerStatus returns the current circuit breaker state
func (e *RateLimitIntelligenceEngine) GetCircuitBreakerStatus() CircuitBreakerStatus {
	e.circuitBreaker.mu.RLock()
	defer e.circuitBreaker.mu.RUnlock()

	stateName := "closed"
	switch e.circuitBreaker.state {
	case CircuitOpen:
		stateName = "open"
	case CircuitHalfOpen:
		stateName = "half-open"
	}

	return CircuitBreakerStatus{
		State:         stateName,
		FailureCount:  e.circuitBreaker.failureCount,
		LastFailure:   e.circuitBreaker.lastFailure,
		Threshold:     e.circuitBreaker.threshold,
	}
}

// Helper types for API responses
type ClientStatsResponse struct {
	ClientID        string  `json:"client_id"`
	RequestsInWindow int    `json:"requests_in_window"`
	CurrentLimit    int     `json:"current_limit"`
	TrustScore      float64 `json:"trust_score"`
	AnomalyScore    float64 `json:"anomaly_score"`
	BlockedCount    int64   `json:"blocked_count"`
	TotalRequests   int64   `json:"total_requests"`
	IsWhitelisted   bool    `json:"is_whitelisted"`
}

type CircuitBreakerStatus struct {
	State        string    `json:"state"`
	FailureCount int       `json:"failure_count"`
	LastFailure  time.Time `json:"last_failure"`
	Threshold    int       `json:"threshold"`
}