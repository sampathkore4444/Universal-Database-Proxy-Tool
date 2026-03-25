package engines

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// ConnectionPoolOptimizerEngine provides dynamic pool sizing and connection lifecycle optimization
type ConnectionPoolOptimizerEngine struct {
	BaseEngine
	config           *PoolOptimizerConfig
	poolStats        *PoolStatistics
	connectionTracker *ConnectionTracker
	predictions      *LoadPredictions
	mu               sync.RWMutex
}

type PoolOptimizerConfig struct {
	Enabled              bool          // Enable the engine
	AutoResizeEnabled    bool          // Enable automatic pool resizing
	MinConnections       int           // Minimum pool size
	MaxConnections       int           // Maximum pool size
	ResizeInterval       time.Duration // How often to check/resize
	IdleTimeout          time.Duration // Connection idle timeout
	MaxLifetime          time.Duration // Connection max lifetime
	HealthCheckEnabled   bool          // Enable connection health checks
	PredictionEnabled    bool          // Enable load prediction
	ScalingThreshold     float64       // % utilization to trigger scale up/down
}

type PoolStatistics struct {
	ActiveConnections   int
	IdleConnections     int
	WaitingRequests    int
	TotalConnections    int
	ConnectionAcquired  int64
	ConnectionReleased  int64
	ConnectionClosed    int64
	ConnectionTimeout   int64
	AvgAcquireTime      time.Duration
	MaxAcquireTime      time.Duration
	LastResizeTime      time.Time
	CurrentPoolSize     int
	HistoricalSizes     []PoolSizePoint
	mu                  sync.RWMutex
}

type PoolSizePoint struct {
	Timestamp time.Time
	Size      int
}

type ConnectionTracker struct {
	connections map[string]*ConnectionInfo
	mu          sync.RWMutex
}

type ConnectionInfo struct {
	ConnectionID  string
	AcquiredAt    time.Time
	LastUsedAt    time.Time
	QueryCount    int64
	IsHealthy     bool
	Database      string
	LatencySum    time.Duration
}

type LoadPredictions struct {
	predictedLoad   int
	predictionTime  time.Time
	model           *SimplePredictionModel
	history         []PredictionPoint
	mu              sync.RWMutex
}

type PredictionPoint struct {
	Timestamp   time.Time
	ActualLoad  int
	PredictedLoad int
}

type SimplePredictionModel struct {
	weights    []float64
	history    []float64
	windowSize int
}

// NewConnectionPoolOptimizerEngine creates a new Connection Pool Optimizer Engine
func NewConnectionPoolOptimizerEngine(config *PoolOptimizerConfig) *ConnectionPoolOptimizerEngine {
	if config == nil {
		config = &PoolOptimizerConfig{
			Enabled:            true,
			AutoResizeEnabled:  true,
			MinConnections:     5,
			MaxConnections:     100,
			ResizeInterval:     30 * time.Second,
			IdleTimeout:        5 * time.Minute,
			MaxLifetime:        30 * time.Minute,
			HealthCheckEnabled: true,
			PredictionEnabled:  true,
			ScalingThreshold:    0.8, // 80% utilization
		}
	}

	engine := &ConnectionPoolOptimizerEngine{
		BaseEngine:        BaseEngine{name: "connection_pool_optimizer"},
		config:            config,
		poolStats:         &PoolStatistics{HistoricalSizes: make([]PoolSizePoint, 0)},
		connectionTracker: &ConnectionTracker{connections: make(map[string]*ConnectionInfo)},
		predictions:       &LoadPredictions{
			model: &SimplePredictionModel{
				windowSize: 10,
				history:    make([]float64, 0),
			},
			history: make([]PredictionPoint, 0),
		},
	}

	// Start background optimization
	go engine.optimizationLoop()

	return engine
}

