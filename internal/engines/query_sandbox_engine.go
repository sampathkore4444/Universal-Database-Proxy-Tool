package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// QuerySandboxEngine provides safe query execution with preview mode and rollback simulation
type QuerySandboxEngine struct {
	BaseEngine
	config       *SandboxConfig
	sandboxQueries map[string]*SandboxSession
	queryHistory  []SandboxLogEntry
	mu           sync.RWMutex
}

type SandboxConfig struct {
	Enabled          bool          // Enable the engine
	PreviewMode      bool          // Enable preview mode for write queries
	MaxPreviewRows   int           // Maximum rows to show in preview
	SimulationEnabled bool         // Enable rollback simulation
	SandboxTimeout   time.Duration // Timeout for sandbox queries
	BlockedPatterns []string      // Regex patterns to block
	AllowReadOnly   bool           // Allow read-only queries in sandbox
}

type SandboxSession struct {
	SessionID    string
	Query        string
	QueryType    string // SELECT, INSERT, UPDATE, DELETE, etc.
	Tables       []string
	PreviewRows  [][]interface{}
	AffectedRows int
	StartTime    time.Time
	Status       SandboxStatus // PENDING, EXECUTED, PREVIEW, BLOCKED
	CanRollback  bool
	OriginalData []byte // For rollback simulation
}

type SandboxStatus int

const (
	SandboxPending SandboxStatus = iota
	SandboxExecuted
	SandboxPreview
	SandboxBlocked
	SandboxSimulated
)

type SandboxLogEntry struct {
	SessionID   string
	Query       string
	QueryType   string
	Tables      []string
	Timestamp   time.Time
	Status      SandboxStatus
	AffectedRows int
	PreviewRows int
	IsWrite     bool
}

// NewQuerySandboxEngine creates a new Query Sandbox Engine
func NewQuerySandboxEngine(config *SandboxConfig) *QuerySandboxEngine {
	if config == nil {
		config = &SandboxConfig{
			Enabled:           true,
			PreviewMode:       true,
			MaxPreviewRows:   10,
			SimulationEnabled: true,
			SandboxTimeout:    30 * time.Second,
			BlockedPatterns:   []string{},
			AllowReadOnly:     true,
		}
	}

	engine := &QuerySandboxEngine{
		BaseEngine:     BaseEngine{name: "query_sandbox"},
		config:         config,
		sandboxQueries: make(map[string]*SandboxSession),
		queryHistory:   make([]SandboxLogEntry, 0),
	}

	// Add default blocked patterns
	if len(config.BlockedPatterns) == 0 {
		config.BlockedPatterns = []string{
			`(?i)DROP\s+DATABASE`,
			`(?i)TRUNCATE\s+\w+`,
			`(?i)GRANT\s+`,
			`(?i)REVOKE\s+`,
			`(?i)SHUTDOWN`,
			`(?i)KILL\s+\d`,
		}
	}

	return engine
}

// Process handles sandbox logic for queries
func (e *QuerySandboxEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.Query)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	queryType := e.detectQueryType(query)
	isWrite := e.isWriteQuery(query)

	// Create sandbox session
	session := &SandboxSession{
		SessionID: generateSessionID(),
		Query:     query,
		QueryType: queryType,
		Tables:    e.extractTables(query),
		StartTime: time.Now(),
		Status:    SandboxPending,
	}

	// Check blocked patterns
	if e.isBlocked(query) {
		session.Status = SandboxBlocked
		e.logSession(session)

		return types.EngineResult{
			Continue: false,
			Error:    fmt.Errorf("query blocked by sandbox: dangerous pattern detected"),
			Metadata: map[string]interface{}{
				"sandbox_status": "blocked",
				"session_id":     session.SessionID,
				"reason":        "blocked_pattern",
			},
		}
	}

	// Handle write queries based on preview mode
	if isWrite && e.config.PreviewMode {
		session.Status = SandboxPreview
		e.logSession(session)

		// Add preview info to metadata - let the query pass but mark for preview
		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["sandbox_enabled"] = true
		qc.Metadata["sandbox_session_id"] = session.SessionID
		qc.Metadata["sandbox_status"] = "preview_required"
		qc.Metadata["preview_rows"] = e.config.MaxPreviewRows
		qc.Metadata["query_type"] = queryType

		// Store session for later retrieval
		e.mu.Lock()
		e.sandboxQueries[session.SessionID] = session
		e.mu.Unlock()
	} else if !isWrite && !e.config.AllowReadOnly {
		// Block read-only if configured
		e.logSession(session)
		return types.EngineResult{Continue: true} // Allow
	}

	e.logSession(session)

	// Store session
	e.mu.Lock()
	e.sandboxQueries[session.SessionID] = session
	e.mu.Unlock()

	// Add sandbox info to metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["sandbox_enabled"] = true
	qc.Metadata["sandbox_session_id"] = session.SessionID
	qc.Metadata["is_write_query"] = isWrite
	qc.Metadata["query_type"] = queryType

	return types.EngineResult{Continue: true}
}

