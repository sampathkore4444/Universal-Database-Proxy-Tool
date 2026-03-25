package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// QueryCostEstimatorEngine predicts query cost before execution, prevents expensive queries
type QueryCostEstimatorEngine struct {
	BaseEngine
	config      *CostEstimatorConfig
	costModel   *CostModel
	queryCosts  map[string]*QueryCost
	stats       *CostEstimatorStats
	mu          sync.RWMutex
}

type CostEstimatorConfig struct {
	Enabled            bool
	MaxCostThreshold  float64
	EnableCostWarning bool
	CostModelType     string // simple, advanced
}

type CostModel struct {
	TableScanCost      float64
	IndexScanCost      float64
	JoinCost           float64
	SortCost           float64
	AggregateCost      float64
	SubqueryMultiplier float64
}

type QueryCost struct {
	Query             string
	EstimatedCost     float64
	EstimatedRows     int64
	HasFullTableScan  bool
	HasSubqueries     bool
	HasJoins          bool
	HasAggregates     bool
	Complexity        string
	Recommendation    string
}

type CostEstimatorStats struct {
	QueriesEstimated   int64
	ExpensiveQueries  int64
	QueriesBlocked    int64
	AvgEstimatedCost  float64
	mu                sync.RWMutex
}

// NewQueryCostEstimatorEngine creates a new Query Cost Estimator Engine
func NewQueryCostEstimatorEngine(config *CostEstimatorConfig) *QueryCostEstimatorEngine {
	if config == nil {
		config = &CostEstimatorConfig{
			Enabled:            true,
			MaxCostThreshold:  1000.0,
			EnableCostWarning: true,
			CostModelType:     "simple",
		}
	}

	engine := &QueryCostEstimatorEngine{
		BaseEngine: BaseEngine{name: "query_cost_estimator"},
		config:     config,
		costModel: &CostModel{
			TableScanCost:      10.0,
			IndexScanCost:      1.0,
			JoinCost:           5.0,
			SortCost:           3.0,
			AggregateCost:      2.0,
			SubqueryMultiplier: 2.0,
		},
		queryCosts: make(map[string]*QueryCost),
		stats:      &CostEstimatorStats{},
	}

	return engine
}

