package engines

import (
	"context"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// FailoverEngine handles automatic database failover with health checking
type FailoverEngine struct {
	BaseEngine
	config      *FailoverConfig
	databases   map[string]*DatabaseNode
	primary     string
	failoverCtx *FailoverContext
	stats       *FailoverStats
	mu          sync.RWMutex
}

type FailoverConfig struct {
	Enabled              bool
	HealthCheckInterval  time.Duration
	HealthCheckTimeout   time.Duration
	FailureThreshold     int
	RecoveryThreshold    int
	FailoverTimeout      time.Duration
	EnableAutoFailover   bool
}

type DatabaseNode struct {
	Name        string
	Host        string
	Port        int
	Database    string
	IsPrimary   bool
	IsHealthy   bool
	LatencyMs   int64
	FailCount   int
	LastCheck   time.Time
}

type FailoverContext struct {
	FailoverInProgress bool
	SourceNode         string
	TargetNode         string
	StartTime          time.Time
	AttemptCount       int
}

type FailoverStats struct {
	FailoversTriggered   int64
	FailoversSuccessful  int64
	FailoversFailed      int64
	HealthChecksRun      int64
	HealthCheckFailures  int64
	AvgFailoverTimeMs    float64
	mu                   sync.RWMutex
}

// NewFailoverEngine creates a new Failover Engine
func NewFailoverEngine(config *FailoverConfig) *FailoverEngine {
	if config == nil {
		config = &FailoverConfig{
			Enabled:             false,
			HealthCheckInterval: 10 * time.Second,
			HealthCheckTimeout:  5 * time.Second,
			FailureThreshold:   3,
			RecoveryThreshold:  2,
			FailoverTimeout:    30 * time.Second,
			EnableAutoFailover: true,
		}
	}

	engine := &FailoverEngine{
		BaseEngine:  BaseEngine{name: "failover"},
		config:      config,
		databases:   make(map[string]*DatabaseNode),
		failoverCtx: &FailoverContext{},
		stats:       &FailoverStats{},
	}

	return engine
}

// AddDatabase adds a database node
func (e *FailoverEngine) AddDatabase(node *DatabaseNode) {
	e.mu.Lock()
	defer e.mu.Unlock()
	node.IsHealthy = true
	e.databases[node.Name] = node
	
	if node.IsPrimary {
		e.primary = node.Name
	}
}

// Process performs health checks and failover logic
func (e *FailoverEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	// Check if failover is in progress
	e.mu.RLock()
	if e.failoverCtx.FailoverInProgress {
		e.mu.RUnlock()
		// Route to failover target if available
		if e.failoverCtx.TargetNode != "" {
			if qc.Metadata == nil {
				qc.Metadata = make(map[string]interface{})
			}
			qc.Metadata["failover_target"] = e.failoverCtx.TargetNode
		}
	}
	e.mu.RUnlock()

	// Store failover info in metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["primary_node"] = e.primary

	return types.EngineResult{Continue: true}
}

// ProcessResponse processes response and updates health
func (e *FailoverEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil {
		// Update health metrics
		if qc.Response.Error != nil {
			e.recordFailure(qc.Database)
		} else {
			e.recordSuccess(qc.Database)
		}
	}

	// Check if failover is needed
	if e.config.EnableAutoFailover && e.shouldFailover() {
		e.triggerFailover()
	}

	return types.EngineResult{Continue: true}
}

// recordSuccess records a successful operation
func (e *FailoverEngine) recordSuccess(nodeName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if node, ok := e.databases[nodeName]; ok {
		node.FailCount = 0
		node.IsHealthy = true
		node.LastCheck = time.Now()
	}
}

// recordFailure records a failed operation
func (e *FailoverEngine) recordFailure(nodeName string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if node, ok := e.databases[nodeName]; ok {
		node.FailCount++
		if node.FailCount >= e.config.FailureThreshold {
			node.IsHealthy = false
		}
	}
}

// shouldFailover determines if failover should be triggered
func (e *FailoverEngine) shouldFailover() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.failoverCtx.FailoverInProgress {
		return false
	}

	primary, ok := e.databases[e.primary]
	if !ok || primary == nil {
		return true
	}

	return !primary.IsHealthy && primary.FailCount >= e.config.FailureThreshold
}

// triggerFailover initiates failover to a healthy replica
func (e *FailoverEngine) triggerFailover() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stats.mu.Lock()
	e.stats.FailoversTriggered++
	e.stats.mu.Unlock()

	e.failoverCtx.FailoverInProgress = true
	e.failoverCtx.SourceNode = e.primary
	e.failoverCtx.StartTime = time.Now()

	// Find healthy replica
	var targetNode string
	for name, node := range e.databases {
		if name != e.primary && node.IsHealthy {
			targetNode = name
			break
		}
	}

	if targetNode == "" {
		e.stats.mu.Lock()
		e.stats.FailoversFailed++
		e.stats.mu.Unlock()
		e.failoverCtx.FailoverInProgress = false
		return
	}

	e.failoverCtx.TargetNode = targetNode

	// Update primary
	e.primary = targetNode
	if node, ok := e.databases[targetNode]; ok {
		node.IsPrimary = true
	}

	failoverTime := time.Since(e.failoverCtx.StartTime).Milliseconds()
	e.stats.mu.Lock()
	e.stats.FailoversSuccessful++
	e.stats.AvgFailoverTimeMs = (e.stats.AvgFailoverTimeMs*float64(e.stats.FailoversSuccessful-1) + float64(failoverTime)) / float64(e.stats.FailoversSuccessful)
	e.stats.mu.Unlock()

	e.failoverCtx.FailoverInProgress = false
}

// GetFailoverStats returns failover statistics
func (e *FailoverEngine) GetFailoverStats() FailoverStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return FailoverStatsResponse{
		FailoversTriggered:  e.stats.FailoversTriggered,
		FailoversSuccessful: e.stats.FailoversSuccessful,
		FailoversFailed:    e.stats.FailoversFailed,
		HealthChecksRun:    e.stats.HealthChecksRun,
		HealthCheckFailures: e.stats.HealthCheckFailures,
		AvgFailoverTimeMs:  e.stats.AvgFailoverTimeMs,
	}
}

// GetPrimaryDatabase returns current primary database
func (e *FailoverEngine) GetPrimaryDatabase() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.primary
}

type FailoverStatsResponse struct {
	FailoversTriggered   int64   `json:"failovers_triggered"`
	FailoversSuccessful  int64   `json:"failovers_successful"`
	FailoversFailed      int64   `json:"failovers_failed"`
	HealthChecksRun      int64   `json:"health_checks_run"`
	HealthCheckFailures  int64   `json:"health_check_failures"`
	AvgFailoverTimeMs    float64 `json:"avg_failover_time_ms"`
}