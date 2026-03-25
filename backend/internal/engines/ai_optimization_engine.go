package engines

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

type AIOptimizationEngine struct {
	BaseEngine
	config     *AIConfig
	queryStats *QueryStatistics
	model      *SimpleAIModel
	mu         sync.RWMutex
}

type AIConfig struct {
	Enabled           bool
	AdaptiveCaching   bool
	AnomalyDetection  bool
	PredictiveScaling bool
	QueryOptimization bool
	TrainingInterval  time.Duration
	Threshold         float64
}

type QueryStatistics struct {
	Queries     map[string]*QueryStats
	SlowQueries []string
	mu          sync.Mutex
}

type QueryStats struct {
	Query           string
	AvgDuration     time.Duration
	MinDuration     time.Duration
	MaxDuration     time.Duration
	Count           int64
	TotalDuration   time.Duration
	CacheHitRate    float64
	ErrorRate       float64
	RecentDurations []time.Duration
	LastExecuted    time.Time
}

type SimpleAIModel struct {
	weights map[string]float64
	mu      sync.RWMutex
}

func NewAIOptimizationEngine(config *AIConfig) *AIOptimizationEngine {
	if config == nil {
		config = &AIConfig{
			Enabled:           true,
			AdaptiveCaching:   true,
			AnomalyDetection:  true,
			PredictiveScaling: false,
			QueryOptimization: true,
			TrainingInterval:  5 * time.Minute,
			Threshold:         2.0,
		}
	}

	engine := &AIOptimizationEngine{
		BaseEngine: BaseEngine{name: "ai_optimization"},
		config:     config,
		queryStats: &QueryStatistics{Queries: make(map[string]*QueryStats)},
		model:      &SimpleAIModel{weights: make(map[string]float64)},
	}

	go engine.trainModel()

	return engine
}

func (e *AIOptimizationEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	e.recordQuery(qc)

	if e.config.QueryOptimization {
		suggestion := e.analyzeQuery(qc)
		if suggestion != "" {
			qc.Metadata["ai_suggestion"] = suggestion
		}
	}

	if e.config.AdaptiveCaching {
		cacheRecommendation := e.recommendCache(qc)
		if cacheRecommendation.ShouldCache {
			qc.Metadata["ai_cache_ttl"] = cacheRecommendation.TTL
			qc.Metadata["ai_cache_priority"] = cacheRecommendation.Priority
		}
	}

	if e.config.AnomalyDetection {
		if e.isAnomaly(qc) {
			qc.Metadata["ai_anomaly_detected"] = true
			qc.Metadata["ai_anomaly_score"] = e.calculateAnomalyScore(qc)
		}
	}

	return types.EngineResult{Continue: true}
}

func (e *AIOptimizationEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	e.updateStats(qc)

	if e.config.PredictiveScaling {
		scalingSignal := e.predictScalingNeed()
		qc.Metadata["ai_scaling_signal"] = scalingSignal
	}

	return types.EngineResult{Continue: true}
}

func (e *AIOptimizationEngine) recordQuery(qc *types.QueryContext) {
	e.queryStats.mu.Lock()
	defer e.queryStats.mu.Unlock()

	normalizedQuery := normalizeQuery(qc.RawQuery)

	if _, ok := e.queryStats.Queries[normalizedQuery]; !ok {
		e.queryStats.Queries[normalizedQuery] = &QueryStats{
			Query:           normalizedQuery,
			MinDuration:     math.MaxInt64,
			RecentDurations: make([]time.Duration, 0, 100),
		}
	}

	stats := e.queryStats.Queries[normalizedQuery]
	stats.Count++
	stats.LastExecuted = time.Now()

	if qc.Response != nil && qc.Response.Duration > 0 {
		stats.RecentDurations = append(stats.RecentDurations, qc.Response.Duration)
		if len(stats.RecentDurations) > 100 {
			stats.RecentDurations = stats.RecentDurations[1:]
		}
	}
}

