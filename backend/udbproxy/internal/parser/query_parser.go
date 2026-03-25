package parser

import (
	"fmt"
	"regexp"
	"strings"
)

type QueryAST struct {
	Type       string
	Database   string
	Table      string
	Columns    []string
	Conditions []Condition
	Operations []string
	Subqueries []QueryAST
	Raw        string
}

type Condition struct {
	Column    string
	Operator  string
	Value     string
	Connector string
}

type QueryParser struct {
	selectRegex    *regexp.Regexp
	fromRegex      *regexp.Regexp
	whereRegex     *regexp.Regexp
	insertRegex    *regexp.Regexp
	updateRegex    *regexp.Regexp
	deleteRegex    *regexp.Regexp
	joinRegex      *regexp.Regexp
	conditionRegex *regexp.Regexp
}

func NewQueryParser() *QueryParser {
	return &QueryParser{
		selectRegex:    regexp.MustCompile(`(?i)SELECT\s+(.+?)\s+FROM`),
		fromRegex:      regexp.MustCompile(`(?i)FROM\s+([a-zA-Z0-9_\.\-]+)`),
		whereRegex:     regexp.MustCompile(`(?i)WHERE\s+(.+?)(?:\s+ORDER|\s+LIMIT|\s+GROUP|$)`),
		insertRegex:    regexp.MustCompile(`(?i)INSERT\s+INTO\s+([a-zA-Z0-9_\.\-]+)`),
		updateRegex:    regexp.MustCompile(`(?i)UPDATE\s+([a-zA-Z0-9_\.\-]+)`),
		deleteRegex:    regexp.MustCompile(`(?i)DELETE\s+FROM\s+([a-zA-Z0-9_\.\-]+)`),
		joinRegex:      regexp.MustCompile(`(?i)JOIN\s+([a-zA-Z0-9_\.\-]+)`),
		conditionRegex: regexp.MustCompile(`([a-zA-Z0-9_\.]+)\s*(=|!=|<>|<|>|>=|<=|LIKE|IN|BETWEEN)\s*'?([^']+?)'?(?:\s+AND|\s+OR|$)`),
	}
}

func (qp *QueryParser) Parse(query string) *QueryAST {
	ast := &QueryAST{
		Raw: query,
	}

	query = strings.TrimSpace(query)

	if strings.HasPrefix(strings.ToUpper(query), "SELECT") {
		ast.Type = "SELECT"
		qp.parseSelect(ast, query)
	} else if strings.HasPrefix(strings.ToUpper(query), "INSERT") {
		ast.Type = "INSERT"
		qp.parseInsert(ast, query)
	} else if strings.HasPrefix(strings.ToUpper(query), "UPDATE") {
		ast.Type = "UPDATE"
		qp.parseUpdate(ast, query)
	} else if strings.HasPrefix(strings.ToUpper(query), "DELETE") {
		ast.Type = "DELETE"
		qp.parseDelete(ast, query)
	} else if strings.HasPrefix(strings.ToUpper(query), "CREATE") {
		ast.Type = "CREATE"
	} else if strings.HasPrefix(strings.ToUpper(query), "DROP") {
		ast.Type = "DROP"
	} else if strings.HasPrefix(strings.ToUpper(query), "ALTER") {
		ast.Type = "ALTER"
	} else {
		ast.Type = "OTHER"
	}

	return ast
}

func (qp *QueryParser) parseSelect(ast *QueryAST, query string) {
	if match := qp.selectRegex.FindStringSubmatch(query); len(match) > 1 {
		columns := strings.Split(match[1], ",")
		for i := range columns {
			columns[i] = strings.TrimSpace(columns[i])
			if strings.Contains(strings.ToUpper(columns[i]), " AS ") {
				parts := strings.Split(columns[i], " AS ")
				columns[i] = strings.TrimSpace(parts[1])
			}
		}
		ast.Columns = columns
	}

	if match := qp.fromRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Table = match[1]
	}

	if match := qp.whereRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Conditions = qp.parseConditions(match[1])
	}

	joinMatches := qp.joinRegex.FindAllStringSubmatch(query, -1)
	for _, m := range joinMatches {
		if len(m) > 1 {
			ast.Operations = append(ast.Operations, "JOIN:"+m[1])
		}
	}
}

func (qp *QueryParser) parseInsert(ast *QueryAST, query string) {
	if match := qp.insertRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Table = match[1]
	}

	re := regexp.MustCompile(`(?i)\(([^)]+)\)\s*VALUES\s*\(([^)]+)\)`)
	if match := re.FindStringSubmatch(query); len(match) > 2 {
		columns := strings.Split(match[1], ",")
		values := strings.Split(match[2], ",")
		for i := range columns {
			columns[i] = strings.TrimSpace(columns[i])
			values[i] = strings.TrimSpace(values[i])
		}
		ast.Columns = columns
		ast.Operations = values
	}
}

