package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// QueryTranslationEngine converts queries between database dialects
type QueryTranslationEngine struct {
	BaseEngine
	config       *TranslationConfig
	dialectMaps  map[string]map[string]string // sourceDB -> function mapping
	translations map[string]*TranslationRule
	stats        *TranslationStats
	mu           sync.RWMutex
}

type TranslationConfig struct {
	Enabled        bool
	SourceDialect  string // mysql, postgres, mssql, oracle
	TargetDialect  string
	AutoDetect     bool
	PreserveHints  bool
}

type TranslationRule struct {
	SourcePattern string
	TargetPattern string
	SourceDialect string
	TargetDialect string
	Priority      int
}

type TranslationStats struct {
	QueriesTranslated  int64
	TranslationsFailed int64
	PartialTranslations int64
	AvgLatencyMs       float64
	mu                 sync.RWMutex
}

// NewQueryTranslationEngine creates a new Query Translation Engine
func NewQueryTranslationEngine(config *TranslationConfig) *QueryTranslationEngine {
	if config == nil {
		config = &TranslationConfig{
			Enabled:       false,
			SourceDialect: "mysql",
			TargetDialect: "postgres",
			AutoDetect:    true,
		}
	}

	engine := &QueryTranslationEngine{
		BaseEngine:   BaseEngine{name: "query_translation"},
		config:       config,
		dialectMaps:  make(map[string]map[string]string),
		translations: make(map[string]*TranslationRule),
		stats:        &TranslationStats{},
	}

	engine.initDefaultMappings()

	return engine
}

func (e *QueryTranslationEngine) initDefaultMappings() {
	// MySQL to PostgreSQL mappings
	e.dialectMaps["mysql_to_postgres"] = map[string]string{
		"`":            "",
		"CONCAT(":      "CONCAT(",
		"IFNULL(":      "COALESCE(",
		"IF(":          "CASE WHEN ",
		"LIMIT":        "LIMIT",
		"CHAR_LENGTH(": "CHAR_LENGTH(",
		"LOCATE(":      "POSITION(",
		"GROUP_CONCAT": "STRING_AGG",
		"AUTO_INCREMENT": "SERIAL",
		"ENGINE=InnoDB": "",
	}
}

// AddTranslationRule adds a custom translation rule
func (e *QueryTranslationEngine) AddTranslationRule(rule *TranslationRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.translations[rule.SourcePattern] = rule
}

// Process handles query translation
func (e *QueryTranslationEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Only translate SELECT, INSERT, UPDATE, DELETE
	upperQuery := strings.ToUpper(query)
	if !strings.HasPrefix(upperQuery, "SELECT") && 
	   !strings.HasPrefix(upperQuery, "INSERT") &&
	   !strings.HasPrefix(upperQuery, "UPDATE") &&
	   !strings.HasPrefix(upperQuery, "DELETE") {
		return types.EngineResult{Continue: true}
	}

	// Detect source dialect if auto-detect enabled
	sourceDialect := e.config.SourceDialect
	if e.config.AutoDetect {
		sourceDialect = e.detectDialect(query)
	}

	// Skip if already target dialect
	if sourceDialect == e.config.TargetDialect {
		return types.EngineResult{Continue: true}
	}

	// Translate query
	translated := e.translateQuery(query, sourceDialect, e.config.TargetDialect)

	if translated != query {
		qc.RawQuery = translated

		e.stats.mu.Lock()
		e.stats.QueriesTranslated++
		e.stats.mu.Unlock()

		if qc.Metadata == nil {
			qc.Metadata = make(map[string]interface{})
		}
		qc.Metadata["query_translated"] = true
		qc.Metadata["source_dialect"] = sourceDialect
		qc.Metadata["target_dialect"] = e.config.TargetDialect
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles translation response
func (e *QueryTranslationEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response != nil && qc.Response.Duration > 0 {
		e.stats.mu.Lock()
		latency := float64(qc.Response.Duration.Milliseconds())
		count := e.stats.QueriesTranslated
		e.stats.AvgLatencyMs = (e.stats.AvgLatencyMs*float64(count-1) + latency) / float64(count)
		e.stats.mu.Unlock()
	}
	return types.EngineResult{Continue: true}
}

// detectDialect detects SQL dialect from query syntax
func (e *QueryTranslationEngine) detectDialect(query string) string {
	upperQuery := strings.ToUpper(query)

	// MySQL indicators
	if strings.Contains(upperQuery, "`") || 
	   strings.Contains(upperQuery, "AUTO_INCREMENT") ||
	   strings.Contains(upperQuery, "CHAR_LENGTH(") {
		return "mysql"
	}

	// PostgreSQL indicators
	if strings.Contains(upperQuery, "RETURNING") ||
	   strings.Contains(upperQuery, "SERIAL") ||
	   strings.Contains(upperQuery, "COALESCE(") {
		return "postgres"
	}

	// MSSQL indicators
	if strings.Contains(upperQuery, "TOP (") ||
	   strings.Contains(upperQuery, "N''") ||
	   strings.Contains(upperQuery, "IDENTITY(") {
		return "mssql"
	}

	// Oracle indicators
	if strings.Contains(upperQuery, "ROWNUM") ||
	   strings.Contains(upperQuery, "START WITH") ||
	   strings.Contains(upperQuery, "CONNECT BY") {
		return "oracle"
	}

	return e.config.SourceDialect
}

// translateQuery translates query from source to target dialect
func (e *QueryTranslationEngine) translateQuery(query, source, target string) string {
	key := fmt.Sprintf("%s_to_%s", source, target)
	mappings, ok := e.dialectMaps[key]
	if !ok {
		return query
	}

	translated := query

	// Apply dialect-specific transformations
	for sourcePattern, targetPattern := range mappings {
		translated = strings.ReplaceAll(translated, sourcePattern, targetPattern)
	}

	// Apply custom rules
	e.mu.RLock()
	for _, rule := range e.translations {
		if rule.SourceDialect == source && rule.TargetDialect == target {
			translated = strings.ReplaceAll(translated, rule.SourcePattern, rule.TargetPattern)
		}
	}
	e.mu.RUnlock()

	// Remove backticks for non-MySQL targets
	if target != "mysql" {
		translated = strings.ReplaceAll(translated, "`", "")
	}

	// Handle LIMIT syntax differences
	if target == "mssql" && strings.Contains(strings.ToUpper(query), "LIMIT") {
		// Convert LIMIT to TOP for MSSQL
		re := regexp.MustCompile(`(?i)LIMIT\s+(\d+)`)
		translated = re.ReplaceAllString(translated, "")
	}

	return translated
}

// GetTranslationStats returns translation statistics
func (e *QueryTranslationEngine) GetTranslationStats() TranslationStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return TranslationStatsResponse{
		QueriesTranslated:     e.stats.QueriesTranslated,
		TranslationsFailed:   e.stats.TranslationsFailed,
		PartialTranslations:  e.stats.PartialTranslations,
		AvgLatencyMs:         e.stats.AvgLatencyMs,
	}
}

type TranslationStatsResponse struct {
	QueriesTranslated    int64   `json:"queries_translated"`
	TranslationsFailed   int64   `json:"translations_failed"`
	PartialTranslations  int64   `json:"partial_translations"`
	AvgLatencyMs         float64 `json:"avg_latency_ms"`
}

