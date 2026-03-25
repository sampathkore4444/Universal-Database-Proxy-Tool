package engines

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// QueryHistoryEngine provides long-term storage and analysis of query patterns
type QueryHistoryEngine struct {
	BaseEngine
	config      *HistoryConfig
	history     []QueryRecord
	index       map[string][]int
	stats       *HistoryStats
	mu          sync.RWMutex
}

type HistoryConfig struct {
	Enabled        bool
	MaxRecords     int
	RetentionDays  int
	StorageType    string // memory, file, database
	IndexEnabled   bool
	AggregateStats bool
}

type QueryRecord struct {
	ID         string
	Query      string
	User       string
	Database   string
	Timestamp  time.Time
	Duration   time.Duration
	Rows       int64
	Status     string
	Hash       string
	Metadata   map[string]interface{}
}

type HistoryStats struct {
	RecordsStored  int64
	QueriesRetrieved int64
	StorageUsedMB  float64
	mu             sync.RWMutex
}

// NewQueryHistoryEngine creates a new Query History Engine
func NewQueryHistoryEngine(config *HistoryConfig) *QueryHistoryEngine {
	if config == nil {
		config = &HistoryConfig{
			Enabled:     false,
			MaxRecords:  100000,
			RetentionDays: 30,
			StorageType: "memory",
			IndexEnabled: true,
		}
	}

	engine := &QueryHistoryEngine{
		BaseEngine: BaseEngine{name: "query_history"},
		config:     config,
		history:    make([]QueryRecord, 0, config.MaxRecords),
		index:      make(map[string][]int),
		stats:      &HistoryStats{},
	}

	return engine
}

// Process stores query in history
func (e *QueryHistoryEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := qc.RawQuery
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	hash := e.hashQuery(query)

	record := QueryRecord{
		ID:        qc.ID,
		Query:     query,
		User:      qc.User,
		Database:  qc.Database,
		Timestamp: time.Now(),
		Duration:  0,
		Rows:      0,
		Status:    "executed",
		Hash:      hash,
		Metadata:  qc.Metadata,
	}

	e.mu.Lock()
	
	// Add record
	e.history = append(e.history, record)
	
	// Update index
	if e.config.IndexEnabled {
		e.index[hash] = append(e.index[hash], len(e.history)-1)
	}

	// Trim old records if needed
	if len(e.history) > e.config.MaxRecords {
		e.history = e.history[len(e.history)-e.config.MaxRecords:]
	}

	e.stats.mu.Lock()
	e.stats.RecordsStored = int64(len(e.history))
	e.stats.StorageUsedMB = float64(len(e.history) * 200 / 1024 / 1024)
	e.stats.mu.Unlock()

	e.mu.Unlock()

	return types.EngineResult{Continue: true}
}

// ProcessResponse updates record with response data
func (e *QueryHistoryEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled || qc.Response == nil {
		return types.EngineResult{Continue: true}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Update last record with response data
	if len(e.history) > 0 {
		last := &e.history[len(e.history)-1]
		last.Duration = qc.Response.Duration
		last.Rows = qc.Response.RowsReturned
		if qc.Response.Error != nil {
			last.Status = "failed"
		}
	}

	return types.EngineResult{Continue: true}
}

// hashQuery creates a hash for query
func (e *QueryHistoryEngine) hashQuery(query string) string {
	hash := 0
	for _, c := range []byte(query) {
		hash = hash*31 + int(c)
	}
	return fmt.Sprintf("%x", hash)
}

// QueryHistory searches history for queries matching pattern
func (e *QueryHistoryEngine) QueryHistory(pattern string) []QueryRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()

	e.stats.mu.Lock()
	e.stats.QueriesRetrieved++
	e.stats.mu.Unlock()

	var results []QueryRecord
	
	for _, record := range e.history {
		if strings.Contains(strings.ToLower(record.Query), strings.ToLower(pattern)) {
			results = append(results, record)
		}
	}

	return results
}

// GetTopQueries returns most frequent queries
func (e *QueryHistoryEngine) GetTopQueries(limit int) []QueryFrequency {
	e.mu.RLock()
	defer e.mu.RUnlock()

	frequency := make(map[string]int)
	
	for _, record := range e.history {
		frequency[record.Query]++
	}

	type QueryFrequency struct {
		Query string
		Count int
	}

	results := make([]QueryFrequency, 0, limit)
	for query, count := range frequency {
		results = append(results, QueryFrequency{Query: query, Count: count})
	}

	// Sort by count descending
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Count > results[i].Count {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

// GetHistoryStats returns history statistics
func (e *QueryHistoryEngine) GetHistoryStats() HistoryStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	e.mu.RLock()
	totalRecords := len(e.history)
	e.mu.RUnlock()

	return HistoryStatsResponse{
		RecordsStored:    e.stats.RecordsStored,
		QueriesRetrieved: e.stats.QueriesRetrieved,
		StorageUsedMB:   e.stats.StorageUsedMB,
		TotalRecords:    int64(totalRecords),
	}
}

type QueryFrequency struct {
	Query string
	Count int
}

type HistoryStatsResponse struct {
	RecordsStored     int64   `json:"records_stored"`
	QueriesRetrieved   int64   `json:"queries_retrieved"`
	StorageUsedMB     float64 `json:"storage_used_mb"`
	TotalRecords      int64   `json:"total_records"`
}