// Process handles connection pool optimization
func (e *ConnectionPoolOptimizerEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	// Update statistics
	e.poolStats.mu.Lock()
	e.poolStats.TotalConnections = e.poolStats.ActiveConnections + e.poolStats.IdleConnections
	
	// Record connection acquisition time for average calculation
	if qc.Response != nil && qc.Response.Duration > 0 {
		e.poolStats.ConnectionAcquired++
		if e.poolStats.AvgAcquireTime == 0 {
			e.poolStats.AvgAcquireTime = qc.Response.Duration
		} else {
			e.poolStats.AvgAcquireTime = (e.poolStats.AvgAcquireTime*time.Duration(e.poolStats.ConnectionAcquired-1) + qc.Response.Duration) / time.Duration(e.poolStats.ConnectionAcquired)
		}
		if qc.Response.Duration > e.poolStats.MaxAcquireTime {
			e.poolStats.MaxAcquireTime = qc.Response.Duration
		}
	}
	e.poolStats.mu.Unlock()

	// Update metadata with pool status
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}

	e.poolStats.mu.RLock()
	qc.Metadata["pool_active"] = e.poolStats.ActiveConnections
	qc.Metadata["pool_idle"] = e.poolStats.IdleConnections
	qc.Metadata["pool_total"] = e.poolStats.TotalConnections
	qc.Metadata["pool_size"] = e.poolStats.CurrentPoolSize
	
	// Add predicted load if enabled
	if e.config.PredictionEnabled {
		e.predictions.mu.RLock()
		qc.Metadata["predicted_load"] = e.predictions.predictedLoad
		e.predictions.mu.RUnlock()
	}
	e.poolStats.mu.RUnlock()

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles connection release events
func (e *ConnectionPoolOptimizerEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	e.poolStats.mu.Lock()
	e.poolStats.ConnectionReleased++

	// Track waiting requests (if any)
	if qc.Response != nil && qc.Response.Duration > 5*time.Second {
		// Could indicate pool exhaustion
		e.poolStats.ConnectionTimeout++
	}

e.poolStats.mu.Unlock()

	// Update connection info if tracked (using ClientAddr as proxy)
	if qc.ClientAddr != "" {
		e.connectionTracker.mu.Lock()
		if conn, exists := e.connectionTracker.connections[qc.ClientAddr]; exists {
			conn.LastUsedAt = time.Now()
			conn.QueryCount++
			if qc.Response != nil {
				conn.LatencySum += qc.Response.Duration
			}
		}
		e.connectionTracker.mu.Unlock()
	}

	return types.EngineResult{Continue: true}
}

// TrackConnection adds a new connection to the tracker
func (e *ConnectionPoolOptimizerEngine) TrackConnection(connID, database string) {
	e.connectionTracker.mu.Lock()
	defer e.connectionTracker.mu.Unlock()

	e.connectionTracker.connections[connID] = &ConnectionInfo{
		ConnectionID: connID,
		AcquiredAt:   time.Now(),
		LastUsedAt:   time.Now(),
		IsHealthy:    true,
		Database:     database,
	}

	e.poolStats.mu.Lock()
	e.poolStats.ActiveConnections++
	e.poolStats.CurrentPoolSize++
	e.poolStats.mu.Unlock()
}

// ReleaseConnection marks a connection as released
func (e *ConnectionPoolOptimizerEngine) ReleaseConnection(connID string) {
	e.connectionTracker.mu.Lock()
	defer e.connectionTracker.mu.Unlock()

	if conn, exists := e.connectionTracker.connections[connID]; exists {
		conn.LastUsedAt = time.Now()
	}

	e.poolStats.mu.Lock()
	e.poolStats.ActiveConnections--
	e.poolStats.IdleConnections++
	e.poolStats.mu.Unlock()
}

// CloseConnection marks a connection as closed
func (e *ConnectionPoolOptimizerEngine) CloseConnection(connID string) {
	e.connectionTracker.mu.Lock()
	defer e.connectionTracker.mu.Unlock()

	delete(e.connectionTracker.connections, connID)

	e.poolStats.mu.Lock()
	e.poolStats.ActiveConnections--
	e.poolStats.TotalConnections--
	e.poolStats.CurrentPoolSize--
	e.poolStats.ConnectionClosed++
	e.poolStats.mu.Unlock()
}

// optimizationLoop performs periodic pool optimization
func (e *ConnectionPoolOptimizerEngine) optimizationLoop() {
	ticker := time.NewTicker(e.config.ResizeInterval)
	defer ticker.Stop()

	for range ticker.C {
		if !e.config.AutoResizeEnabled {
			continue
		}

		e.poolStats.mu.Lock()
		utilization := 0.0
		if e.poolStats.TotalConnections > 0 {
			utilization = float64(e.poolStats.ActiveConnections) / float64(e.poolStats.TotalConnections)
		}
		
		// Track history
		e.poolStats.HistoricalSizes = append(e.poolStats.HistoricalSizes, PoolSizePoint{
			Timestamp: time.Now(),
			Size:      e.poolStats.CurrentPoolSize,
		})
		// Keep last 100 points
		if len(e.poolStats.HistoricalSizes) > 100 {
			e.poolStats.HistoricalSizes = e.poolStats.HistoricalSizes[1:]
		}
		e.poolStats.mu.Unlock()

		// Calculate new pool size
		newSize := e.calculateOptimalPoolSize(utilization)
		
		if newSize != e.poolStats.CurrentPoolSize {
			e.poolStats.mu.Lock()
			e.poolStats.LastResizeTime = time.Now()
			e.poolStats.mu.Unlock()
		}

		// Run health checks if enabled
		if e.config.HealthCheckEnabled {
			e.runHealthChecks()
		}

		// Update predictions if enabled
		if e.config.PredictionEnabled {
			e.updatePredictions()
		}
	}
}

