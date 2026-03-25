package engines

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

// CDCEngine handles Change Data Capture - streams data changes to downstream systems
type CDCEngine struct {
	BaseEngine
	config      *CDCConfig
	subscribers map[string]*CDCSubscriber
	changeLog   *ChangeLog
	stats       *CDCStats
	mu          sync.RWMutex
}

type CDCConfig struct {
	Enabled           bool
	BufferSize        int
	FlushInterval     time.Duration
	SupportedOps      []string // INSERT, UPDATE, DELETE
	IncludeBeforeImage bool
	IncludeAfterImage bool
}

type CDCSubscriber struct {
	ID          string
	Name        string
	Endpoint    string
	Format      string // json, avro, protobuf
	Filter      string // Table filter
	EventTypes  []string
	RetryCount  int
	Status      string
	LastAck     time.Time
}

type ChangeEvent struct {
	ID            string
	Timestamp     time.Time
	Operation     string
	Table         string
	Database      string
	BeforeImage   map[string]interface{}
	AfterImage    map[string]interface{}
	TransactionID string
	SequenceNum   int64
}

type ChangeLog struct {
	events  []ChangeEvent
	maxSize int
	mu      sync.RWMutex
}

type CDCStats struct {
	EventsPublished   int64
	EventsBuffered    int64
	FailedPublishes   int64
	SubscriberErrors  int64
	AvgLatencyMs      float64
	mu                sync.RWMutex
}

// NewCDCEngine creates a new CDC Engine
func NewCDCEngine(config *CDCConfig) *CDCEngine {
	if config == nil {
		config = &CDCConfig{
			Enabled:            true,
			BufferSize:         1000,
			FlushInterval:      time.Second * 5,
			SupportedOps:       []string{"INSERT", "UPDATE", "DELETE"},
			IncludeAfterImage:  true,
		}
	}

	engine := &CDCEngine{
		BaseEngine:  BaseEngine{name: "cdc"},
		config:      config,
		subscribers: make(map[string]*CDCSubscriber),
		changeLog:  &ChangeLog{maxSize: 10000},
		stats:       &CDCStats{},
	}

	return engine
}

// AddSubscriber adds a CDC subscriber
func (e *CDCEngine) AddSubscriber(subscriber *CDCSubscriber) {
	e.mu.Lock()
	defer e.mu.Unlock()
	subscriber.Status = "active"
	e.subscribers[subscriber.ID] = subscriber
}

// Process captures change events
func (e *CDCEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Detect operation type
	operation := e.detectOperation(query)
	if operation == "" {
		return types.EngineResult{Continue: true}
	}

	// Check if operation is supported
	if !e.isOperationSupported(operation) {
		return types.EngineResult{Continue: true}
	}

	// Extract table name
	table := e.extractTable(query)

	// Create change event
	event := ChangeEvent{
		ID:            fmt.Sprintf("%d-%s", time.Now().UnixNano(), operation),
		Timestamp:     time.Now(),
		Operation:     operation,
		Table:         table,
		Database:      qc.Database,
		TransactionID: qc.ID,
	}

	// Add event to change log
	e.changeLog.mu.Lock()
	if len(e.changeLog.events) >= e.changeLog.maxSize {
		e.changeLog.events = e.changeLog.events[1:]
	}
	e.changeLog.events = append(e.changeLog.events, event)
	e.changeLog.mu.Unlock()

	e.stats.mu.Lock()
	e.stats.EventsBuffered++
	e.stats.mu.Unlock()

	// Store event in metadata for response processing
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["cdc_event"] = event
	qc.Metadata["cdc_enabled"] = true

	return types.EngineResult{Continue: true}
}

// ProcessResponse publishes change events to subscribers
func (e *CDCEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	if event, ok := qc.Metadata["cdc_event"].(ChangeEvent); ok {
		// Add after image from response
		if qc.Response != nil && e.config.IncludeAfterImage {
			event.AfterImage = e.extractAfterImage(qc)
		}

		// Publish to subscribers
		e.publishEvent(ctx, event)

		e.stats.mu.Lock()
		e.stats.EventsPublished++
		e.stats.mu.Unlock()
	}

	return types.EngineResult{Continue: true}
}

