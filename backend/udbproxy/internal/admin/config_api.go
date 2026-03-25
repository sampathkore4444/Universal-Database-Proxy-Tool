package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/udbp/udbproxy/config"
)

type ConfigAPI struct {
	cfg *config.Config
	mu  sync.RWMutex
	mux *http.ServeMux
}

func NewConfigAPI(cfg *config.Config) *ConfigAPI {
	api := &ConfigAPI{
		cfg: cfg,
		mux: http.NewServeMux(),
	}

	api.registerRoutes()
	return api
}

func (api *ConfigAPI) registerRoutes() {
	api.mux.HandleFunc("/config", api.handleConfig)
	api.mux.HandleFunc("/config/databases", api.handleDatabases)
	api.mux.HandleFunc("/config/routing", api.handleRouting)
	api.mux.HandleFunc("/config/security", api.handleSecurity)
	api.mux.HandleFunc("/config/reload", api.handleReload)
}

func (api *ConfigAPI) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.mu.RLock()
	defer api.mu.RUnlock()

	json.NewEncoder(w).Encode(api.cfg)
}

func (api *ConfigAPI) handleDatabases(w http.ResponseWriter, r *http.Request) {
	api.mu.RLock()
	defer api.mu.RUnlock()

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(api.cfg.Databases)
	case http.MethodPost:
		var db config.DatabaseConfig
		if err := json.NewDecoder(r.Body).Decode(&db); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		api.cfg.Databases = append(api.cfg.Databases, db)
		json.NewEncoder(w).Encode(map[string]string{"status": "added"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *ConfigAPI) handleRouting(w http.ResponseWriter, r *http.Request) {
	api.mu.RLock()
	defer api.mu.RUnlock()

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(api.cfg.RoutingRules)
	case http.MethodPost:
		var rule config.RoutingRuleConfig
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		api.cfg.RoutingRules = append(api.cfg.RoutingRules, rule)
		json.NewEncoder(w).Encode(map[string]string{"status": "added"})
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *ConfigAPI) handleSecurity(w http.ResponseWriter, r *http.Request) {
	api.mu.RLock()
	defer api.mu.RUnlock()

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(api.cfg.SecurityRules)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *ConfigAPI) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.cfg.WatchChanges(func() {
		fmt.Println("Configuration reloaded")
	})

	json.NewEncoder(w).Encode(map[string]string{"status": "reload_triggered"})
}

func (api *ConfigAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mux.ServeHTTP(w, r)
}

func (api *ConfigAPI) UpdateConfig(newCfg *config.Config) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.cfg = newCfg
}

func (api *ConfigAPI) GetConfig() *config.Config {
	api.mu.RLock()
	defer api.mu.RUnlock()
	return api.cfg
}
