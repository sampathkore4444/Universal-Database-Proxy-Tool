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

// TimeSeriesEngine handles specialized time-series data patterns
type TimeSeriesEngine struct {
	BaseEngine
	config        *TimeSeriesConfig
	aggregations  map[string]*TimeSeriesAggregation
	windowBuckets map[string]*WindowBucket
	stats         *TimeSeriesStats
	mu            sync.RWMutex
}

type TimeSeriesConfig struct {
	Enabled             bool
	AutoAggregation     bool // Auto-add time-based aggregations
	RetentionDays       int
	EnableDownsampling  bool
	DownsampleIntervals []string // 1m, 5m, 1h, 1d
}

type TimeSeriesAggregation struct {
	Table      string
	Column     string
	Function   string // AVG, SUM, MIN, MAX, COUNT
	Interval   string // 1m, 5m, 1h
	RetainDays int
}

type WindowBucket struct {
	Window      string
	Timestamp   time.Time
	Count       int64
	Sum         float64
	Min         float64
	Max         float64
}

type TimeSeriesStats struct {
	QueriesDetected    int64
	AggregationsAdded  int64
	DownsampledQueries int64
	AvgLatencyMs       float64
	mu                 sync.RWMutex
}

// NewTimeSeriesEngine creates a new Time-Series Engine
func NewTimeSeriesEngine(config *TimeSeriesConfig) *TimeSeriesEngine {
	if config == nil {
		config = &TimeSeriesConfig{
			Enabled:            true,
			AutoAggregation:    true,
			RetentionDays:      365,
			EnableDownsampling:  true,
			DownsampleIntervals: []string{"1m", "5m", "1h", "1d"},
		}
	}

	engine := &TimeSeriesEngine{
		BaseEngine:    BaseEngine{name: "timeseries"},
		config:        config,
		aggregations:  make(map[string]*TimeSeriesAggregation),
		windowBuckets: make(map[string]*WindowBucket),
		stats:         &TimeSeriesStats{},
	}

	return engine
}

// Process handles time-series query optimization
func (e *TimeSeriesEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	upperQuery := strings.ToUpper(query)
	
	// Detect if this is a time-series query
	if !e.isTimeSeriesQuery(upperQuery) {
		return types.EngineResult{Continue: true}
	}

	e.stats.mu.Lock()
	e.stats.QueriesDetected++
	e.stats.mu.Unlock()

	// Add aggregation hints
	if e.config.AutoAggregation {
		interval := e.detectTimeInterval(query)
		if interval != "" {
			// Modify query to add time-based grouping
			rewritten := e.addTimeAggregation(query, interval)
			if rewritten != query {
				qc.RawQuery = rewritten
				
				e.stats.mu.Lock()
				e.stats.AggregationsAdded++
				e.stats.mu.Unlock()

				if qc.Metadata == nil {
					qc.Metadata = make(map[string]interface{})
				}
				qc.Metadata["timeseries_optimized"] = true
				qc.Metadata["aggregation_interval"] = interval
			}
		}
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles time-series response
func (e *TimeSeriesEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil && qc.Response.Duration > 0 {
		e.stats.mu.Lock()
		latency := float64(qc.Response.Duration.Milliseconds())
		count := e.stats.QueriesDetected
		e.stats.AvgLatencyMs = (e.stats.AvgLatencyMs*float64(count-1) + latency) / float64(count)
		e.stats.mu.Unlock()
	}
	return types.EngineResult{Continue: true}
}

// isTimeSeriesQuery checks if query contains time-series patterns
func (e *TimeSeriesEngine) isTimeSeriesQuery(query string) bool {
	patterns := []string{
		"GROUP BY",
		"TIME",
		"DATE",
		"TIMESTAMP",
		"OVER",
		"WINDOW",
		"FLOOR",
		"CEIL",
		"INTERVAL",
	}

	for _, pattern := range patterns {
		if strings.Contains(query, pattern) {
			return true
		}
	}

	// Check for time columns
	timeColumnPatterns := []string{
		"created_at",
		"updated_at",
		"timestamp",
		"recorded_at",
		"event_time",
	}

	for _, col := range timeColumnPatterns {
		re := regexp.MustCompile(fmt.Sprintf("(?i)\\b%s\\b", col))
		if re.MatchString(query) {
			return true
		}
	}

	return false
}

// detectTimeInterval detects time interval from query
func (e *TimeSeriesEngine) detectTimeInterval(query string) string {
	// Look for INTERVAL keyword
	re := regexp.MustCompile(`(?i)INTERVAL\s+(\d+)\s*(minute|hour|day|second|week|month|year)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 2 {
		return matches[1] + " " + matches[2]
	}

	// Look for TIME bucket
	re = regexp.MustCompile(`(?i)FLOOR\([^)]*timestamp.*to\s+(\w+)\)`)
	matches = re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}

	// Default to 1 hour
	return "1 hour"
}

// addTimeAggregation adds time-based aggregation to query
func (e *TimeSeriesEngine) addTimeAggregation(query, interval string) string {
	// Check if already has GROUP BY
	if strings.Contains(strings.ToUpper(query), "GROUP BY") {
		return query
	}

	// Add time-based grouping (simplified)
	re := regexp.MustCompile(`(?i)FROM\s+(\w+)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		_ = matches[1] // table name not currently used but reserved for future
		
		// Add GROUP BY with time bucket
		if !strings.Contains(strings.ToUpper(query), "WHERE") {
			return query + " GROUP BY FLOOR(created_at / INTERVAL '1 hour')"
		}
	}

	return query
}

// GetTimeSeriesStats returns time-series statistics
func (e *TimeSeriesEngine) GetTimeSeriesStats() TimeSeriesStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return TimeSeriesStatsResponse{
		QueriesDetected:    e.stats.QueriesDetected,
		AggregationsAdded:  e.stats.AggregationsAdded,
		DownsampledQueries: e.stats.DownsampledQueries,
		AvgLatencyMs:       e.stats.AvgLatencyMs,
	}
}

type TimeSeriesStatsResponse struct {
	QueriesDetected    int64   `json:"queries_detected"`
	AggregationsAdded  int64   `json:"aggregations_added"`
	DownsampledQueries int64   `json:"downsampled_queries"`
	AvgLatencyMs       float64 `json:"avg_latency_ms"`
}