// Process estimates query cost before execution
func (e *QueryCostEstimatorEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Don't estimate for non-SELECT queries (except complex ones)
	upperQuery := strings.ToUpper(query)
	if !strings.HasPrefix(upperQuery, "SELECT") && !strings.HasPrefix(upperQuery, "WITH") {
		return types.EngineResult{Continue: true}
	}

	// Estimate cost
	cost := e.estimateCost(query)

	e.stats.mu.Lock()
	e.stats.QueriesEstimated++
	
	if cost.EstimatedCost > e.config.MaxCostThreshold {
		e.stats.ExpensiveQueries++
		e.stats.QueriesBlocked++
		e.stats.mu.Unlock()

		// Block expensive query
		return types.EngineResult{
			Continue: false,
			Error: fmt.Errorf("query cost %.2f exceeds threshold %.2f - %s", 
				cost.EstimatedCost, e.config.MaxCostThreshold, cost.Recommendation),
		}
	}

	e.stats.AvgEstimatedCost = (e.stats.AvgEstimatedCost*float64(e.stats.QueriesEstimated-1) + cost.EstimatedCost) / float64(e.stats.QueriesEstimated)
	e.stats.mu.Unlock()

	// Store cost info
	e.mu.Lock()
	e.queryCosts[fmt.Sprintf("%x", hashString(query))] = cost
	e.mu.Unlock()

	// Add metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["estimated_cost"] = cost.EstimatedCost
	qc.Metadata["estimated_rows"] = cost.EstimatedRows
	qc.Metadata["has_full_scan"] = cost.HasFullTableScan
	qc.Metadata["cost_complexity"] = cost.Complexity

	if e.config.EnableCostWarning && cost.EstimatedCost > e.config.MaxCostThreshold*0.5 {
		qc.Metadata["cost_warning"] = cost.Recommendation
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse processes query response
func (e *QueryCostEstimatorEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// estimateCost calculates estimated cost of a query
func (e *QueryCostEstimatorEngine) estimateCost(query string) *QueryCost {
	cost := &QueryCost{
		Query: query,
	}

	upperQuery := strings.ToUpper(query)

	// Check for full table scans
	if !strings.Contains(upperQuery, "WHERE") && !strings.Contains(upperQuery, "INDEX") {
		cost.HasFullTableScan = true
		cost.EstimatedCost += e.costModel.TableScanCost * 100
	}

	// Check for WHERE clause
	if strings.Contains(upperQuery, "WHERE") {
		cost.EstimatedCost += e.costModel.IndexScanCost * 10
	}

	// Count joins
	joinCount := strings.Count(upperQuery, " JOIN ")
	cost.HasJoins = joinCount > 0
	cost.EstimatedCost += float64(joinCount) * e.costModel.JoinCost

	// Check for subqueries
	subqueryCount := strings.Count(upperQuery, "(")
	cost.HasSubqueries = subqueryCount > 0
	cost.EstimatedCost += float64(subqueryCount) * e.costModel.SubqueryMultiplier

	// Check for aggregates
	if strings.Contains(upperQuery, "GROUP BY") || strings.Contains(upperQuery, "ORDER BY") {
		cost.HasAggregates = true
		cost.EstimatedCost += e.costModel.AggregateCost
	}

	// Estimate rows (simplified)
	limitMatch := regexp.MustCompile(`(?i)LIMIT\s+(\d+)`)
	matches := limitMatch.FindStringSubmatch(query)
	if len(matches) > 1 {
		cost.EstimatedRows = 0
	var limitVal int64
	if _, err := fmt.Sscanf(matches[1], "%d", &limitVal); err == nil {
		cost.EstimatedRows = limitVal
	}
	} else {
		cost.EstimatedRows = 10000 // default
	}

	// Calculate complexity
	if cost.EstimatedCost < 10 {
		cost.Complexity = "low"
		cost.Recommendation = "Query appears optimized"
	} else if cost.EstimatedCost < 100 {
		cost.Complexity = "medium"
		cost.Recommendation = "Consider adding indexes on filter columns"
	} else {
		cost.Complexity = "high"
		cost.Recommendation = "Query may cause performance issues, consider optimization"
	}

	// Adjust for full table scan
	if cost.HasFullTableScan {
		cost.EstimatedCost *= 10
		cost.Recommendation = "Full table scan detected - add WHERE clause or index"
	}

	return cost
}

// GetEstimatedCosts returns historical cost estimates
func (e *QueryCostEstimatorEngine) GetEstimatedCosts() []QueryCost {
	e.mu.RLock()
	defer e.mu.RUnlock()

	costs := make([]QueryCost, 0, len(e.queryCosts))
	for _, c := range e.queryCosts {
		costs = append(costs, *c)
	}

	return costs
}

// GetCostEstimatorStats returns cost estimator statistics
func (e *QueryCostEstimatorEngine) GetCostEstimatorStats() CostEstimatorStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return CostEstimatorStatsResponse{
		QueriesEstimated:   e.stats.QueriesEstimated,
		ExpensiveQueries:   e.stats.ExpensiveQueries,
		QueriesBlocked:    e.stats.QueriesBlocked,
		AvgEstimatedCost:  e.stats.AvgEstimatedCost,
	}
}

type CostEstimatorStatsResponse struct {
	QueriesEstimated  int64   `json:"queries_estimated"`
	ExpensiveQueries  int64   `json:"expensive_queries"`
	QueriesBlocked    int64   `json:"queries_blocked"`
	AvgEstimatedCost  float64 `json:"avg_estimated_cost"`
}