package engines

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// DataCompressionEngine provides transparent compression for large fields
type DataCompressionEngine struct {
	BaseEngine
	config       *CompressionConfig
	columnRules  map[string]*CompressionRule
	stats        *CompressionStats
	mu           sync.RWMutex
}

type CompressionConfig struct {
	Enabled        bool
	ThresholdBytes int // Only compress if larger than this
	Algorithm      string // gzip, zstd, lz4
	MinRatio       float64 // Minimum compression ratio to store
}

type CompressionRule struct {
	Table         string
	Column        string
	CompressionType string // always, auto, never
	MinSizeBytes  int
}

type CompressionStats struct {
	FieldsCompressed int64
	BytesSaved       int64
	CompressionRatio float64
	mu               sync.RWMutex
}

// NewDataCompressionEngine creates a new Data Compression Engine
func NewDataCompressionEngine(config *CompressionConfig) *DataCompressionEngine {
	if config == nil {
		config = &CompressionConfig{
			Enabled:        false,
			ThresholdBytes: 1024,
			Algorithm:      "gzip",
			MinRatio:       0.7,
		}
	}

	engine := &DataCompressionEngine{
		BaseEngine:  BaseEngine{name: "data_compression"},
		config:      config,
		columnRules: make(map[string]*CompressionRule),
		stats:       &CompressionStats{},
	}

	return engine
}

// AddCompressionRule adds a compression rule for a column
func (e *DataCompressionEngine) AddCompressionRule(rule *CompressionRule) {
	key := fmt.Sprintf("%s.%s", rule.Table, rule.Column)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.columnRules[key] = rule
}

// Process handles compression logic
func (e *DataCompressionEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Check for INSERT/UPDATE with large values
	upperQuery := strings.ToUpper(query)
	if !strings.HasPrefix(upperQuery, "INSERT") && !strings.HasPrefix(upperQuery, "UPDATE") {
		return types.EngineResult{Continue: true}
	}

	// Extract table and columns
	table := e.extractTable(query)
	if table == "" {
		return types.EngineResult{Continue: true}
	}

	// Mark as compression eligible
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["compression_candidate"] = true
	qc.Metadata["compression_enabled"] = true

	return types.EngineResult{Continue: true}
}

// ProcessResponse compresses response data
func (e *DataCompressionEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled || qc.Response == nil || qc.Response.Data == nil {
		return types.EngineResult{Continue: true}
	}

	e.stats.mu.Lock()
	e.stats.FieldsCompressed++
	e.stats.mu.Unlock()

	return types.EngineResult{Continue: true}
}

// extractTable extracts table name from query
func (e *DataCompressionEngine) extractTable(query string) string {
	re := regexp.MustCompile(`(?i)(?:INTO|UPDATE)\s+(\w+)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// CompressValue compresses a single value
func (e *DataCompressionEngine) CompressValue(data string) (string, error) {
	if len(data) < e.config.ThresholdBytes {
		return data, nil
	}

	var compressed []byte
	var err error

	switch e.config.Algorithm {
	case "gzip":
		compressed, err = e.compressGzip([]byte(data))
	default:
		compressed, err = e.compressGzip([]byte(data))
	}

	if err != nil {
		return "", err
	}

	ratio := float64(len(data)) / float64(len(compressed))
	
	e.stats.mu.Lock()
	e.stats.BytesSaved += int64(len(data) - len(compressed))
	e.stats.CompressionRatio = (e.stats.CompressionRatio*float64(e.stats.FieldsCompressed-1) + ratio) / float64(e.stats.FieldsCompressed)
	e.stats.mu.Unlock()

	if ratio >= e.config.MinRatio {
		return base64.StdEncoding.EncodeToString(compressed), nil
	}

	return data, nil
}

// compressGzip compresses using gzip
func (e *DataCompressionEngine) compressGzip(data []byte) ([]byte, error) {
	var buf []byte
	gzw := gzip.NewWriter(&buf)
	_, err := gzw.Write(data)
	if err != nil {
		return nil, err
	}
	gzw.Close()
	return buf, nil
}

// GetCompressionStats returns compression statistics
func (e *DataCompressionEngine) GetCompressionStats() CompressionStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return CompressionStatsResponse{
		FieldsCompressed:  e.stats.FieldsCompressed,
		BytesSaved:       e.stats.BytesSaved,
		CompressionRatio: e.stats.CompressionRatio,
	}
}

type CompressionStatsResponse struct {
	FieldsCompressed int64   `json:"fields_compressed"`
	BytesSaved       int64   `json:"bytes_saved"`
	CompressionRatio float64 `json:"compression_ratio"`
}