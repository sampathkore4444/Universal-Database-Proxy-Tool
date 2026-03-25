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

// DataLineageEngine provides data flow tracking and impact analysis
type DataLineageEngine struct {
	BaseEngine
	config          *LineageConfig
	lineageGraph    *LineageGraph
	tableAccessLog  *TableAccessLog
	queryFlows      []DataFlow
	impactAnalysis  map[string]*ImpactResult
	mu              sync.RWMutex
}

type LineageConfig struct {
	Enabled            bool          // Enable the engine
	TrackReadAccess    bool          // Track which queries read from tables
	TrackWriteAccess   bool          // Track which queries write to tables
	BuildDependencyGraph bool        // Build table dependency graph
	ImpactAnalysisEnabled bool       // Enable impact analysis
	MaxFlowHistory     int           // Maximum flow history entries
	RetentionPeriod    time.Duration // How long to keep lineage data
}

type LineageGraph struct {
	nodes map[string]*TableNode
	edges map[string][]string // table -> dependent tables
	mu    sync.RWMutex
}

type TableNode struct {
	TableName      string
	Columns        []string
	ReadCount      int64
	WriteCount     int64
	LastRead       time.Time
	LastWrite      time.Time
	Dependencies   []string
	Dependents     []string
}

type TableAccessLog struct {
	entries  []AccessEntry
	maxSize  int
	mu       sync.RWMutex
}

type AccessEntry struct {
	Timestamp   time.Time
	QueryID     string
	Query       string
	TableName   string
	AccessType  AccessType // READ, WRITE
	Columns     []string
	ClientID    string
	Database    string
}

type AccessType string

const (
	AccessRead AccessType = "READ"
	AccessWrite AccessType = "WRITE"
)

type DataFlow struct {
	FlowID       string
	Timestamp    time.Time
	SourceTable  string
	TargetTable  string
	Query        string
	QueryType    string
	Columns      []string
	IsDirect     bool
	Path         []string // For indirect flows
}

type ImpactResult struct {
	TableName       string
	AffectedBy      []string
	Impacts         []string
	Depth           int
	CascadeDepth    int
	CriticalTables  []string
	AffectedQueries []string
	EstimatedImpact ImpactLevel
}

type ImpactLevel string

const (
	ImpactCritical ImpactLevel = "CRITICAL"
	ImpactHigh     ImpactLevel = "HIGH"
	ImpactMedium   ImpactLevel = "MEDIUM"
	ImpactLow      ImpactLevel = "LOW"
	ImpactNone     ImpactLevel = "NONE"
)

// NewDataLineageEngine creates a new Data Lineage Engine
func NewDataLineageEngine(config *LineageConfig) *DataLineageEngine {
	if config == nil {
		config = &LineageConfig{
			Enabled:             true,
			TrackReadAccess:    true,
			TrackWriteAccess:   true,
			BuildDependencyGraph: true,
			ImpactAnalysisEnabled: true,
			MaxFlowHistory:     1000,
			RetentionPeriod:    24 * time.Hour,
		}
	}

	engine := &DataLineageEngine{
		BaseEngine:     BaseEngine{name: "data_lineage"},
		config:         config,
		lineageGraph:   &LineageGraph{nodes: make(map[string]*TableNode), edges: make(map[string][]string)},
		tableAccessLog: &AccessLog{maxSize: 10000},
		queryFlows:    make([]DataFlow, 0),
		impactAnalysis: make(map[string]*ImpactResult),
	}

	// Start cleanup loop
	go engine.cleanupLoop()

	return config
}