// ProcessResponse captures query results for sandbox tracking
func (e *QuerySandboxEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	sessionID, exists := qc.Metadata["sandbox_session_id"].(string)
	if !exists {
		return types.EngineResult{Continue: true}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	session, exists := e.sandboxQueries[sessionID]
	if !exists {
		return types.EngineResult{Continue: true}
	}

	session.Status = SandboxExecuted
	session.AffectedRows = qc.RowsAffected

	// Store preview data if this was a preview
	if qc.Metadata != nil {
		if previewData, ok := qc.Metadata["preview_rows"].([][]interface{}); ok {
			session.PreviewRows = previewData
			if len(previewData) > e.config.MaxPreviewRows {
				session.PreviewRows = previewData[:e.config.MaxPreviewRows]
			}
		}
	}

	return types.EngineResult{Continue: true}
}

// detectQueryType determines the type of SQL query
func (e *QuerySandboxEngine) detectQueryType(query string) string {
	upper := strings.ToUpper(query)

	if strings.HasPrefix(upper, "SELECT") {
		return "SELECT"
	}
	if strings.HasPrefix(upper, "INSERT") {
		return "INSERT"
	}
	if strings.HasPrefix(upper, "UPDATE") {
		return "UPDATE"
	}
	if strings.HasPrefix(upper, "DELETE") {
		return "DELETE"
	}
	if strings.HasPrefix(upper, "CREATE") {
		return "CREATE"
	}
	if strings.HasPrefix(upper, "ALTER") {
		return "ALTER"
	}
	if strings.HasPrefix(upper, "DROP") {
		return "DROP"
	}
	if strings.HasPrefix(upper, "TRUNCATE") {
		return "TRUNCATE"
	}
	if strings.HasPrefix(upper, "REPLACE") {
		return "REPLACE"
	}
	if strings.HasPrefix(upper, "MERGE") {
		return "MERGE"
	}

	return "UNKNOWN"
}

// isWriteQuery determines if query modifies data
func (e *QuerySandboxEngine) isWriteQuery(query string) bool {
	writeTypes := []string{"INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE", "CREATE", "ALTER", "DROP", "TRUNCATE"}
	upper := strings.ToUpper(query)
	for _, wt := range writeTypes {
		if strings.HasPrefix(upper, wt) {
			return true
		}
	}
	return false
}

// extractTables extracts table names from query
func (e *QuerySandboxEngine) extractTables(query string) []string {
	patterns := []string{
		`(?i)FROM\s+(\w+)`,
		`(?i)JOIN\s+(\w+)`,
		`(?i)INTO\s+(\w+)`,
		`(?i)UPDATE\s+(\w+)`,
		`(?i)TABLE\s+(\w+)`,
	}

	tables := make(map[string]bool)
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllStringSubmatch(query, -1)
		for _, match := range matches {
			if len(match) > 1 {
				tables[strings.ToLower(match[1])] = true
			}
		}
	}

	result := make([]string, 0, len(tables))
	for t := range tables {
		result = append(result, t)
	}
	return result
}

// isBlocked checks if query matches blocked patterns
func (e *QuerySandboxEngine) isBlocked(query string) bool {
	for _, pattern := range e.config.BlockedPatterns {
		re := regexp.MustCompile(pattern)
		if re.MatchString(query) {
			return true
		}
	}
	return false
}

// logSession logs sandbox session for audit
func (e *QuerySandboxEngine) logSession(session *SandboxSession) {
	entry := SandboxLogEntry{
		SessionID:    session.SessionID,
		Query:        session.Query,
		QueryType:    session.QueryType,
		Tables:       session.Tables,
		Timestamp:    time.Now(),
		Status:       session.Status,
		AffectedRows: session.AffectedRows,
		PreviewRows:  len(session.PreviewRows),
		IsWrite:      e.isWriteQuery(session.Query),
	}

	e.mu.Lock()
	e.queryHistory = append(e.queryHistory, entry)
	// Keep only last 1000 entries
	if len(e.queryHistory) > 1000 {
		e.queryHistory = e.queryHistory[1:]
	}
	e.mu.Unlock()
}

