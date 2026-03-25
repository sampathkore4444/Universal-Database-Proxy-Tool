package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/udbp/udbproxy/pkg/types"
)

type QueryInspector struct {
	mu           sync.RWMutex
	queries      []LiveQuery
	maxQueries   int
	debugMode    bool
	queryChannel chan *types.QueryContext
}

type LiveQuery struct {
	ID           string                 `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	Query        string                 `json:"query"`
	Database     string                 `json:"database"`
	DatabaseType string                 `json:"database_type"`
	User         string                 `json:"user"`
	ClientIP     string                 `json:"client_ip"`
	Operation    string                 `json:"operation"`
	Status       string                 `json:"status"`
	Duration     time.Duration          `json:"duration"`
	Error        string                 `json:"error,omitempty"`
	ResponseSize int                    `json:"response_size"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

func NewQueryInspector(maxQueries int) *QueryInspector {
	return &QueryInspector{
		queries:      make([]LiveQuery, 0),
		maxQueries:   maxQueries,
		debugMode:    false,
		queryChannel: make(chan *types.QueryContext, 1000),
	}
}

func (qi *QueryInspector) RecordQuery(qc *types.QueryContext) {
	if !qi.debugMode && !qi.isDebugQuery(qc) {
		return
	}

	query := LiveQuery{
		ID:           qc.ID,
		Timestamp:    qc.Timestamp,
		Query:        qc.RawQuery,
		Database:     qc.Database,
		DatabaseType: string(qc.DatabaseType),
		User:         qc.User,
		ClientIP:     qc.ClientAddr,
		Operation:    string(qc.Operation),
		Duration:     time.Since(qc.Timestamp),
		Metadata:     qc.Metadata,
	}

	if qc.Response != nil {
		query.Status = "success"
		if qc.Response.Error != nil {
			query.Status = "error"
			query.Error = qc.Response.Error.Error()
		}
		if qc.Response.Data != nil {
			query.ResponseSize = len(qc.Response.Data)
		}
	} else {
		query.Status = "pending"
	}

	qi.mu.Lock()
	qi.queries = append(qi.queries, query)
	if len(qi.queries) > qi.maxQueries {
		qi.queries = qi.queries[len(qi.queries)-qi.maxQueries:]
	}
	qi.mu.Unlock()

	select {
	case qi.queryChannel <- qc:
	default:
	}
}

func (qi *QueryInspector) isDebugQuery(qc *types.QueryContext) bool {
	if qc.Metadata == nil {
		return false
	}
	debug, ok := qc.Metadata["debug"].(bool)
	return ok && debug
}

func (qi *QueryInspector) GetQueries(limit int) []LiveQuery {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	if limit <= 0 || limit > len(qi.queries) {
		limit = len(qi.queries)
	}

	result := make([]LiveQuery, limit)
	copy(result, qi.queries[len(qi.queries)-limit:])
	return result
}

func (qi *QueryInspector) GetQuery(id string) (*LiveQuery, bool) {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	for i := len(qi.queries) - 1; i >= 0; i-- {
		if qi.queries[i].ID == id {
			return &qi.queries[i], true
		}
	}
	return nil, false
}

func (qi *QueryInspector) GetQueriesByUser(user string) []LiveQuery {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	var result []LiveQuery
	for _, q := range qi.queries {
		if q.User == user {
			result = append(result, q)
		}
	}
	return result
}

func (qi *QueryInspector) GetQueriesByDatabase(database string) []LiveQuery {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	var result []LiveQuery
	for _, q := range qi.queries {
		if q.Database == database {
			result = append(result, q)
		}
	}
	return result
}

func (qi *QueryInspector) GetQueriesByTimeRange(start, end time.Time) []LiveQuery {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	var result []LiveQuery
	for _, q := range qi.queries {
		if q.Timestamp.After(start) && q.Timestamp.Before(end) {
			result = append(result, q)
		}
	}
	return result
}

func (qi *QueryInspector) GetSlowQueries(threshold time.Duration) []LiveQuery {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	var result []LiveQuery
	for _, q := range qi.queries {
		if q.Duration > threshold {
			result = append(result, q)
		}
	}
	return result
}

func (qi *QueryInspector) ClearQueries() {
	qi.mu.Lock()
	defer qi.mu.Unlock()
	qi.queries = make([]LiveQuery, 0)
}

func (qi *QueryInspector) SetDebugMode(enabled bool) {
	qi.mu.Lock()
	defer qi.mu.Unlock()
	qi.debugMode = enabled
}

func (qi *QueryInspector) IsDebugMode() bool {
	qi.mu.RLock()
	defer qi.mu.RUnlock()
	return qi.debugMode
}

func (qi *QueryInspector) GetQueryChannel() <-chan *types.QueryContext {
	return qi.queryChannel
}

func (qi *QueryInspector) ExportToJSON() (string, error) {
	qi.mu.RLock()
	defer qi.mu.RUnlock()

	data, err := json.MarshalIndent(qi.queries, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to export queries: %w", err)
	}

	return string(data), nil
}

type QueryInspectorAPI struct {
	inspector *QueryInspector
	mux       *http.ServeMux
}

func NewQueryInspectorAPI(inspector *QueryInspector) *QueryInspectorAPI {
	api := &QueryInspectorAPI{
		inspector: inspector,
		mux:       http.NewServeMux(),
	}

	api.registerRoutes()
	return api
}

func (api *QueryInspectorAPI) registerRoutes() {
	api.mux.HandleFunc("/queries", api.handleQueries)
	api.mux.HandleFunc("/queries/", api.handleQueryByID)
	api.mux.HandleFunc("/debug", api.handleDebug)
	api.mux.HandleFunc("/clear", api.handleClear)
	api.mux.HandleFunc("/slow", api.handleSlowQueries)
}

func (api *QueryInspectorAPI) handleQueries(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limit := 100
		if l := r.URL.Query().Get("limit"); l != "" {
			fmt.Sscanf(l, "%d", &limit)
		}
		queries := api.inspector.GetQueries(limit)
		json.NewEncoder(w).Encode(queries)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *QueryInspectorAPI) handleQueryByID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Path[len("/queries/"):]
		query, found := api.inspector.GetQuery(id)
		if !found {
			http.Error(w, "Query not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(query)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *QueryInspectorAPI) handleDebug(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]bool{"debug_mode": api.inspector.IsDebugMode()})
	case http.MethodPost:
		api.inspector.SetDebugMode(true)
		json.NewEncoder(w).Encode(map[string]string{"status": "debug_enabled"})
	case http.MethodDelete:
		api.inspector.SetDebugMode(false)
		json.NewEncoder(w).Encode(map[string]string{"status": "debug_disabled"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *QueryInspectorAPI) handleClear(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		api.inspector.ClearQueries()
		json.NewEncoder(w).Encode(map[string]string{"status": "cleared"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *QueryInspectorAPI) handleSlowQueries(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		threshold := 1 * time.Second
		if t := r.URL.Query().Get("threshold"); t != "" {
			d, err := time.ParseDuration(t)
			if err == nil {
				threshold = d
			}
		}
		queries := api.inspector.GetSlowQueries(threshold)
		json.NewEncoder(w).Encode(queries)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *QueryInspectorAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mux.ServeHTTP(w, r)
}

func (api *QueryInspectorAPI) StartDebugListener(ctx context.Context) {
	go func() {
		for {
			select {
			case qc := <-api.inspector.queryChannel:
				api.inspector.RecordQuery(qc)
			case <-ctx.Done():
				return
			}
		}
	}()
}
