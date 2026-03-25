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

// SchemaIntelligenceEngine provides schema change detection, compatibility checking, and version tracking
type SchemaIntelligenceEngine struct {
	BaseEngine
	config          *SchemaConfig
	schemaVersions  map[string]*SchemaVersion
	deprecatedItems map[string]DeprecatedItem
	tableMetadata   map[string]TableMetadata
	changeHistory   []SchemaChange
	mu              sync.RWMutex
}

type SchemaConfig struct {
	Enabled            bool          // Enable the engine
	CompatibilityCheck bool          // Check query compatibility after DDL
	DeprecationTracking bool         // Track deprecated columns/tables
	VersionTracking    bool          // Enable schema version history
	MaxHistorySize     int           // Maximum history entries
	AlertOnChange      bool          // Alert when schema changes detected
}

type SchemaVersion struct {
	Version     string
	Timestamp   time.Time
	Tables      []TableDefinition
	Changes     []SchemaChange
	MigrationID string
}

type TableDefinition struct {
	Name        string
	Columns     []ColumnDefinition
	Indexes     []IndexDefinition
	Constraints []ConstraintDefinition
}

type ColumnDefinition struct {
	Name       string
	Type       string
	Nullable   bool
	Default    string
	IsPrimary  bool
	IsForeign  bool
	References string
}

type IndexDefinition struct {
	Name    string
	Columns []string
	Type    string // B-TREE, HASH, GIN, etc.
	Unique  bool
}

type ConstraintDefinition struct {
	Name       string
	Type       string // PRIMARY KEY, FOREIGN KEY, UNIQUE, CHECK
	Columns    []string
	Reference  string
	Expression string
}

type SchemaChange struct {
	Timestamp   time.Time
	ChangeType  SchemaChangeType // CREATE_TABLE, ALTER_TABLE, DROP_TABLE, etc.
	TableName   string
	ItemName    string
	ItemType    string // COLUMN, INDEX, CONSTRAINT
	OldValue    string
	NewValue    string
	Description string
}

type SchemaChangeType string

const (
	SchemaCreateTable SchemaChangeType = "CREATE_TABLE"
	SchemaAlterTable  SchemaChangeType = "ALTER_TABLE"
	SchemaDropTable   SchemaChangeType = "DROP_TABLE"
	SchemaCreateColumn SchemaChangeType = "CREATE_COLUMN"
	SchemaAlterColumn SchemaChangeType = "ALTER_COLUMN"
	SchemaDropColumn  SchemaChangeType = "DROP_COLUMN"
	SchemaRenameTable SchemaChangeType = "RENAME_TABLE"
	SchemaRenameColumn SchemaChangeType = "RENAME_COLUMN"
	SchemaCreateIndex SchemaChangeType = "CREATE_INDEX"
	SchemaDropIndex   SchemaChangeType = "DROP_INDEX"
)

type DeprecatedItem struct {
	ItemName   string
	ItemType   string // TABLE, COLUMN, INDEX
	TableName  string
	DeprecatedAt time.Time
	RemovalDate time.Time
	Reason     string
	MigratedTo string
}

type TableMetadata struct {
	TableName       string
	LastDDLTime     time.Time
	ColumnCount     int
	IndexCount      int
	SizeBytes       int64
	RowCount        int64
	IsDeprecated    bool
	DeprecationInfo string
}

// NewSchemaIntelligenceEngine creates a new Schema Intelligence Engine
func NewSchemaIntelligenceEngine(config *SchemaConfig) *SchemaIntelligenceEngine {
	if config == nil {
		config = &SchemaConfig{
			Enabled:            true,
			CompatibilityCheck: true,
			DeprecationTracking: true,
			VersionTracking:    true,
			MaxHistorySize:     500,
			AlertOnChange:      true,
		}
	}

	engine := &SchemaIntelligenceEngine{
		BaseEngine:      BaseEngine{name: "schema_intelligence"},
		config:          config,
		schemaVersions:  make(map[string]*SchemaVersion),
		deprecatedItems: make(map[string]DeprecatedItem),
		tableMetadata:   make(map[string]TableMetadata),
		changeHistory:   make([]SchemaChange, 0),
	}

	return engine
}