// SimulateRollback simulates what would happen if we roll back this query
func (e *QuerySandboxEngine) SimulateRollback(sessionID string) (RollbackSimulation, error) {
	e.mu.RLock()
	session, exists := e.sandboxQueries[sessionID]
	e.mu.RUnlock()

	if !exists {
		return RollbackSimulation{}, fmt.Errorf("session not found: %s", sessionID)
	}

	if !session.CanRollback {
		return RollbackSimulation{}, fmt.Errorf("rollback not available for this query")
	}

	simulation := RollbackSimulation{
		SessionID:       sessionID,
		Query:           session.Query,
		QueryType:       session.QueryType,
		Tables:          session.Tables,
		AffectedRows:    session.AffectedRows,
		SimulationTime:  time.Now(),
		RollbackPossible: true,
		ImpactLevel:     e.estimateImpact(session),
	}

	return simulation, nil
}

// estimateImpact estimates the impact level of a query
func (e *QuerySandboxEngine) estimateImpact(session *SandboxSession) string {
	if session.AffectedRows == 0 {
		return "NONE"
	}
	if session.AffectedRows < 10 {
		return "LOW"
	}
	if session.AffectedRows < 100 {
		return "MEDIUM"
	}
	return "HIGH"
}

// GetSession returns sandbox session by ID
func (e *QuerySandboxEngine) GetSession(sessionID string) (SandboxSessionResponse, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	session, exists := e.sandboxQueries[sessionID]
	if !exists {
		return SandboxSessionResponse{}, false
	}

	return SandboxSessionResponse{
		SessionID:    session.SessionID,
		Query:        session.Query,
		QueryType:    session.QueryType,
		Tables:       session.Tables,
		Status:       session.Status.String(),
		AffectedRows: session.AffectedRows,
		PreviewRows:  len(session.PreviewRows),
		StartTime:    session.StartTime,
	}, true
}

// GetHistory returns recent sandbox history
func (e *QuerySandboxEngine) GetHistory(limit int) []SandboxLogEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit > len(e.queryHistory) {
		limit = len(e.queryHistory)
	}

	result := make([]SandboxLogEntry, limit)
	copy(result, e.queryHistory[len(e.queryHistory)-limit:])
	return result
}

// EnableSimulation enables rollback simulation for a session
func (e *QuerySandboxEngine) EnableSimulation(sessionID string, originalData []byte) error {
	if !e.config.SimulationEnabled {
		return fmt.Errorf("simulation is not enabled")
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	session, exists := e.sandboxQueries[sessionID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	session.CanRollback = true
	session.OriginalData = originalData
	session.Status = SandboxSimulated

	return nil
}

// generateSessionID creates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("sandbox_%d_%d", time.Now().UnixNano(), time.Now().Nanosecond()%1000)
}

// Helper types for API responses
type SandboxSessionResponse struct {
	SessionID    string    `json:"session_id"`
	Query        string    `json:"query"`
	QueryType    string    `json:"query_type"`
	Tables       []string  `json:"tables"`
	Status       string    `json:"status"`
	AffectedRows int       `json:"affected_rows"`
	PreviewRows  int       `json:"preview_rows"`
	StartTime    time.Time `json:"start_time"`
}

type RollbackSimulation struct {
	SessionID        string    `json:"session_id"`
	Query            string    `json:"query"`
	QueryType        string    `json:"query_type"`
	Tables           []string  `json:"tables"`
	AffectedRows     int       `json:"affected_rows"`
	SimulationTime   time.Time `json:"simulation_time"`
	RollbackPossible bool      `json:"rollback_possible"`
	ImpactLevel      string    `json:"impact_level"`
}

// String returns string representation of SandboxStatus
func (s SandboxStatus) String() string {
	switch s {
	case SandboxPending:
		return "pending"
	case SandboxExecuted:
		return "executed"
	case SandboxPreview:
		return "preview"
	case SandboxBlocked:
		return "blocked"
	case SandboxSimulated:
		return "simulated"
	default:
		return "unknown"
	}
}