// Process handles data lineage tracking
func (e *DataLineageEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.Query)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	upper := strings.ToUpper(query)
	tables := e.extractTables(query)
	isWrite := e.isWriteQuery(query)

	// Track access for each table
	for _, table := range tables {
		accessType := AccessRead
		if isWrite {
			accessType = AccessWrite
		}

		columns := e.extractColumnsFromQuery(query, table)

		// Log access
		e.tableAccessLog.mu.Lock()
		e.tableAccessLog.entries = append(e.tableAccessLog.entries, AccessEntry{
			Timestamp:   time.Now(),
			QueryID:     qc.QueryID,
			Query:       query,
			TableName:   table,
			AccessType:  accessType,
			Columns:     columns,
			ClientID:    qc.ClientInfo.ClientID,
			Database:    qc.Database,
		})
		// Keep bounded
		if len(e.tableAccessLog.entries) > e.tableAccessLog.maxSize {
			e.tableAccessLog.entries = e.tableAccessLog.entries[1:]
		}
		e.tableAccessLog.mu.Unlock()

		// Update lineage graph
		e.updateLineageGraph(table, accessType, columns)

		// Create data flows for write queries
		if isWrite && e.config.TrackWriteAccess {
			e.createDataFlow(qc, table, tables, columns)
		}

		// Build dependency relationships
		if e.config.BuildDependencyGraph && isWrite {
			e.addDependency(tables[0], tables[1:]...)
		}
	}

	// Add lineage info to metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["tables_accessed"] = tables
	qc.Metadata["is_write"] = isWrite

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles response processing
func (e *DataLineageEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// extractTables extracts table names from query
func (e *DataLineageEngine) extractTables(query string) []string {
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

// extractColumnsFromQuery extracts columns accessed in query for a specific table
func (e *DataLineageEngine) extractColumnsFromQuery(query, table string) []string {
	re := regexp.MustCompile(fmt.Sprintf(`(?i)%s\.(\w+)`, table))
	matches := re.FindAllStringSubmatch(query, -1)

	columns := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			columns[match[1]] = true
		}
	}

	result := make([]string, 0, len(columns))
	for c := range columns {
		result = append(result, c)
	}
	return result
}

// isWriteQuery determines if query modifies data
func (e *DataLineageEngine) isWriteQuery(query string) bool {
	writeTypes := []string{"INSERT", "UPDATE", "DELETE", "CREATE", "ALTER", "DROP", "TRUNCATE", "REPLACE"}
	upper := strings.ToUpper(query)
	for _, wt := range writeTypes {
		if strings.HasPrefix(upper, wt) {
			return true
		}
	}
	return false
}

// updateLineageGraph updates the lineage graph with access information
func (e *DataLineageEngine) updateLineageGraph(table string, accessType AccessType, columns []string) {
	e.lineageGraph.mu.Lock()
	defer e.lineageGraph.mu.Unlock()

	node, exists := e.lineageGraph.nodes[table]
	if !exists {
		node = &TableNode{
			TableName: table,
			Columns:   columns,
		}
		e.lineageGraph.nodes[table] = node
	}

	if accessType == AccessRead {
		node.ReadCount++
		node.LastRead = time.Now()
	} else {
		node.WriteCount++
		node.LastWrite = time.Now()
	}

	// Add new columns if discovered
	for _, col := range columns {
		found := false
		for _, existing := range node.Columns {
			if existing == col {
				found = true
				break
			}
		}
		if !found {
			node.Columns = append(node.Columns, col)
		}
	}
}

// addDependency adds a dependency relationship between tables
func (e *DataLineageEngine) addDependency(source string, targets ...string) {
	e.lineageGraph.mu.Lock()
	defer e.lineageGraph.mu.Unlock()

	// Update source node
	if sourceNode, exists := e.lineageGraph.nodes[source]; exists {
		for _, target := range targets {
			found := false
			for _, dep := range sourceNode.Dependencies {
				if dep == target {
					found = true
					break
				}
			}
			if !found {
				sourceNode.Dependencies = append(sourceNode.Dependencies, target)
			}
		}
	}

	// Update edges
	for _, target := range targets {
		if _, exists := e.lineageGraph.edges[source]; !exists {
			e.lineageGraph.edges[source] = make([]string, 0)
		}
		found := false
		for _, t := range e.lineageGraph.edges[source] {
			if t == target {
				found = true
				break
			}
		}
		if !found {
			e.lineageGraph.edges[source] = append(e.lineageGraph.edges[source], target)
		}

		// Update target's dependents
		if targetNode, exists := e.lineageGraph.nodes[target]; exists {
			found = false
			for _, dep := range targetNode.Dependents {
				if dep == source {
					found = true
					break
				}
			}
			if !found {
				targetNode.Dependents = append(targetNode.Dependents, source)
			}
		}
	}
}