func (qp *QueryParser) parseUpdate(ast *QueryAST, query string) {
	if match := qp.updateRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Table = match[1]
	}

	re := regexp.MustCompile(`(?i)SET\s+(.+?)\s+WHERE`)
	if match := re.FindStringSubmatch(query); len(match) > 1 {
		sets := strings.Split(match[1], ",")
		for _, s := range sets {
			parts := strings.Split(s, "=")
			if len(parts) == 2 {
				ast.Operations = append(ast.Operations, strings.TrimSpace(parts[0]))
			}
		}
	}

	if match := qp.whereRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Conditions = qp.parseConditions(match[1])
	}
}

func (qp *QueryParser) parseDelete(ast *QueryAST, query string) {
	if match := qp.deleteRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Table = match[1]
	}

	if match := qp.whereRegex.FindStringSubmatch(query); len(match) > 1 {
		ast.Conditions = qp.parseConditions(match[1])
	}
}

func (qp *QueryParser) parseConditions(whereClause string) []Condition {
	var conditions []Condition

	matches := qp.conditionRegex.FindAllStringSubmatch(whereClause, -1)
	for i, m := range matches {
		if len(m) > 3 {
			cond := Condition{
				Column:   strings.TrimSpace(m[1]),
				Operator: strings.TrimSpace(m[2]),
				Value:    strings.TrimSpace(m[3]),
			}

			if i > 0 {
				re := regexp.MustCompile(`(?i)\s+(AND|OR)\s+`)
				parts := re.Split(whereClause, -1)
				if len(parts) > i {
					conn := strings.TrimSpace(parts[i])
					if strings.HasPrefix(strings.ToUpper(conn), "AND") {
						cond.Connector = "AND"
					} else if strings.HasPrefix(strings.ToUpper(conn), "OR") {
						cond.Connector = "OR"
					}
				}
			}

			conditions = append(conditions, cond)
		}
	}

	return conditions
}

func (ast *QueryAST) String() string {
	return fmt.Sprintf("QueryAST{Type:%s Table:%s Columns:%v Conditions:%v}",
		ast.Type, ast.Table, ast.Columns, ast.Conditions)
}

func (qp *QueryParser) ExtractTables(query string) []string {
	var tables []string

	fromMatch := qp.fromRegex.FindStringSubmatch(query)
	if len(fromMatch) > 1 {
		tables = append(tables, fromMatch[1])
	}

	joinMatches := qp.joinRegex.FindAllStringSubmatch(query, -1)
	for _, m := range joinMatches {
		if len(m) > 1 {
			tables = append(tables, m[1])
		}
	}

	insertMatch := qp.insertRegex.FindStringSubmatch(query)
	if len(insertMatch) > 1 {
		tables = append(tables, insertMatch[1])
	}

	updateMatch := qp.updateRegex.FindStringSubmatch(query)
	if len(updateMatch) > 1 {
		tables = append(tables, updateMatch[1])
	}

	deleteMatch := qp.deleteRegex.FindStringSubmatch(query)
	if len(deleteMatch) > 1 {
		tables = append(tables, deleteMatch[1])
	}

	return tables
}

func (qp *QueryParser) IsReadOnly(query string) bool {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(upperQuery, "SELECT") ||
		strings.HasPrefix(upperQuery, "SHOW") ||
		strings.HasPrefix(upperQuery, "DESCRIBE") ||
		strings.HasPrefix(upperQuery, "EXPLAIN")
}

func (qp *QueryParser) IsWriteQuery(query string) bool {
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	return strings.HasPrefix(upperQuery, "INSERT") ||
		strings.HasPrefix(upperQuery, "UPDATE") ||
		strings.HasPrefix(upperQuery, "DELETE") ||
		strings.HasPrefix(upperQuery, "CREATE") ||
		strings.HasPrefix(upperQuery, "DROP") ||
		strings.HasPrefix(upperQuery, "ALTER") ||
		strings.HasPrefix(upperQuery, "TRUNCATE")
}

func (qp *QueryParser) GetQueryComplexity(query string) string {
	ast := qp.Parse(query)

	if len(ast.Subqueries) > 0 {
		return "complex"
	}

	if len(ast.Conditions) > 3 {
		return "medium"
	}

	if ast.WhereHasSubquery(query) {
		return "medium"
	}

	return "simple"
}

func (ast *QueryAST) WhereHasSubquery(query string) bool {
	return regexp.MustCompile(`(?i)\bWHERE\b.*\(.*SELECT.*\)`).MatchString(query)
}
