package engines

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/udbp/udbproxy/pkg/types"
)

// DataValidationEngine validates data against business rules before INSERT/UPDATE
type DataValidationEngine struct {
	BaseEngine
	config       *ValidationConfig
	rules        map[string]*ValidationRule
	constraints  []*ColumnConstraint
	stats        *ValidationStats
	mu           sync.RWMutex
}

type ValidationConfig struct {
	Enabled       bool
	StrictMode    bool // Block on validation failure
	RulesEngine   string // regex, builtin, external
}

type ValidationRule struct {
	Name        string
	Table       string
	Column      string
	Type        string // range, regex, enum, custom
	Expression  string
	Message     string
	Priority    int
	Enabled     bool
}

type ColumnConstraint struct {
	Table        string
	Column       string
	ConstraintType string // not_null, unique, check, primary_key
	Expression   string
}

type ValidationStats struct {
	ValidationsRun   int64
	ValidationsPassed int64
	ValidationsFailed int64
	BlockedQueries   int64
	mu               sync.RWMutex
}

// NewDataValidationEngine creates a new Data Validation Engine
func NewDataValidationEngine(config *ValidationConfig) *DataValidationEngine {
	if config == nil {
		config = &ValidationConfig{
			Enabled:    false,
			StrictMode: true,
		}
	}

	engine := &DataValidationEngine{
		BaseEngine:  BaseEngine{name: "data_validation"},
		config:      config,
		rules:       make(map[string]*ValidationRule),
		constraints: make([]*ColumnConstraint, 0),
		stats:       &ValidationStats{},
	}

	return engine
}

// AddValidationRule adds a validation rule
func (e *DataValidationEngine) AddValidationRule(rule *ValidationRule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rule.Enabled = true
	e.rules[rule.Name] = rule
}

// Process validates data before execution
func (e *DataValidationEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if !e.config.Enabled {
		return types.EngineResult{Continue: true}
	}

	query := strings.TrimSpace(qc.RawQuery)
	if query == "" {
		return types.EngineResult{Continue: true}
	}

	upperQuery := strings.ToUpper(query)
	
	// Only validate INSERT and UPDATE
	if !strings.HasPrefix(upperQuery, "INSERT") && !strings.HasPrefix(upperQuery, "UPDATE") {
		return types.EngineResult{Continue: true}
	}

	// Extract table and values
	table := e.extractTable(query)
	if table == "" {
		return types.EngineResult{Continue: true}
	}

	// Find applicable rules
	rules := e.getRulesForTable(table)
	if len(rules) == 0 {
		return types.EngineResult{Continue: true}
	}

	// Extract values to validate
	values := e.extractValues(query)

	// Validate against rules
	validationErrors := e.validateValues(table, rules, values)

	e.stats.mu.Lock()
	e.stats.ValidationsRun++
	
	if len(validationErrors) > 0 {
		e.stats.ValidationsFailed++
		
		if e.config.StrictMode {
			e.stats.BlockedQueries++
			e.stats.mu.Unlock()
			return types.EngineResult{
				Continue: false,
				Error:    fmt.Errorf("validation failed: %s", strings.Join(validationErrors, "; ")),
			}
		}
	} else {
		e.stats.ValidationsPassed++
	}
	e.stats.mu.Unlock()

	// Store validation result in metadata
	if qc.Metadata == nil {
		qc.Metadata = make(map[string]interface{})
	}
	qc.Metadata["validation_passed"] = len(validationErrors) == 0
	qc.Metadata["validation_errors"] = validationErrors

	return types.EngineResult{Continue: true}
}

// ProcessResponse handles validation response
func (e *DataValidationEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	return types.EngineResult{Continue: true}
}

// extractTable extracts table name from query
func (e *DataValidationEngine) extractTable(query string) string {
	re := regexp.MustCompile(`(?i)(?:INTO|UPDATE)\s+(\w+)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractValues extracts values from INSERT/UPDATE
func (e *DataValidationEngine) extractValues(query string) map[string]interface{} {
	values := make(map[string]interface{})
	
	// Extract VALUES clause for INSERT
	re := regexp.MustCompile(`(?i)VALUES\s*\(([^)]+)\)`)
	matches := re.FindStringSubmatch(query)
	if len(matches) > 1 {
		// Parse comma-separated values
		parts := strings.Split(matches[1], ",")
		for i, part := range parts {
			values[fmt.Sprintf("col_%d", i)] = strings.Trim(part, " '\"")
		}
	}
	
	// Extract SET clause for UPDATE
	re = regexp.MustCompile(`(?i)SET\s+(.+?)(?:WHERE|$)`)
	matches = re.FindStringSubmatch(query)
	if len(matches) > 1 {
		setParts := strings.Split(matches[1], ",")
		for _, part := range setParts {
			kv := strings.Split(strings.TrimSpace(part), "=")
			if len(kv) == 2 {
				values[strings.Trim(kv[0], " ")] = strings.Trim(kv[1], " '\"")
			}
		}
	}
	
	return values
}

// getRulesForTable gets validation rules for a table
func (e *DataValidationEngine) getRulesForTable(table string) []*ValidationRule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var tableRules []*ValidationRule
	for _, rule := range e.rules {
		if rule.Table == table && rule.Enabled {
			tableRules = append(tableRules, rule)
		}
	}
	return tableRules
}

// validateValues validates values against rules
func (e *DataValidationEngine) validateValues(table string, rules []*ValidationRule, values map[string]interface{}) []string {
	var errors []string

	for _, rule := range rules {
		value, exists := values[rule.Column]
		if !exists {
			// Check for column in indexed values
			continue
		}

		strValue, ok := value.(string)
		if !ok {
			continue
		}

		switch rule.Type {
		case "regex":
			re := regexp.MustCompile(rule.Expression)
			if !re.MatchString(strValue) {
				errors = append(errors, fmt.Sprintf("%s: %s", rule.Name, rule.Message))
			}
		case "range":
			// Parse range expression like "0-100" or "1-10"
			parts := strings.Split(rule.Expression, "-")
			if len(parts) == 2 {
				var minVal, maxVal, val float64
				fmt.Sscanf(parts[0], "%f", &minVal)
				fmt.Sscanf(parts[1], "%f", &maxVal)
				fmt.Sscanf(strValue, "%f", &val)
				if val < minVal || val > maxVal {
					errors = append(errors, fmt.Sprintf("%s: %s", rule.Name, rule.Message))
				}
			}
		case "enum":
			// Check if value is in enum list
			enumValues := strings.Split(rule.Expression, ",")
			found := false
			for _, ev := range enumValues {
				if strings.Trim(ev, " ") == strValue {
					found = true
					break
				}
			}
			if !found {
				errors = append(errors, fmt.Sprintf("%s: %s", rule.Name, rule.Message))
			}
		}
	}

	return errors
}

// GetValidationStats returns validation statistics
func (e *DataValidationEngine) GetValidationStats() ValidationStatsResponse {
	e.stats.mu.RLock()
	defer e.stats.mu.RUnlock()

	return ValidationStatsResponse{
		ValidationsRun:    e.stats.ValidationsRun,
		ValidationsPassed: e.stats.ValidationsPassed,
		ValidationsFailed: e.stats.ValidationsFailed,
		BlockedQueries:    e.stats.BlockedQueries,
	}
}

type ValidationStatsResponse struct {
	ValidationsRun    int64 `json:"validations_run"`
	ValidationsPassed int64 `json:"validations_passed"`
	ValidationsFailed int64 `json:"validations_failed"`
	BlockedQueries    int64 `json:"blocked_queries"`
}