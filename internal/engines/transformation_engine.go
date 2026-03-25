package engines

import (
	"context"
	"regexp"
	"strings"

	"github.com/udbp/udbproxy/pkg/types"
)

type TransformationEngine struct {
	BaseEngine
	rules     []types.TransformRule
	schemaMap map[string]string
}

func NewTransformationEngine(rules []types.TransformRule) *TransformationEngine {
	return &TransformationEngine{
		BaseEngine: BaseEngine{name: "transformation"},
		rules:      rules,
		schemaMap:  make(map[string]string),
	}
}

func (e *TransformationEngine) Process(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.RawQuery == "" {
		return types.EngineResult{Continue: true}
	}

	qc.NormalizedQuery = qc.RawQuery

	for _, rule := range e.rules {
		if e.matchRule(qc.RawQuery, rule) {
			result := e.applyRule(qc, rule)
			if !result.Continue {
				return result
			}
		}
	}

	if e.schemaMap != nil {
		qc.NormalizedQuery = e.applySchemaMapping(qc.NormalizedQuery)
	}

	return types.EngineResult{Continue: true}
}

func (e *TransformationEngine) ProcessResponse(ctx context.Context, qc *types.QueryContext) types.EngineResult {
	if qc.Response == nil || qc.Response.Data == nil {
		return types.EngineResult{Continue: true}
	}

	transformFields, ok := qc.Metadata["transform_fields"].([]string)
	if !ok || len(transformFields) == 0 {
		return types.EngineResult{Continue: true}
	}

	for _, field := range transformFields {
		for rowIdx := range qc.Response.Data {
			for colIdx, col := range qc.Response.Columns {
				if col == field && rowIdx < len(qc.Response.Data[rowIdx]) && colIdx < len(qc.Response.Data[rowIdx]) {
					if val, ok := qc.Response.Data[rowIdx][colIdx].(string); ok {
						qc.Response.Data[rowIdx][colIdx] = e.transformValue(val, field)
					}
				}
			}
		}
	}

	return types.EngineResult{Continue: true}
}

func (e *TransformationEngine) matchRule(query string, rule types.TransformRule) bool {
	if rule.MatchPattern == "" {
		return false
	}

	pattern := "(?i)" + rule.MatchPattern
	matched, err := regexp.MatchString(pattern, query)
	return err == nil && matched
}

func (e *TransformationEngine) applyRule(qc *types.QueryContext, rule types.TransformRule) types.EngineResult {
	switch rule.Action {
	case types.TransformActionRewrite:
		qc.NormalizedQuery = e.rewriteQuery(qc.RawQuery, rule.MatchPattern, rule.ReplaceWith)
	case types.TransformActionAbstract:
		qc.NormalizedQuery = e.abstractQuery(qc.RawQuery, rule.MatchPattern)
	case types.TransformActionMask:
		qc.Metadata["mask_fields"] = rule.MaskFields
	case types.TransformActionRemove:
		qc.NormalizedQuery = e.removePattern(qc.RawQuery, rule.MatchPattern)
	}

	return types.EngineResult{Continue: true}
}

func (e *TransformationEngine) rewriteQuery(query, pattern, replacement string) string {
	re := regexp.MustCompile("(?i)" + pattern)
	return re.ReplaceAllString(query, replacement)
}

func (e *TransformationEngine) abstractQuery(query, pattern string) string {
	re := regexp.MustCompile("(?i)" + pattern)
	return re.ReplaceAllStringFunc(query, func(match string) string {
		return "?"
	})
}

func (e *TransformationEngine) removePattern(query, pattern string) string {
	re := regexp.MustCompile("(?i)" + pattern)
	return re.ReplaceAllString(query, "")
}

func (e *TransformationEngine) applySchemaMapping(query string) string {
	for oldSchema, newSchema := range e.schemaMap {
		query = strings.ReplaceAll(query, oldSchema+".", newSchema+".")
	}
	return query
}

func (e *TransformationEngine) transformValue(value, fieldType string) string {
	switch fieldType {
	case "email":
		parts := strings.Split(value, "@")
		if len(parts) == 2 && len(parts[0]) > 0 {
			return parts[0][:1] + "***@" + parts[1]
		}
	case "phone":
		if len(value) > 4 {
			return "***-" + value[len(value)-4:]
		}
	case "credit_card":
		if len(value) > 4 {
			return "****-****-****-" + value[len(value)-4:]
		}
	case "ssn":
		if len(value) > 4 {
			return "***-**-" + value[len(value)-4:]
		}
	}
	return value
}

func (e *TransformationEngine) AddRule(rule types.TransformRule) {
	e.rules = append(e.rules, rule)
}

func (e *TransformationEngine) RemoveRule(ruleName string) {
	var newRules []types.TransformRule
	for _, r := range e.rules {
		if r.Name != ruleName {
			newRules = append(newRules, r)
		}
	}
	e.rules = newRules
}

func (e *TransformationEngine) AddSchemaMapping(source, target string) {
	e.schemaMap[source] = target
}

func (e *TransformationEngine) RemoveSchemaMapping(source string) {
	delete(e.schemaMap, source)
}

func (e *TransformationEngine) GetSchemaMappings() map[string]string {
	return e.schemaMap
}