func (e *AIOptimizationEngine) updateStats(qc *types.QueryContext) {
	e.queryStats.mu.Lock()
	defer e.queryStats.mu.Unlock()

	normalizedQuery := normalizeQuery(qc.RawQuery)

	if stats, ok := e.queryStats.Queries[normalizedQuery]; ok {
		if qc.Response != nil && qc.Response.Duration > 0 {
			stats.TotalDuration += qc.Response.Duration

			newAvg := stats.TotalDuration.Nanoseconds() / stats.Count
			stats.AvgDuration = time.Duration(newAvg)

			if qc.Response.Duration < stats.MinDuration {
				stats.MinDuration = qc.Response.Duration
			}
			if qc.Response.Duration > stats.MaxDuration {
				stats.MaxDuration = qc.Response.Duration
			}
		}

		if qc.Response != nil && qc.Response.Error != nil {
			stats.ErrorRate = stats.ErrorRate + 0.01
		}
	}
}

func (e *AIOptimizationEngine) analyzeQuery(qc *types.QueryContext) string {
	normalizedQuery := normalizeQuery(qc.RawQuery)

	e.queryStats.mu.Lock()
	stats, exists := e.queryStats.Queries[normalizedQuery]
	e.queryStats.mu.Unlock()

	if !exists {
		return "Query pattern not seen before - consider adding to query cache"
	}

	if stats.AvgDuration > 1*time.Second {
		return fmt.Sprintf("Slow query detected (avg: %v) - consider adding index or optimizing", stats.AvgDuration)
	}

	if stats.ErrorRate > 0.05 {
		return fmt.Sprintf("High error rate (%.2f%%) - investigate query", stats.ErrorRate*100)
	}

	if stats.Count > 1000 {
		return "Frequently executed query - consider aggressive caching"
	}

	return ""
}

type CacheRecommendation struct {
	ShouldCache bool
	TTL         time.Duration
	Priority    int
}

func (e *AIOptimizationEngine) recommendCache(qc *types.QueryContext) CacheRecommendation {
	normalizedQuery := normalizeQuery(qc.RawQuery)

	e.queryStats.mu.Lock()
	stats, exists := e.queryStats.Queries[normalizedQuery]
	e.queryStats.mu.Unlock()

	if !exists {
		return CacheRecommendation{ShouldCache: false}
	}

	rec := CacheRecommendation{}

	if stats.Count > 100 && stats.AvgDuration > 50*time.Millisecond {
		rec.ShouldCache = true
		rec.Priority = 1
		rec.TTL = 5 * time.Minute
	} else if stats.Count > 50 {
		rec.ShouldCache = true
		rec.Priority = 2
		rec.TTL = 2 * time.Minute
	} else {
		rec.ShouldCache = false
	}

	return rec
}

func (e *AIOptimizationEngine) isAnomaly(qc *types.QueryContext) bool {
	if qc.Response == nil || qc.Response.Duration == 0 {
		return false
	}

	normalizedQuery := normalizeQuery(qc.RawQuery)

	e.queryStats.mu.Lock()
	stats, exists := e.queryStats.Queries[normalizedQuery]
	e.queryStats.mu.Unlock()

	if !exists || stats.Count < 10 {
		return false
	}

	mean := stats.AvgDuration
	stdDev := calculateStdDev(stats.RecentDurations, mean)

	duration := qc.Response.Duration
	zScore := float64(duration-mean) / float64(stdDev)

	return math.Abs(zScore) > e.config.Threshold
}

func (e *AIOptimizationEngine) calculateAnomalyScore(qc *types.QueryContext) float64 {
	if qc.Response == nil || qc.Response.Duration == 0 {
		return 0
	}

	normalizedQuery := normalizeQuery(qc.RawQuery)

	e.queryStats.mu.Lock()
	stats, exists := e.queryStats.Queries[normalizedQuery]
	e.queryStats.mu.Unlock()

	if !exists {
		return 0
	}

	mean := stats.AvgDuration
	if mean == 0 {
		return 0
	}

	stdDev := calculateStdDev(stats.RecentDurations, mean)
	if stdDev == 0 {
		return 0
	}

	zScore := float64(qc.Response.Duration-mean) / float64(stdDev)
	return math.Abs(zScore)
}