// detectOperation determines the type of operation
func (e *CDCEngine) detectOperation(query string) string {
	upper := strings.ToUpper(query)
	if strings.HasPrefix(upper, "INSERT") {
		return "INSERT"
	}
	if strings.HasPrefix(upper, "UPDATE") {
		return "UPDATE"
	}
	if strings.HasPrefix(upper, "DELETE") {
		return "DELETE"
	}
	return ""
}

// isOperationSupported checks if operation should be captured
func (e *CDCEngine) isOperationSupported(operation string) bool {
	for _, op := range e.config.SupportedOps {
		if op == operation {
			return true
		}
	}
	return false
}

// extractTable extracts table name from query
func (e *CDCEngine) extractTable(query string) string {
	upper := strings.ToUpper(query)

	// Extract FROM clause
	re := regexp.MustCompile(`(?i)FROM\s+(\w+)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}

	// Extract INTO clause
	re = regexp.MustCompile(`(?i)INTO\s+(\w+)`)
	matches = re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}

	// Extract UPDATE clause
	re = regexp.MustCompile(`(?i)UPDATE\s+(\w+)`)
	matches = re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}

	return ""
}

// extractAfterImage extracts row data from response
func (e *CDCEngine) extractAfterImage(qc *types.QueryContext) map[string]interface{} {
	if qc.Response == nil || qc.Response.Data == nil || len(qc.Response.Data) == 0 {
		return nil
	}

	result := make(map[string]interface{})
	columns := qc.Response.Columns

	for rowIdx, row := range qc.Response.Data {
		rowMap := make(map[string]interface{})
		for colIdx, col := range columns {
			if colIdx < len(row) {
				rowMap[col] = row[colIdx]
			}
		}
		result[fmt.Sprintf("row_%d", rowIdx)] = rowMap
	}

	return result
}

// publishEvent sends event to all subscribers
func (e *CDCEngine) publishEvent(ctx context.Context, event ChangeEvent) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, sub := range e.subscribers {
		if sub.Status != "active" {
			continue
		}

		// Check if event matches subscriber filter
		if sub.Filter != "" && !strings.Contains(event.Table, sub.Filter) {
			continue
		}

		// Publish event (simplified - would use actual protocol)
		go e.publishToSubscriber(ctx, sub, event)
	}
}

// publishToSubscriber sends event to a specific subscriber
func (e *CDCEngine) publishToSubscriber(ctx context.Context, sub *CDCSubscriber, event ChangeEvent) error {
	// Serialize event based on format
	var data []byte
	var err error

	switch sub.Format {
	case "json":
		data, err = json.Marshal(event)
	default:
		data, err = json.Marshal(event)
	}

	if err != nil {
		e.stats.mu.Lock()
		e.stats.FailedPublishes++
		e.stats.mu.Unlock()
		return err
	}

	// In real implementation, send to endpoint
	_ = data

	sub.LastAck = time.Now()
	return nil
}

// GetCDCStats returns CDC statistics
func (e *CDCEngine) GetCDCStats() CDCStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return CDCStatsResponse{
		EventsPublished:  e.stats.EventsPublished,
		EventsBuffered:   e.stats.EventsBuffered,
		FailedPublishes:  e.stats.FailedPublishes,
		SubscriberErrors: e.stats.SubscriberErrors,
		AvgLatencyMs:    e.stats.AvgLatencyMs,
	}
}

// GetSubscribers returns all subscribers
func (e *CDCEngine) GetSubscribers() []CDCSubscriber {
	e.mu.RLock()
	defer e.mu.RUnlock()

	subs := make([]CDCSubscriber, 0, len(e.subscribers))
	for _, sub := range e.subscribers {
		subs = append(subs, *sub)
	}
	return subs
}

type CDCStatsResponse struct {
	EventsPublished  int64   `json:"events_published"`
	EventsBuffered   int64   `json:"events_buffered"`
	FailedPublishes  int64   `json:"failed_publishes"`
	SubscriberErrors int64   `json:"subscriber_errors"`
	AvgLatencyMs     float64 `json:"avg_latency_ms"`
}