// createDataFlow creates a data flow record
func (e *DataLineageEngine) createDataFlow(qc *types.QueryContext, sourceTable string, allTables []string, columns []string) {
	flow := DataFlow{
		FlowID:      fmt.Sprintf("flow_%d", time.Now().UnixNano()),
		Timestamp:   time.Now(),
		SourceTable: sourceTable,
		TargetTable: "",
		Query:       qc.Query,
		QueryType:   e.detectQueryType(qc.Query),
		Columns:     columns,
		IsDirect:   true,
	}

	// If source is also read from other tables, that's the indirect source
	if len(allTables) > 1 {
		flow.SourceTable = allTables[1] // Second table is typically the source for JOINs
		flow.TargetTable = allTables[0] // First table is typically the target
		flow.IsDirect = len(allTables) == 2

		// Build path for indirect flows
		if !flow.IsDirect {
			flow.Path = allTables
		}
	}

	e.mu.Lock()
	e.queryFlows = append(e.queryFlows, flow)
	if len(e.queryFlows) > e.config.MaxFlowHistory {
		e.queryFlows = e.queryFlows[1:]
	}
	e.mu.Unlock()
}

// detectQueryType determines query type
func (e *DataLineageEngine) detectQueryType(query string) string {
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
	return "OTHER"
}

// cleanupLoop performs periodic cleanup
func (e *DataLineageEngine) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		e.tableAccessLog.mu.Lock()
		cutoff := time.Now().Add(-e.config.RetentionPeriod)
		newEntries := make([]AccessEntry, 0)
		for _, entry := range e.tableAccessLog.entries {
			if entry.Timestamp.After(cutoff) {
				newEntries = append(newEntries, entry)
			}
		}
		e.tableAccessLog.entries = newEntries
		e.tableAccessLog.mu.Unlock()
	}
}

// AnalyzeImpact performs impact analysis for a table
func (e *DataLineageEngine) AnalyzeImpact(tableName string) ImpactResult {
	e.lineageGraph.mu.RLock()
	defer e.lineageGraph.mu.RUnlock()

	result := ImpactResult{
		TableName:      tableName,
		AffectedBy:     make([]string, 0),
		Impacts:        make([]string, 0),
		CascadeDepth:   0,
		CriticalTables: make([]string, 0),
	}

	// Find what this table depends on
	if node, exists := e.lineageGraph.nodes[tableName]; exists {
		result.AffectedBy = node.Dependencies

		// Find what depends on this table (cascade)
		result.Impacts = node.Dependents

		// Calculate cascade depth
		result.CascadeDepth = e.calculateCascadeDepth(tableName, make(map[string]bool), 0)

		// Identify critical tables (high write count)
		for _, t := range e.lineageGraph.nodes {
			if t.WriteCount > 100 {
				result.CriticalTables = append(result.CriticalTables, t.TableName)
			}
		}

		// Determine impact level
		if len(result.Impacts) > 10 {
			result.EstimatedImpact = ImpactCritical
		} else if len(result.Impacts) > 5 {
			result.EstimatedImpact = ImpactHigh
		} else if len(result.Impacts) > 0 {
			result.EstimatedImpact = ImpactMedium
		} else {
			result.EstimatedImpact = ImpactLow
		}
	}

	return result
}

