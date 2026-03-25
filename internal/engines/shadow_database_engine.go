package engines

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// ShadowDatabaseEngine mirrors queries to test/QA database without affecting production
type ShadowDatabaseEngine struct {
	BaseEngine
	config      *ShadowConfig
	targetDBs   map[string]*ShadowDatabase
	matchRules  []*ShadowMatchRule
	stats       *ShadowStats
	mu          sync.RWMutex
}

type ShadowConfig struct {
	Enabled          bool
	Mode             string // mirror, diff, replay
	TargetDatabases  []string
	SampleRate       float64 // 0.0 to 1.0
	DelayMs          int // delay before executing on shadow
	EnabledOps       []string // SELECT, INSERT, UPDATE, DELETE
}

type ShadowDatabase struct {
	Name      string
	Host      string
	Port      int
	Database  string
	Username  string
	Password  string
	Type      string
	Status    string
}

type ShadowMatchRule struct {
	Pattern    string
	TargetDB   string
	Priority   int
	Enabled    bool
}

type ShadowStats struct {
	QueriesMirrored   int64
	QueriesDiffed     int64
	QueriesReplayed   int64
	ShadowErrors      int64
	AvgLatencyMs      float64
	mu                sync.RWMutex
}

// NewShadowDatabaseEngine creates a new Shadow Database Engine
func NewShadowDatabaseEngine(config *ShadowConfig) *ShadowDatabaseEngine {
	if config == nil {
		config = &ShadowConfig{
			Enabled:       false,
			Mode:         "mirror",
			SampleRate:   1.0,
			DelayMs:      0,
			EnabledOps:   []string{"SELECT", "INSERT", "UPDATE", "DELETE"},
		}
	}

	engine := &ShadowDatabaseEngine{
		BaseEngine: BaseEngine{name: "shadow_database"},
		config:     config,
		targetDBs:  make(map[string]*ShadowDatabase),
		matchRules: make([]*ShadowMatchRule, 0),
		stats:      &ShadowStats{},
	}

	return engine
}

// AddShadowDatabase adds a target shadow database
func (e *ShadowDatabaseEngine) AddShadowDatabase(db *ShadowDatabase) {
	e.mu.Lock()
	defer e.mu.Unlock()
	db.Status = "active"
	e.targetDBs[db.Name] = db
}

// Process handles shadow database routing
func (e *ShadowDatabaseEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Check if operation is enabled for shadow
	upperQuery := strings.ToUpper(query)
	opType := e.detectOperation(upperQuery)
	if !e.isOperationEnabled(opType) {
		return types.EngineResult{Continue: true}
	}

	// Sampling check
	if e.config.SampleRate < 1.0 {
		if time.Now().UnixNano()%100 > int64(e.config.SampleRate*100) {
			return types.EngineResult{Continue: true}
		}
	}

	// Determine target shadow database
	targetDB := e.determineTargetDB(query)
	if targetDB == "" {
		return types.EngineResult{Continue: true}
	}

	// Store shadow metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["shadow_enabled"] = true
	qc.Metadata["shadow_mode"] = e.config.Mode
	qc.Metadata["shadow_target"] = targetDB
	qc.Metadata["shadow_query"] = query

	e.stats.mu.Lock()
	e.stats.QueriesMirrored++
	e.stats.mu.Unlock()

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles shadow response
func (e *ShadowDatabaseEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil && qc.Response.Duration > 0 {
		e.stats.mu.Lock()
		latency := float64(qc.Response.Duration.Milliseconds())
		count := e.stats.QueriesMirrored
		e.stats.AvgLatencyMs = (e.stats.AvgLatencyMs*float64(count-1) + latency) / float64(count)
		e.stats.mu.Unlock()
	}
	return types.EngineResult{Continue: true}
}

// detectOperation determines query operation type
func (e *ShadowDatabaseEngine) detectOperation(query string) string {
	if strings.HasPrefix(query, "SELECT") {
		return "SELECT"
	}
	if strings.HasPrefix(query, "INSERT") {
		return "INSERT"
	}
	if strings.HasPrefix(query, "UPDATE") {
		return "UPDATE"
	}
	if strings.HasPrefix(query, "DELETE") {
		return "DELETE"
	}
	return ""
}

// isOperationEnabled checks if operation should be shadowed
func (e *ShadowDatabaseEngine) isOperationEnabled(operation string) bool {
	for _, op := range e.config.EnabledOps {
		if op == operation {
			return true
		}
	}
	return false
}

// determineTargetDB determines which shadow database to use
func (e *ShadowDatabaseEngine) determineTargetDB(query string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check match rules first
	for _, rule := range e.matchRules {
		if !rule.Enabled {
			continue
		}
		if strings.Contains(query, rule.Pattern) {
			return rule.TargetDB
		}
	}

	// Default to first available target
	for name := range e.targetDBs {
		return name
	}

	return ""
}

// AddMatchRule adds a shadow matching rule
func (e *ShadowDatabaseEngine) AddMatchRule(rule *ShadowMatchRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.matchRules = append(e.matchRules, rule)
}

// GetShadowStats returns shadow statistics
func (e *ShadowDatabaseEngine) GetShadowStats() ShadowStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return ShadowStatsResponse{
		QueriesMirrored:  e.stats.QueriesMirrored,
		QueriesDiffed:    e.stats.QueriesDiffed,
		QueriesReplayed:  e.stats.QueriesReplayed,
		ShadowErrors:     e.stats.ShadowErrors,
		AvgLatencyMs:     e.stats.AvgLatencyMs,
	}
}

// GetTargetDatabases returns all shadow databases
func (e *ShadowDatabaseEngine) GetTargetDatabases() []ShadowDatabase {
	e.mu.RLock()
	defer e.mu.RUnlock()

	dbs := make([]ShadowDatabase, 0, len(e.targetDBs))
	for _, db := range e.targetDBs {
		dbs = append(dbs, *db)
	}
	return dbs
}

type ShadowStatsResponse struct {
	QueriesMirrored int64   `json:"queries_mirrored"`
	QueriesDiffed   int64   `json:"queries_diffed"`
	QueriesReplayed int64   `json:"queries_replayed"`
	ShadowErrors    int64   `json:"shadow_errors"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}