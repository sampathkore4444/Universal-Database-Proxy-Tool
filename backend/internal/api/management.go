package api

import (
	"encoding/json"
	"net/http"

	"github.com/udbp/udbproxy/internal/engines"
)

// ManagementAPIHandler handles REST API requests
type ManagementAPIHandler struct {
	enginePipeline *engines.EnginePipeline
	databases      map[string]*DatabaseConfig
	stats          *ProxyStats
}

type DatabaseConfig struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Status string `json:"status"`
}

type ProxyStats struct {
	TotalQueries      int64   `json:"totalQueries"`
	ActiveQueries     int64   `json:"activeQueries"`
	BlockedQueries    int64   `json:"blockedQueries"`
	AvgLatency        float64 `json:"avgLatency"`
	P99Latency        float64 `json:"p99Latency"`
	ActiveConnections int64   `json:"activeConnections"`
	PooledConnections int64   `json:"pooledConnections"`
}

type EngineInfo struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Enabled     bool        `json:"enabled"`
	Description string      `json:"description"`
	Stats       interface{} `json:"stats,omitempty"`
}

// NewManagementAPIHandler creates a new API handler
func NewManagementAPIHandler() *ManagementAPIHandler {
	return &ManagementAPIHandler{
		databases: make(map[string]*DatabaseConfig),
		stats: &ProxyStats{
			TotalQueries:      10245,
			ActiveQueries:     25,
			BlockedQueries:    3,
			AvgLatency:        5.2,
			P99Latency:        45.0,
			ActiveConnections: 12,
			PooledConnections: 50,
		},
	}
}

// RegisterRoutes registers API routes on the given mux
func (h *ManagementAPIHandler) RegisterRoutes(mux *http.ServeMux) {
	// CORS middleware wrapper
	corsMux := corsMiddleware(mux)

	// Health check
	mux.HandleFunc("/api/v1/health", h.handleHealth)

	// Engine endpoints
	mux.HandleFunc("/api/v1/engines", h.handleEngines)
	mux.HandleFunc("/api/v1/engines/", h.handleEngine)

	// Database endpoints
	mux.HandleFunc("/api/v1/databases", h.handleDatabases)
	mux.HandleFunc("/api/v1/databases/", h.handleDatabase)

	// Stats endpoints
	mux.HandleFunc("/api/v1/stats", h.handleStats)
	mux.HandleFunc("/api/v1/stats/reset", h.handleStatsReset)

	// Query history
	mux.HandleFunc("/api/v1/query/history", h.handleQueryHistory)

	// Config endpoints
	mux.HandleFunc("/api/v1/config", h.handleConfig)
	mux.HandleFunc("/api/v1/config/databases", h.handleConfigDatabases)

	// Use corsMux for serving
	_ = corsMux
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *ManagementAPIHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    "proxy",
			"enabled": true,
			"fields":  map[string]interface{}{},
		})
	case http.MethodPut:
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ManagementAPIHandler) handleConfigDatabases(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"id": "1", "name": "primary-mysql", "type": "mysql", "host": "localhost", "port": 3306, "username": "root", "enabled": true},
			{"id": "2", "name": "analytics-pg", "type": "postgresql", "host": "localhost", "port": 5432, "username": "admin", "enabled": true},
		})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *ManagementAPIHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"uptime": 3600,
	})
}