// Process handles schema intelligence for queries
func (e *SchemaIntelligenceEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	// Detect DDL statements
	changeType := e.detectSchemaChange(query)
	if changeType != "" {
		e.mu.Lock()
		change := e.parseSchemaChange(query, changeType)
		e.changeHistory = append(e.changeHistory, change)
		// Keep history bounded
		if len(e.changeHistory) > e.config.MaxHistorySize {
			e.changeHistory = e.changeHistory[1:]
		}
		e.mu.Unlock()

		// Update metadata
		e.updateTableMetadata(query, changeType)

		// Check for compatibility issues with read queries
		if e.config.CompatibilityCheck && changeType != "" {
			// Add compatibility warnings to metadata
			if qc.Metadata == nil {
				qc.Metadata = make(map[string]interface{})
			}
			qc.Metadata["schema_changed"] = true
			qc.Metadata["change_type"] = string(changeType)
			qc.Metadata["change_impact"] = e.assessChangeImpact(change)
		}
	}

	// Check for deprecated items usage in SELECT queries
	if e.config.DeprecationTracking && strings.HasPrefix(strings.ToUpper(query), "SELECT") {
		warnings := e.checkDeprecatedUsage(query)
		if len(warnings) > 0 && qc.Metadata != nil {
			qc.Metadata["deprecation_warnings"] = warnings
		}
	}

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles response processing
func (e *SchemaIntelligenceEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// detectSchemaChange identifies if query is a DDL statement
func (e *SchemaIntelligenceEngine) detectSchemaChange(query string) SchemaChangeType {
	upper := strings.ToUpper(query)

	if strings.Contains(upper, "CREATE TABLE") {
		return SchemaCreateTable
	}
	if strings.Contains(upper, "ALTER TABLE") {
		return SchemaAlterTable
	}
	if strings.Contains(upper, "DROP TABLE") {
		return SchemaDropTable
	}
	if strings.Contains(upper, "CREATE INDEX") || strings.Contains(upper, "CREATE UNIQUE INDEX") {
		return SchemaCreateIndex
	}
	if strings.Contains(upper, "DROP INDEX") {
		return SchemaDropIndex
	}
	if strings.Contains(upper, "RENAME TABLE") {
		return SchemaRenameTable
	}

	return ""
}

// parseSchemaChange parses the DDL query into a schema change record
func (e *SchemaIntelligenceEngine) parseSchemaChange(query string, changeType SchemaChangeType) SchemaChange {
	change := SchemaChange{
		Timestamp:  time.Now(),
		ChangeType: changeType,
	}

	// Extract table name
	tableName := e.extractTableName(query)
	change.TableName = tableName

	switch changeType {
	case SchemaCreateTable:
		change.Description = fmt.Sprintf("Created table: %s", tableName)
		change.NewValue = query
	case SchemaAlterTable:
		// Extract alteration details
		if strings.Contains(strings.ToUpper(query), "ADD COLUMN") {
			change.ItemType = "COLUMN"
			colMatch := regexp.MustCompile(`ADD\s+COLUMN\s+(\w+)`).FindStringSubmatch(query)
			if len(colMatch) > 1 {
				change.ItemName = colMatch[1]
				change.Description = fmt.Sprintf("Added column %s to table %s", change.ItemName, tableName)
			}
		} else if strings.Contains(strings.ToUpper(query), "DROP COLUMN") {
			change.ItemType = "COLUMN"
			colMatch := regexp.MustCompile(`DROP\s+COLUMN\s+(\w+)`).FindStringSubmatch(query)
			if len(colMatch) > 1 {
				change.ItemName = colMatch[1]
				change.Description = fmt.Sprintf("Dropped column %s from table %s", change.ItemName, tableName)
			}
		} else if strings.Contains(strings.ToUpper(query), "MODIFY") || strings.Contains(strings.ToUpper(query), "CHANGE") {
			change.ItemType = "COLUMN"
			change.Description = fmt.Sprintf("Modified table %s", tableName)
		}
	case SchemaDropTable:
		change.Description = fmt.Sprintf("Dropped table: %s", tableName)
	case SchemaCreateIndex:
		idxMatch := regexp.MustCompile(`(?i)CREATE\s+(UNIQUE\s+)?INDEX\s+(\w+)`).FindStringSubmatch(query)
		if len(idxMatch) > 2 {
			change.ItemName = idxMatch[2]
			change.ItemType = "INDEX"
			change.Description = fmt.Sprintf("Created index %s on table %s", change.ItemName, tableName)
		}
	case SchemaDropIndex:
		idxMatch := regexp.MustCompile(`(?i)DROP\s+INDEX\s+(\w+)`).FindStringSubmatch(query)
		if len(idxMatch) > 1 {
			change.ItemName = idxMatch[1]
			change.ItemType = "INDEX"
			change.Description = fmt.Sprintf("Dropped index %s", change.ItemName)
		}
	}

	return change
}

// extractTableName extracts table name from query
func (e *SchemaIntelligenceEngine) extractTableName(query string) string {
	patterns := []string{
		`(?i)(?:CREATE\s+TABLE|ALTER\s+TABLE|DROP\s+TABLE)\s+(\w+)`,
		`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+\w+\s+ON\s+(\w+)`,
		`(?i)DROP\s+INDEX\s+\w+\s+ON\s+(\w+)`,
		`(?i)RENAME\s+TABLE\s+(\w+)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(query)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return "unknown"
}

// updateTableMetadata updates metadata when schema changes
func (e *SchemaIntelligenceEngine) updateTableMetadata(query string, changeType SchemaChangeType) {
	tableName := e.extractTableName(query)

	e.mu.Lock()
	defer e.mu.Unlock()

	if metadata, exists := e.tableMetadata[tableName]; exists {
		metadata.LastDDLTime = time.Now()
		e.tableMetadata[tableName] = metadata
	} else {
		e.tableMetadata[tableName] = TableMetadata{
			TableName:   tableName,
			LastDDLTime: time.Now(),
		}
	}
}

// checkDeprecatedUsage checks if query uses any deprecated items
func (e *SchemaIntelligenceEngine) checkDeprecatedUsage(query string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	warnings := make([]string, 0)
	queryLower := strings.ToLower(query)

	for key, item := range e.deprecatedItems {
		if strings.Contains(queryLower, strings.ToLower(item.ItemName)) {
			warnings = append(warnings, fmt.Sprintf(
				"DEPRECATED: %s '%s' - %s. Migrate to: %s",
				item.ItemType, item.ItemName, item.Reason, item.MigratedTo,
			))
			_ = key // Silence unused warning
		}
	}

	return warnings
}

// assessChangeImpact assesses the impact level of a schema change
func (e *SchemaIntelligenceEngine) assessChangeImpact(change SchemaChange) string {
	switch change.ChangeType {
	case SchemaDropTable, SchemaDropColumn:
		return "HIGH"
	case SchemaAlterTable:
		return "MEDIUM"
	case SchemaCreateTable, SchemaCreateIndex:
		return "LOW"
	default:
		return "UNKNOWN"
	}
}

// MarkAsDeprecated marks a table/column as deprecated
func (e *SchemaIntelligenceEngine) MarkAsDeprecated(itemName, itemType, tableName, reason, migratedTo string, removalDays int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := fmt.Sprintf("%s.%s.%s", itemType, tableName, itemName)
	e.deprecatedItems[key] = DeprecatedItem{
		ItemName:     itemName,
		ItemType:     itemType,
		TableName:    tableName,
		DeprecatedAt: time.Now(),
		RemovalDate:  time.Now().AddDate(0, 0, removalDays),
		Reason:       reason,
		MigratedTo:   migratedTo,
	}

	// Update table metadata
	if metadata, exists := e.tableMetadata[tableName]; exists {
		metadata.IsDeprecated = true
		metadata.DeprecationInfo = fmt.Sprintf("%s is deprecated: %s", itemName, reason)
		e.tableMetadata[tableName] = metadata
	}
}

// GetTableMetadata returns metadata for a table
func (e *SchemaIntelligenceEngine) GetTableMetadata(tableName string) (TableMetadata, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	metadata, exists := e.tableMetadata[tableName]
	return metadata, exists
}

// GetChangeHistory returns recent schema changes
func (e *SchemaIntelligenceEngine) GetChangeHistory(limit int) []SchemaChange {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit > len(e.changeHistory) {
		limit = len(e.changeHistory)
	}

	result := make([]SchemaChange, limit)
	copy(result, e.changeHistory[len(e.changeHistory)-limit:])
	return result
}

// GetDeprecationWarnings returns all deprecated items
func (e *SchemaIntelligenceEngine) GetDeprecationWarnings() []DeprecatedItemResponse {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make([]DeprecatedItemResponse, 0, len(e.deprecatedItems))
	for _, item := range e.deprecatedItems {
		result = append(result, DeprecatedItemResponse{
			ItemName:     item.ItemName,
			ItemType:     item.ItemType,
			TableName:    item.TableName,
			DeprecatedAt: item.DeprecatedAt,
			RemovalDate:  item.RemovalDate,
			Reason:       item.Reason,
			MigratedTo:   item.MigratedTo,
			DaysUntilRemoval: int(time.Until(item.RemovalDate).Hours() / 24),
		})
	}
	return result
}

// CheckQueryCompatibility checks if a query is compatible with current schema
func (e *SchemaIntelligenceEngine) CheckQueryCompatibility(query string) CompatibilityReport {
	report := CompatibilityReport{
		IsCompatible: true,
		Issues:       make([]string, 0),
		Suggestions:  make([]string, 0),
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	upper := strings.ToUpper(query)
	if !strings.HasPrefix(upper, "SELECT") {
		return report
	}

	// Check for columns in SELECT that might not exist
	colPattern := regexp.MustCompile(`(?i)(?:SELECT|WHERE|JOIN|ON)\s+([\w\.]+)`)
	matches := colPattern.FindAllStringSubmatch(query, -1)

	for _, match := range matches {
		if len(match) > 1 {
			parts := strings.Split(match[1], ".")
			if len(parts) == 2 {
				tableName := parts[0]
				_ = parts[1] // column name not currently used but reserved for future checks

				// Check if we have metadata for this table
				if metadata, exists := e.tableMetadata[tableName]; exists && metadata.IsDeprecated {
					report.IsCompatible = false
					report.Issues = append(report.Issues, fmt.Sprintf("Table '%s' is deprecated", tableName))
				}
			}
		}
	}

	return report
}

// Helper types for API responses
type DeprecatedItemResponse struct {
	ItemName        string    `json:"item_name"`
	ItemType        string    `json:"item_type"`
	TableName       string    `json:"table_name"`
	DeprecatedAt    time.Time `json:"deprecated_at"`
	RemovalDate     time.Time `json:"removal_date"`
	Reason          string    `json:"reason"`
	MigratedTo      string    `json:"migrated_to"`
	DaysUntilRemoval int       `json:"days_until_removal"`
}

type CompatibilityReport struct {
	IsCompatible bool     `json:"is_compatible"`
	Issues       []string `json:"issues"`
	Suggestions  []string `json:"suggestions"`
}