// calculateOptimalPoolSize determines the ideal pool size based on current load
func (e *ConnectionPoolOptimizerEngine) calculateOptimalPoolSize(utilization float64) int {
	e.poolStats.mu.Lock()
	currentSize := e.poolStats.CurrentPoolSize
	e.poolStats.mu.Unlock()

	// If no connections yet, use minimum
	if currentSize == 0 {
		return e.config.MinConnections
	}

	var newSize int

	if utilization > e.config.ScalingThreshold {
		// Scale up
		scaleFactor := 1.0 + (utilization - e.config.ScalingThreshold)
		newSize = int(float64(currentSize) * (1 + scaleFactor))
	} else if utilization < (e.config.ScalingThreshold * 0.5) {
		// Scale down
		scaleFactor := 1.0 - ((e.config.ScalingThreshold * 0.5) - utilization)
		newSize = int(float64(currentSize) * scaleFactor)
	} else {
		// Stay same
		newSize = currentSize
	}

	// Apply bounds
	if newSize < e.config.MinConnections {
		newSize = e.config.MinConnections
	}
	if newSize > e.config.MaxConnections {
		newSize = e.config.MaxConnections
	}

	return newSize
}

// runHealthChecks checks connection health
func (e *ConnectionPoolOptimizerEngine) runHealthChecks() {
	e.connectionTracker.mu.Lock()
	defer e.connectionTracker.mu.Unlock()

	now := time.Now()
	toClose := make([]string, 0, 0)

	for connID, conn := range e.connectionTracker.connections {
		// Check idle timeout
		if now.Sub(conn.LastUsedAt) > e.config.IdleTimeout {
			toClose = append(toClose, connID)
			continue
		}

		// Check max lifetime
		if now.Sub(conn.AcquiredAt) > e.config.MaxLifetime {
			toClose = append(toClose, connID)
			continue
		}

		// Check for unhealthy connections (high latency)
		if conn.QueryCount > 0 {
			avgLatency := conn.LatencySum / time.Duration(conn.QueryCount)
			if avgLatency > 10*time.Second {
				conn.IsHealthy = false
			}
		}
	}

	// Close unhealthy/idle connections
	for _, connID := range toClose {
		delete(e.connectionTracker.connections, connID)
		e.poolStats.mu.Lock()
		e.poolStats.IdleConnections--
		e.poolStats.TotalConnections--
		e.poolStats.CurrentPoolSize--
		e.poolStats.ConnectionClosed++
		e.poolStats.mu.Unlock()
	}
}

// updatePredictions predicts future load
func (e *ConnectionPoolOptimizerEngine) updatePredictions() {
	e.poolStats.mu.RLock()
	currentLoad := e.poolStats.ActiveConnections
	e.poolStats.mu.RUnlock()

	e.predictions.mu.Lock()
	defer e.predictions.mu.Unlock()

	// Add to history
	e.predictions.history = append(e.predictions.history, PredictionPoint{
		Timestamp:    time.Now(),
		ActualLoad:   currentLoad,
		PredictedLoad: e.predictions.predictedLoad,
	})
	
	// Keep last 50 points
	if len(e.predictions.history) > 50 {
		e.predictions.history = e.predictions.history[1:]
	}

	// Simple moving average prediction
	if len(e.predictions.history) >= 5 {
		recentHistory := e.predictions.history[len(e.predictions.history)-5:]
		sum := 0
		for _, p := range recentHistory {
			sum += p.ActualLoad
		}
		e.predictions.predictedLoad = sum / len(recentHistory)
	} else {
		e.predictions.predictedLoad = currentLoad
	}

	e.predictions.predictionTime = time.Now()
}