// calculateCascadeDepth calculates how deep the dependency chain goes
func (e *DataLineageEngine) calculateCascadeDepth(table string, visited map[string]bool, depth int) int {
	if visited[table] {
		return depth
	}
	visited[table] = true

	e.lineageGraph.mu.RLock()
	node, exists := e.lineageGraph.nodes[table]
	e.lineageGraph.mu.RUnlock()

	if !exists {
		return depth
	}

	maxDepth := depth
	for _, dependent := range node.Dependents {
		d := e.calculateCascadeDepth(dependent, visited, depth+1)
		if d > maxDepth {
			maxDepth = d
		}
	}

	return maxDepth
}

// GetTableLineage returns lineage information for a table
func (e *DataLineageEngine) GetTableLineage(tableName string) (TableLineageResponse, bool) {
	e.lineageGraph.mu.RLock()
	defer e.lineageGraph.mu.RUnlock()

	node, exists := e.lineageGraph.nodes[tableName]
	if !exists {
		return TableLineageResponse{}, false
	}

	return TableLineageResponse{
		TableName:   node.TableName,
		Columns:     node.Columns,
		ReadCount:   node.ReadCount,
		WriteCount:  node.WriteCount,
		Dependencies: node.Dependencies,
		Dependents:  node.Dependents,
		LastRead:    node.LastRead,
		LastWrite:   node.LastWrite,
	}, true
}

// GetDataFlows returns recent data flows
func (e *DataLineageEngine) GetDataFlows(limit int) []DataFlowResponse {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit > len(e.queryFlows) {
		limit = len(e.queryFlows)
	}

	result := make([]DataFlowResponse, 0, limit)
	start := len(e.queryFlows) - limit
	for i := start; i < len(e.queryFlows); i++ {
		f := e.queryFlows[i]
		result = append(result, DataFlowResponse{
			FlowID:      f.FlowID,
			Timestamp:   f.Timestamp,
			SourceTable: f.SourceTable,
			TargetTable: f.TargetTable,
			QueryType:   f.QueryType,
			Columns:     f.Columns,
			IsDirect:    f.IsDirect,
		})
	}

	return result
}

// GetAccessHistory returns table access history
func (e *DataLineageEngine) GetAccessHistory(tableName string, limit int) []AccessEntryResponse {
	e.tableAccessLog.mu.RLock()
	defer e.tableAccessLog.mu.RUnlock()

	result := make([]AccessEntryResponse, 0)
	count := 0

	// Go backwards to get most recent first
	for i := len(e.tableAccessLog.entries) - 1; i >= 0 && count < limit; i-- {
		entry := e.tableAccessLog.entries[i]
		if entry.TableName == tableName || tableName == "" {
			result = append(result, AccessEntryResponse{
				Timestamp:  entry.Timestamp,
				QueryID:     entry.QueryID,
				Query:       entry.Query,
				TableName:   entry.TableName,
				AccessType:  string(entry.AccessType),
				Columns:     entry.Columns,
				ClientID:    entry.ClientID,
			})
			count++
		}
	}

	return result
}

// Helper types for API responses
type TableLineageResponse struct {
	TableName    string    `json:"table_name"`
	Columns      []string  `json:"columns"`
	ReadCount    int64     `json:"read_count"`
	WriteCount   int64     `json:"write_count"`
	Dependencies []string  `json:"dependencies"`
	Dependents   []string  `json:"dependents"`
	LastRead     time.Time `json:"last_read"`
	LastWrite    time.Time `json:"last_write"`
}

type DataFlowResponse struct {
	FlowID      string    `json:"flow_id"`
	Timestamp   time.Time `json:"timestamp"`
	SourceTable string    `json:"source_table"`
	TargetTable string    `json:"target_table"`
	QueryType   string    `json:"query_type"`
	Columns     []string  `json:"columns"`
	IsDirect    bool      `json:"is_direct"`
}

type AccessEntryResponse struct {
	Timestamp time.Time `json:"timestamp"`
	QueryID   string    `json:"query_id"`
	Query     string    `json:"query"`
	TableName string    `json:"table_name"`
	AccessType string   `json:"access_type"`
	Columns   []string  `json:"columns"`
	ClientID  string    `json:"client_id"`
}