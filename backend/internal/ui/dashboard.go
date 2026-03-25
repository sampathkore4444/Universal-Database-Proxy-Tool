package ui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

type DashboardData struct {
	ServerName        string         `json:"server_name"`
	Uptime            time.Duration  `json:"uptime"`
	Version           string         `json:"version"`
	Status            string         `json:"status"`
	TotalQueries      int64          `json:"total_queries"`
	ActiveConnections int64          `json:"active_connections"`
	DatabaseStats     []DatabaseStat `json:"database_stats"`
	RecentErrors      []ErrorEntry   `json:"recent_errors"`
}

type DatabaseStat struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	Queries    int64   `json:"queries"`
	AvgLatency float64 `json:"avg_latency"`
}

type ErrorEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
	Database  string    `json:"database"`
}

var dashboardTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>UDBP Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0f172a; color: #e2e8f0; }
        .header { background: #1e293b; padding: 1rem 2rem; border-bottom: 1px solid #334155; }
        .header h1 { font-size: 1.5rem; color: #38bdf8; }
        .container { max-width: 1400px; margin: 0 auto; padding: 2rem; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 1.5rem; margin-bottom: 2rem; }
        .stat-card { background: #1e293b; border-radius: 0.5rem; padding: 1.5rem; border: 1px solid #334155; }
        .stat-card h3 { font-size: 0.875rem; color: #94a3b8; margin-bottom: 0.5rem; }
        .stat-card .value { font-size: 2rem; font-weight: bold; color: #38bdf8; }
        .section { background: #1e293b; border-radius: 0.5rem; padding: 1.5rem; border: 1px solid #334155; margin-bottom: 2rem; }
        .section h2 { font-size: 1.25rem; margin-bottom: 1rem; color: #f1f5f9; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 0.75rem; text-align: left; border-bottom: 1px solid #334155; }
        th { color: #94a3b8; font-weight: 600; font-size: 0.875rem; }
        .status-ok { color: #22c55e; }
        .status-error { color: #ef4444; }
        .refresh { float: right; color: #38bdf8; cursor: pointer; }
    </style>
</head>
<body>
    <div class="header">
        <h1>UDBP - Universal Database Proxy</h1>
    </div>
    <div class="container">
        <div class="stats-grid">
            <div class="stat-card">
                <h3>Status</h3>
                <div class="value status-ok">{{.Status}}</div>
            </div>
            <div class="stat-card">
                <h3>Uptime</h3>
                <div class="value">{{.Uptime}}</div>
            </div>
            <div class="stat-card">
                <h3>Total Queries</h3>
                <div class="value">{{.TotalQueries}}</div>
            </div>
            <div class="stat-card">
                <h3>Active Connections</h3>
                <div class="value">{{.ActiveConnections}}</div>
            </div>
        </div>

        <div class="section">
            <h2>Database Connections <span class="refresh" onclick="refreshData()">Refresh</span></h2>
            <table>
                <thead>
                    <tr>
                        <th>Name</th>
                        <th>Type</th>
                        <th>Status</th>
                        <th>Queries</th>
                        <th>Avg Latency</th>
                    </tr>
                </thead>
                <tbody>
                    {{range .DatabaseStats}}
                    <tr>
                        <td>{{.Name}}</td>
                        <td>{{.Type}}</td>
                        <td class="status-{{.Status}}">{{.Status}}</td>
                        <td>{{.Queries}}</td>
                        <td>{{.AvgLatency}}ms</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>

        <div class="section">
            <h2>Recent Errors</h2>
            <table>
                <thead>
                    <tr>
                        <th>Timestamp</th>
                        <th>Message</th>
                        <th>Database</th>
                    </tr>
                </thead>
                <tbody>
                    {{range .RecentErrors}}
                    <tr>
                        <td>{{.Timestamp}}</td>
                        <td class="status-error">{{.Message}}</td>
                        <td>{{.Database}}</td>
                    </tr>
                    {{end}}
                </tbody>
            </table>
        </div>
    </div>

    <script>
        function refreshData() {
            fetch('/api/dashboard')
                .then(res => res.json())
                .then(data => {
                    location.reload();
                });
        }
        setInterval(refreshData, 30000);
    </script>
</body>
</html>
`

type DashboardServer struct {
	data     *DashboardData
	template *template.Template
}

func NewDashboardServer() *DashboardServer {
	tmpl, _ := template.New("dashboard").Parse(dashboardTemplate)

	return &DashboardServer{
		data: &DashboardData{
			ServerName:    "UDBP",
			Version:       "1.0.0",
			Status:        "Running",
			DatabaseStats: []DatabaseStat{},
			RecentErrors:  []ErrorEntry{},
		},
		template: tmpl,
	}
}

func (ds *DashboardServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/", "/index.html":
		ds.renderDashboard(w, r)
	case "/api/dashboard":
		ds.serveAPI(w, r)
	case "/api/health":
		ds.serveHealth(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (ds *DashboardServer) renderDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	ds.template.Execute(w, ds.data)
}

func (ds *DashboardServer) serveAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ds.data)
}

func (ds *DashboardServer) serveHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (ds *DashboardServer) UpdateStats(totalQueries int64, connections int64) {
	ds.data.TotalQueries = totalQueries
	ds.data.ActiveConnections = connections
}

func (ds *DashboardServer) AddDatabaseStat(stat DatabaseStat) {
	ds.data.DatabaseStats = append(ds.data.DatabaseStats, stat)
}

func (ds *DashboardServer) AddError(err ErrorEntry) {
	ds.data.RecentErrors = append(ds.data.RecentErrors, err)
	if len(ds.data.RecentErrors) > 10 {
		ds.data.RecentErrors = ds.data.RecentErrors[len(ds.data.RecentErrors)-10:]
	}
}

func (ds *DashboardServer) SetStartTime(t time.Time) {
	ds.data.Uptime = time.Since(t)
}

func StartDashboard(addr string) error {
	dashboard := NewDashboardServer()
	return http.ListenAndServe(addr, dashboard)
}

func StartDashboardWithTLS(addr, certFile, keyFile string) error {
	dashboard := NewDashboardServer()
	return http.ListenAndServeTLS(addr, certFile, keyFile, dashboard)
}

func InitDashboardData(name, version string) *DashboardData {
	return &DashboardData{
		ServerName:        name,
		Version:           version,
		Status:            "Starting",
		Uptime:            0,
		TotalQueries:      0,
		ActiveConnections: 0,
		DatabaseStats:     []DatabaseStat{},
		RecentErrors:      []ErrorEntry{},
	}
}

func (d *DashboardData) ToJSON() (string, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal dashboard data: %w", err)
	}
	return string(data), nil
}