// GetPoolStats returns current pool statistics
func (e *ConnectionPoolOptimizerEngine) GetPoolStats() PoolStatsResponse {
	e.poolStats.mu.RLock()
	defer e.poolStats.mu.RUnlock()

	utilization := 0.0
	if e.poolStats.TotalConnections > 0 {
		utilization = float64(e.poolStats.ActiveConnections) / float64(e.poolStats.TotalConnections)
	}

	return PoolStatsResponse{
		ActiveConnections:   e.poolStats.ActiveConnections,
		IdleConnections:     e.poolStats.IdleConnections,
		TotalConnections:    e.poolStats.TotalConnections,
		CurrentPoolSize:     e.poolStats.CurrentPoolSize,
		Utilization:         utilization,
		ConnectionAcquired:  e.poolStats.ConnectionAcquired,
		ConnectionReleased:  e.poolStats.ConnectionReleased,
		ConnectionClosed:    e.poolStats.ConnectionClosed,
		ConnectionTimeout:   e.poolStats.ConnectionTimeout,
		AvgAcquireTime:      e.poolStats.AvgAcquireTime.String(),
		MaxAcquireTime:      e.poolStats.MaxAcquireTime.String(),
		LastResizeTime:      e.poolStats.LastResizeTime,
	}
}

// GetConnectionHealth returns health status of tracked connections
func (e *ConnectionPoolOptimizerEngine) GetConnectionHealth() []ConnectionHealthResponse {
	e.connectionTracker.mu.RLock()
	defer e.connectionTracker.mu.RUnlock()

	result := make([]ConnectionHealthResponse, 0, len(e.connectionTracker.connections))
	for _, conn := range e.connectionTracker.connections {
		var avgLatency time.Duration
		if conn.QueryCount > 0 {
			avgLatency = conn.LatencySum / time.Duration(conn.QueryCount)
		}

		result = append(result, ConnectionHealthResponse{
			ConnectionID: conn.ConnectionID,
			Database:     conn.Database,
			IsHealthy:    conn.IsHealthy,
			QueryCount:   conn.QueryCount,
			AvgLatency:   avgLatency.String(),
			Uptime:       time.Since(conn.AcquiredAt).String(),
			LastUsed:     conn.LastUsedAt,
		})
	}
	return result
}

// GetLoadPrediction returns predicted load
func (e *ConnectionPoolOptimizerEngine) GetLoadPrediction() (LoadPredictionResponse, bool) {
	e.predictions.mu.RLock()
	defer e.predictions.mu.RUnlock()

	if e.predictions.predictionTime.IsZero() {
		return LoadPredictionResponse{}, false
	}

	return LoadPredictionResponse{
		PredictedLoad: e.predictions.predictedLoad,
		PredictionTime: e.predictions.predictionTime,
		Confidence:     e.calculatePredictionConfidence(),
	}, true
}

// calculatePredictionConfidence returns confidence level of prediction
func (e *ConnectionPoolOptimizerEngine) calculatePredictionConfidence() float64 {
	e.predictions.mu.RLock()
	defer e.predictions.mu.RUnlock()

	if len(e.predictions.history) < 5 {
		return 0.0
	}

	// Calculate error between predicted and actual
	var totalError float64
	count := 0
	
	recent := e.predictions.history[len(e.predictions.history)-5:]
	for _, p := range recent {
		if p.PredictedLoad > 0 {
			error := math.Abs(float64(p.ActualLoad-p.PredictedLoad)) / float64(p.PredictedLoad)
			totalError += error
			count++
		}
	}

	if count == 0 {
		return 0.0
	}

	avgError := totalError / float64(count)
	return math.Max(0, 1.0-avgError)
}

// Helper types for API responses
type PoolStatsResponse struct {
	ActiveConnections   int       `json:"active_connections"`
	IdleConnections     int       `json:"idle_connections"`
	TotalConnections    int       `json:"total_connections"`
	CurrentPoolSize     int       `json:"current_pool_size"`
	Utilization         float64   `json:"utilization"`
	ConnectionAcquired  int64     `json:"connection_acquired"`
	ConnectionReleased  int64     `json:"connection_released"`
	ConnectionClosed    int64     `json:"connection_closed"`
	ConnectionTimeout   int64     `json:"connection_timeout"`
	AvgAcquireTime      string    `json:"avg_acquire_time"`
	MaxAcquireTime      string    `json:"max_acquire_time"`
	LastResizeTime      time.Time `json:"last_resize_time"`
}

type ConnectionHealthResponse struct {
	ConnectionID string    `json:"connection_id"`
	Database     string    `json:"database"`
	IsHealthy    bool      `json:"is_healthy"`
	QueryCount   int64     `json:"query_count"`
	AvgLatency   string    `json:"avg_latency"`
	Uptime       string    `json:"uptime"`
	LastUsed     time.Time `json:"last_used"`
}

type LoadPredictionResponse struct {
	PredictedLoad  int       `json:"predicted_load"`
	PredictionTime time.Time `json:"prediction_time"`
	Confidence     float64   `json:"confidence"`
}