func (e *AIOptimizationEngine) predictScalingNeed() string {
	e.queryStats.mu.Lock()
	defer e.queryStats.mu.Unlock()

	var totalQueries int64
	var slowQueries int64

	for _, stats := range e.queryStats.Queries {
		totalQueries += stats.Count
		if stats.AvgDuration > 1*time.Second {
			slowQueries++
		}
	}

	if slowQueries > 10 && totalQueries > 1000 {
		return "scale_up"
	} else if slowQueries == 0 && totalQueries < 100 {
		return "scale_down"
	}

	return "maintain"
}

func (e *AIOptimizationEngine) trainModel() {
	ticker := time.NewTicker(e.config.TrainingInterval)
	defer ticker.Stop()

	for range ticker.C {
		e.queryStats.mu.Lock()

		for query, stats := range e.queryStats.Queries {
			score := calculateQueryScore(stats)
			e.model.mu.Lock()
			e.model.weights[query] = score
			e.model.mu.Unlock()
		}

		e.queryStats.mu.Unlock()
	}
}

func (e *AIOptimizationEngine) GetTopSlowQueries(limit int) []*QueryStats {
	e.queryStats.mu.Lock()
	defer e.queryStats.mu.Unlock()

	slowQueries := make([]*QueryStats, 0, len(e.queryStats.Queries))
	for _, stats := range e.queryStats.Queries {
		slowQueries = append(slowQueries, stats)
	}

	for i := 0; i < len(slowQueries)-1; i++ {
		for j := i + 1; j < len(slowQueries); j++ {
			if slowQueries[i].AvgDuration < slowQueries[j].AvgDuration {
				slowQueries[i], slowQueries[j] = slowQueries[j], slowQueries[i]
			}
		}
	}

	if limit > 0 && limit < len(slowQueries) {
		return slowQueries[:limit]
	}
	return slowQueries
}

func (e *AIOptimizationEngine) GetQueryStats(query string) (*QueryStats, bool) {
	e.queryStats.mu.Lock()
	defer e.queryStats.mu.Unlock()

	stats, ok := e.queryStats.Queries[normalizeQuery(query)]
	return stats, ok
}

func (e *AIOptimizationEngine) GetInsights() map[string]interface{} {
	e.queryStats.mu.Lock()
	defer e.queryStats.mu.Unlock()

	totalQueries := int64(0)
	var totalDuration time.Duration

	for _, stats := range e.queryStats.Queries {
		totalQueries += stats.Count
		totalDuration += stats.TotalDuration
	}

	insights := map[string]interface{}{
		"total_unique_queries":   len(e.queryStats.Queries),
		"total_executions":       totalQueries,
		"avg_query_time":         totalDuration / time.Duration(max(totalQueries, 1)),
		"scaling_recommendation": e.predictScalingNeed(),
	}

	return insights
}

func (e *AIOptimizationEngine) SetConfig(config *AIConfig) {
	e.config = config
}

func (e *AIOptimizationEngine) GetConfig() *AIConfig {
	return e.config
}

func normalizeQuery(query string) string {
	return query
}

func calculateStdDev(durations []time.Duration, mean time.Duration) time.Duration {
	if len(durations) < 2 {
		return 0
	}

	var sumSquaredDiff int64
	for _, d := range durations {
		diff := int64(d - mean)
		sumSquaredDiff += diff * diff
	}

	variance := float64(sumSquaredDiff) / float64(len(durations))
	return time.Duration(math.Sqrt(variance))
}

func calculateQueryScore(stats *QueryStats) float64 {
	frequencyScore := math.Min(float64(stats.Count)/1000.0, 1.0)
	durationScore := math.Min(float64(stats.AvgDuration.Milliseconds())/1000.0, 1.0)
	errorPenalty := stats.ErrorRate * 2

	score := (frequencyScore * 0.4) + (durationScore * 0.4) - errorPenalty
	return math.Max(0, score)
}
