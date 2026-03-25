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

// HotspotDetectionEngine identifies frequently accessed "hot" rows/tables
type HotspotDetectionEngine struct {
	BaseEngine
	config      *HotspotConfig
	accessLog   *AccessLog
	hotspots    map[string]*Hotspot
	stats       *HotspotStats
	mu          sync.RWMutex
}

type AccessLog struct {
	entries  []HotspotAccessEntry
	maxSize  int
	mu       sync.RWMutex
}

type HotspotAccessEntry struct {
	Timestamp   time.Time
	Table       string
	QueryHash   string
	User        string
	AccessType  string // read, write, scan
	RowsAffected int64
}

type HotspotConfig struct {
	Enabled          bool
	SamplingRate     float64 // 0.0 to 1.0
	HotspotThreshold int     // accesses per window to be considered hot
	WindowMinutes    int
	TrackReads       bool
	TrackWrites      bool
	TrackScans       bool
}

type Hotspot struct {
	Table          string
	AccessCount    int64
	LastAccessed   time.Time
	AccessType     string
	AvgRows        float64
	HotnessScore   float64
}

type HotspotStats struct {
	TotalAccesses    int64
	HotspotDetected int64
	QueriesBlocked  int64
	AvgLatencyMs    float64
	mu              sync.RWMutex
}

// NewHotspotDetectionEngine creates a new Hotspot Detection Engine
func NewHotspotDetectionEngine(config *HotspotConfig) *HotspotDetectionEngine {
	if config == nil {
		config = &HotspotConfig{
			Enabled:          true,
			SamplingRate:     1.0,
			HotspotThreshold: 1000,
			WindowMinutes:    5,
			TrackReads:       true,
			TrackWrites:      true,
			TrackScans:       true,
		}
	}

	engine := &HotspotDetectionEngine{
		BaseEngine:  BaseEngine{name: "hotspot_detection"},
		config:      config,
		accessLog:   &AccessLog{maxSize: 100000},
		hotspots:    make(map[string]*Hotspot),
		stats:       &HotspotStats{},
	}

	return engine
}

// Process tracks query access patterns
func (e *HotspotDetectionEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	// Sampling check
	if e.config.SamplingRate < 1.0 {
		if time.Now().UnixNano()%100 > int64(e.config.SamplingRate*100) {
			return types.EngineResult{Continue: true}
		}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Detect access type
	accessType := e.detectAccessType(query)
	if accessType == "" {
		return types.EngineResult{Continue: true}
	}

	// Extract table
	table := e.extractTable(query)
	if table == "" {
		return types.EngineResult{Continue: true}
	}

	// Log access
	entry := HotspotAccessEntry{
		Timestamp:   time.Now(),
		Table:       table,
		QueryHash:   fmt.Sprintf("%x", hashString(query)),
		User:        qc.User,
		AccessType:  accessType,
		RowsAffected: 0,
	}

	e.accessLog.mu.Lock()
	if len(e.accessLog.entries) >= e.accessLog.maxSize {
		e.accessLog.entries = e.accessLog.entries[1:]
	}
	e.accessLog.entries = append(e.accessLog.entries, entry)
	e.accessLog.mu.Unlock()

	e.stats.mu.Lock()
	e.stats.TotalAccesses++
	e.stats.mu.Unlock()

	// Check for hotspots
	e.detectHotspots(table)

	// If table is hot, add metadata
	if hotspot, ok := e.hotspots[table]; ok {
		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["hotspot_detected"] = true
		qc.Metadata["hotspot_score"] = hotspot.HotnessScore
		qc.Metadata["access_count"] = hotspot.AccessCount
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse processes query response
func (e *HotspotDetectionEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil && qc.Response.RowsAffected > 0 {
		// Update access log with affected rows
		table := e.extractTable(qc.RawQuery)
		if table != "" {
			e.accessLog.mu.Lock()
			if len(e.accessLog.entries) > 0 {
				last := e.accessLog.entries[len(e.accessLog.entries)-1]
				last.RowsAffected = qc.Response.RowsAffected
				e.accessLog.entries[len(e.accessLog.entries)-1] = last
			}
			e.accessLog.mu.Unlock()
		}
	}

	return types.EngineResult{Continue: true}
}

// detectAccessType determines if this is read, write, or scan
func (e *HotspotDetectionEngine) detectAccessType(query string) string {
	upper := strings.ToUpper(query)
	
	if strings.HasPrefix(upper, "SELECT") {
		if strings.Contains(upper, "FULL") || strings.Contains(upper, "LIMIT 1000") {
			return "scan"
		}
		return "read"
	}
	if strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") || strings.HasPrefix(upper, "DELETE") {
		return "write"
	}
	
	return ""
}

// extractTable extracts table name from query
func (e *HotspotDetectionEngine) extractTable(query string) string {
	re := regexp.MustCompile(`(?i)(?:FROM|INTO|UPDATE)\s+(\w+)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// detectHotspots identifies hot tables
func (e *HotspotDetectionEngine) detectHotspots(table string) {
	e.accessLog.mu.Lock()
	
	windowStart := time.Now().Add(-time.Duration(e.config.WindowMinutes) * time.Minute)
	var count int64
	var totalRows int64
	
	for _, entry := range e.accessLog.entries {
		if entry.Timestamp.After(windowStart) && entry.Table == table {
			count++
			totalRows += entry.RowsAffected
		}
	}
	
	e.accessLog.mu.Unlock()

	if int(count) >= e.config.HotspotThreshold {
		e.mu.Lock()
		hotspot, exists := e.hotspots[table]
		if !exists {
			hotspot = &Hotspot{Table: table}
			e.hotspots[table] = hotspot
			e.stats.mu.Lock()
			e.stats.HotspotDetected++
			e.stats.mu.Unlock()
		}
		
		hotspot.AccessCount = count
		hotspot.LastAccessed = time.Now()
		if count > 0 {
			hotspot.AvgRows = float64(totalRows) / float64(count)
		}
		// Calculate hotness score
		hotspot.HotnessScore = float64(count) * hotspot.AvgRows / 1000.0
		
		e.mu.Unlock()
	}
}

// GetHotspots returns detected hotspots
func (e *HotspotDetectionEngine) GetHotspots() []Hotspot {
	e.mu.RLock()
	defer e.mu.RUnlock()

	hotspots := make([]Hotspot, 0, len(e.hotspots))
	for _, h := range e.hotspots {
		hotspots = append(hotspots, *h)
	}

	return hotspots
}

// GetHotspotStats returns hotspot statistics
func (e *HotspotDetectionEngine) GetHotspotStats() HotspotStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	e.mu.RLock()
	hotspotCount := len(e.hotspots)
	e.mu.RUnlock()

	return HotspotStatsResponse{
		TotalAccesses:    e.stats.TotalAccesses,
		HotspotDetected:  int64(hotspotCount),
		QueriesBlocked:   e.stats.QueriesBlocked,
		AvgLatencyMs:    e.stats.AvgLatencyMs,
	}
}

type HotspotStatsResponse struct {
	TotalAccesses   int64   `json:"total_accesses"`
	HotspotDetected int64   `json:"hotspot_detected"`
	QueriesBlocked  int64   `json:"queries_blocked"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}