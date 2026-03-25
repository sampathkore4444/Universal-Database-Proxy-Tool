package engines

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// BatchProcessingEngine optimizes bulk operations with batch hints
type BatchProcessingEngine struct {
	BaseEngine
	config      *BatchConfig
	batchGroups map[string]*BatchGroup
	stats       *BatchStats
	mu          sync.RWMutex
}

type BatchConfig struct {
	Enabled          bool
	MaxBatchSize     int
	BatchTimeoutMs   int
	AutoBatch        bool
	BatchTableHints  map[string]string // table -> batch hint
}

type BatchGroup struct {
	Table       string
	Operation   string
	Queries     []string
	TotalRows   int64
	CreatedAt   time.Time
}

type BatchStats struct {
	BatchesCreated   int64
	QueriesBatched   int64
	RowsOptimized    int64
	AvgBatchSize     float64
	mu               sync.RWMutex
}

// NewBatchProcessingEngine creates a new Batch Processing Engine
func NewBatchProcessingEngine(config *BatchConfig) *BatchBatchEngine {
	if config == nil {
		config = &BatchConfig{
			Enabled:        false,
			MaxBatchSize:   1000,
			BatchTimeoutMs: 5000,
			AutoBatch:     true,
		}
	}

	engine := &BatchProcessingEngine{
		BaseEngine:  BaseEngine{name: "batch_processing"},
		config:      config,
		batchGroups: make(map[string]*BatchGroup),
		stats:       &BatchStats{},
	}

	return engine
}

// Process handles batch optimization
func (e *BatchProcessingEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	upperQuery := strings.ToUpper(query)
	
	// Only batch INSERT, UPDATE, DELETE
	if !strings.HasPrefix(upperQuery, "INSERT") && 
	   !strings.HasPrefix(upperQuery, "UPDATE") &&
	   !strings.HasPrefix(upperQuery, "DELETE") {
		return types.EngineResult{Continue: true}
	}

	// Extract table
	table := e.extractTable(query)
	if table == "" {
		return types.EngineResult{Continue: true}
	}

	// Check if this is a batch query
	isBatch := e.isBatchQuery(query)
	
	// Add batch hints if applicable
	if e.config.AutoBatch && table != "" {
		if hint, ok := e.config.BatchTableHints[table]; ok {
			if !strings.Contains(upperQuery, hint) {
				query = query + " " + hint
				qc.RawQuery = query
				
				e.stats.mu.Lock()
				e.stats.BatchesCreated++
				e.stats.mu.Unlock()
			}
		}
	}

	// Estimate rows for optimization
	rows := e.estimateRows(query)
	if rows > 0 {
		e.stats.mu.Lock()
		e.stats.QueriesBatched++
		e.stats.RowsOptimized += rows
		e.stats.AvgBatchSize = (e.stats.AvgBatchSize*float64(e.stats.QueriesBatched-1) + float64(rows)) / float64(e.stats.QueriesBatched)
		e.stats.mu.Unlock()
	}

	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["batch_optimized"] = true
	qc.Metadata["estimated_rows"] = rows

	return types.EngineResult{Continue: true}
}

// ProcessResponse processes batch response
func (e *BatchProcessingEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// extractTable extracts table name from query
func (e *BatchProcessingEngine) extractTable(query string) string {
	re := regexp.MustCompile(`(?i)(?:INTO|UPDATE|FROM)\s+(\w+)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// isBatchQuery determines if query contains multiple values
func (e *BatchProcessingEngine) isBatchQuery(query string) bool {
	// Multiple VALUES in INSERT
	re := regexp.MustCompile(`(?i)VALUES\s*\(([^)]+)\)(?:\s*,\s*\()`)
	return re.MatchString(query)
}

// estimateRows estimates number of rows affected
func (e *BatchProcessingEngine) estimateRows(query string) int64 {
	upperQuery := strings.ToUpper(query)
	
	// Count VALUES for INSERT
	re := regexp.MustCompile(`(?i)VALUES\s*\(`)
	matches := re.FindAllStringIndex(query, -1)
	if len(matches) > 0 {
		return int64(len(matches))
	}

	// For UPDATE/DELETE with LIMIT
	re = regexp.MustCompile(`(?i)LIMIT\s+(\d+)`)
	matches = re.FindStringSubmatch(query)
	if len(matches) > 1 {
		var limit int
		fmt.Sscanf(matches[1], "%d", &limit)
		return int64(limit)
	}

	return 1
}

// GetBatchStats returns batch statistics
func (e *BatchProcessingEngine) GetBatchStats() BatchStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return BatchStatsResponse{
		BatchesCreated:  e.stats.BatchesCreated,
		QueriesBatched:  e.stats.QueriesBatched,
		RowsOptimized:  e.stats.RowsOptimized,
		AvgBatchSize:   e.stats.AvgBatchSize,
	}
}

type BatchStatsResponse struct {
	BatchesCreated int64   `json:"batches_created"`
	QueriesBatched int64   `json:"queries_batched"`
	RowsOptimized  int64   `json:"rows_optimized"`
	AvgBatchSize   float64 `json:"avg_batch_size"`
}

// Import fmt
import "fmt"