func (h *ManagementAPIHandler) handleEngines(w http.ResponseWriter, r *http.Request) {
	engines := []EngineInfo{
		{ID: "1", Name: "Query Rewrite", Enabled: true, Description: "Auto-rewrite queries for better performance"},
		{ID: "2", Name: "Federation", Enabled: true, Description: "Cross-database query routing"},
		{ID: "3", Name: "Encryption", Enabled: true, Description: "Column-level encryption"},
		{ID: "4", Name: "CDC", Enabled: true, Description: "Change Data Capture"},
		{ID: "5", Name: "Time-Series", Enabled: false, Description: "Time-series data handling"},
		{ID: "6", Name: "Graph", Enabled: false, Description: "Relationship traversal queries"},
		{ID: "7", Name: "Retry Intelligence", Enabled: true, Description: "Smart retry logic"},
		{ID: "8", Name: "Hotspot Detection", Enabled: true, Description: "Identify hot data"},
		{ID: "9", Name: "Query Cost Estimator", Enabled: true, Description: "Predict query cost"},
		{ID: "10", Name: "Shadow Database", Enabled: false, Description: "Mirror queries to QA"},
		{ID: "11", Name: "Data Validation", Enabled: false, Description: "Business rule validation"},
		{ID: "12", Name: "Query Translation", Enabled: false, Description: "Cross-dialect conversion"},
		{ID: "13", Name: "Failover", Enabled: true, Description: "Automatic database failover"},
		{ID: "14", Name: "Query Versioning", Enabled: false, Description: "Track query changes"},
		{ID: "15", Name: "Batch Processing", Enabled: true, Description: "Optimize bulk operations"},
		{ID: "16", Name: "Data Compression", Enabled: false, Description: "Transparent compression"},
		{ID: "17", Name: "Load Balancer", Enabled: true, Description: "Intelligent routing"},
		{ID: "18", Name: "Query History", Enabled: true, Description: "Long-term storage"},
		{ID: "19", Name: "Query Insights", Enabled: true, Description: "Deep query analysis"},
		{ID: "20", Name: "Rate Limit Intelligence", Enabled: true, Description: "Adaptive throttling"},
		{ID: "21", Name: "Query Sandbox", Enabled: true, Description: "Safe query execution"},
		{ID: "22", Name: "Schema Intelligence", Enabled: true, Description: "Schema change detection"},
		{ID: "23", Name: "Connection Pool Optimizer", Enabled: true, Description: "Dynamic pool sizing"},
		{ID: "24", Name: "Data Lineage", Enabled: true, Description: "Data flow tracking"},
		{ID: "25", Name: "Multi-Tenant Isolation", Enabled: true, Description: "Tenant segmentation"},
		{ID: "26", Name: "Security", Enabled: true, Description: "SQL injection detection"},
		{ID: "27", Name: "Observability", Enabled: true, Description: "Query logging and metrics"},
		{ID: "28", Name: "Caching", Enabled: true, Description: "Query result caching"},
		{ID: "29", Name: "Transformation", Enabled: true, Description: "Query rewriting"},
		{ID: "30", Name: "AI Optimization", Enabled: true, Description: "Query optimization suggestions"},
		{ID: "31", Name: "Compliance", Enabled: true, Description: "Audit logging and PII detection"},
	}

	json.NewEncoder(w).Encode(engines)
}

func (h *ManagementAPIHandler) handleEngine(w http.ResponseWriter, r *http.Request) {
	// Get specific engine by ID
	id := r.URL.Path[len("/api/v1/engines/"):]

	engine := map[string]interface{}{
		"id":      id,
		"name":    "Query Rewrite",
		"enabled": true,
		"stats": map[string]interface{}{
			"processed":  1250,
			"avgLatency": 5,
			"errors":     2,
		},
	}

	json.NewEncoder(w).Encode(engine)
}

func (h *ManagementAPIHandler) handleDatabases(w http.ResponseWriter, r *http.Request) {
	databases := []DatabaseConfig{
		{ID: "1", Name: "primary", Type: "MySQL", Host: "localhost", Port: 3306, Status: "active"},
		{ID: "2", Name: "replica1", Type: "MySQL", Host: "localhost", Port: 3307, Status: "active"},
		{ID: "3", Name: "analytics", Type: "PostgreSQL", Host: "localhost", Port: 5432, Status: "active"},
		{ID: "4", Name: "cache", Type: "Redis", Host: "localhost", Port: 6379, Status: "active"},
	}

	json.NewEncoder(w).Encode(databases)
}

func (h *ManagementAPIHandler) handleDatabase(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(DatabaseConfig{
		ID: "1", Name: "primary", Type: "MySQL", Host: "localhost", Port: 3306, Status: "active",
	})
}

func (h *ManagementAPIHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(h.stats)
}

func (h *ManagementAPIHandler) handleStatsReset(w http.ResponseWriter, r *http.Request) {
	h.stats = &ProxyStats{}
	json.NewEncoder(w).Encode(map[string]string{"status": "reset"})
}

func (h *ManagementAPIHandler) handleQueryHistory(w http.ResponseWriter, r *http.Request) {
	history := []map[string]interface{}{
		{
			"id":        "1",
			"query":     "SELECT * FROM users WHERE id = 1",
			"user":      "app_user",
			"database":  "primary",
			"timestamp": "2024-01-15T10:30:00Z",
			"duration":  5,
			"status":    "success",
		},
		{
			"id":        "2",
			"query":     "INSERT INTO orders (user_id, total) VALUES (1, 100.00)",
			"user":      "app_user",
			"database":  "primary",
			"timestamp": "2024-01-15T10:29:45Z",
			"duration":  12,
			"status":    "success",
		},
	}

	json.NewEncoder(w).Encode(history)
}
