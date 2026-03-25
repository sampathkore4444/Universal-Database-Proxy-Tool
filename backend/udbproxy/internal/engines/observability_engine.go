package engines

import (
	"context"
	"time"

	"github.com/udbp/udbproxy/pkg/logger"
	"github.com/udbp/udbproxy/pkg/metrics"
	"github.com/udbp/udbproxy/pkg/types"
)

type ObservabilityEngine struct {
	BaseEngine
	config      *types.ObservabilityConfig
	slowQueries map[string]time.Time
}

func NewObservabilityEngine(config *types.ObservabilityConfig) *ObservabilityEngine {
	return &ObservabilityEngine{
		BaseEngine:  BaseEngine{name: "observability"},
		config:      config,
		slowQueries: make(map[string]time.Time),
	}
}

func (e *ObservabilityEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if e.config == nil || !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	if e.config.LogQueries {
		logger.Info("Query received")

		e.logQuery(qc)
	}

	startTime := time.Now()
	qc.Timestamp = startTime
	qc.Metadata["start_time"] = startTime.Unix()

	return types.EngineResult{Continue: true}
}

func (e *ObservabilityEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if e.config == nil || !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	startTimeI, ok := qc.Metadata["start_time"].(int64)
	if !ok {
		return types.EngineResult{Continue: true}
	}

	startTime := time.Unix(startTimeI, 0)
	duration := time.Since(startTime)

	if qc.Response != nil {
		qc.Response.Duration = duration
	}

	status := "success"
	if qc.Response != nil && qc.Response.Error != nil {
		status = "error"
		metrics.RecordError(ctx, qc.Response.Error.Error())
	}

	metrics.RecordQuery(ctx, string(qc.DatabaseType), string(qc.Operation), status, duration)

	if e.config.SlowQueryThreshold > 0 && duration > e.config.SlowQueryThreshold {
		e.logSlowQuery(qc, duration)
	}

	return types.EngineResult{Continue: true}
}

func (e *ObservabilityEngine) logQuery(qc *types.QueryContext) {
	logger.Debug("Query details")
}

func (e *ObservabilityEngine) logSlowQuery(qc *types.QueryContext, duration time.Duration) {
	logger.Warn("Slow query detected")
}

func (e *ObservabilityEngine) SetConfig(config *types.ObservabilityConfig) {
	e.config = config
}

func (e *ObservabilityEngine) GetSlowQueries() map[string]time.Time {
	return e.slowQueries
}

func (e *ObservabilityEngine) ClearSlowQueries() {
	e.slowQueries = make(map[string]time.Time)
}

type QueryLog struct {
	ID           string
	Query        string
	Database     string
	User         string
	Duration     time.Duration
	RowsReturned int64
	Timestamp    time.Time
	Status       string
	Error        string
}

func (e *ObservabilityEngine) GetQueryLogs() []QueryLog {
	return nil
}

func (e *ObservabilityEngine) SetLogQueries(enabled bool) {
	if e.config != nil {
		e.config.LogQueries = enabled
	}
}

func (e *ObservabilityEngine) SetSlowQueryThreshold(threshold time.Duration) {
	if e.config != nil {
		e.config.SlowQueryThreshold = threshold
	}
}
