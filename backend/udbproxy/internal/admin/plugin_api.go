package admin

import (
	"encoding/json"
	"net/http"

	"github.com/udbp/udbproxy/internal/plugin"
)

type PluginAPI struct {
	manager *plugin.PluginManager
	mux     *http.ServeMux
}

func NewPluginAPI(manager *plugin.PluginManager) *PluginAPI {
	api := &PluginAPI{
		manager: manager,
		mux:     http.NewServeMux(),
	}

	api.registerRoutes()
	return api
}

func (api *PluginAPI) registerRoutes() {
	api.mux.HandleFunc("/plugins", api.handlePlugins)
	api.mux.HandleFunc("/plugins/", api.handlePluginByID)
	api.mux.HandleFunc("/plugins/reload", api.handleReload)
}

func (api *PluginAPI) handlePlugins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		plugins := api.manager.ListPlugins()
		json.NewEncoder(w).Encode(plugins)

	case http.MethodPost:
		var req struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			WASM    string `json:"wasm"` // base64 encoded
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// Note: In production, WASM would be properly handled
		err := api.manager.LoadPlugin(nil, req.Name, req.Version, []byte(req.WASM), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "loaded"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *PluginAPI) handlePluginByID(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/plugins/"):]

	switch r.Method {
	case http.MethodGet:
		p, found := api.manager.GetPlugin(name)
		if !found {
			http.Error(w, "Plugin not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"name":    p.Name,
			"version": p.Version,
			"enabled": p.IsEnabled(),
		})

	case http.MethodDelete:
		err := api.manager.UnloadPlugin(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "unloaded"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *PluginAPI) handleReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name != "" {
		plugins := api.manager.ListPlugins()
		for _, p := range plugins {
			if p.Name == name {
				p.Disable()
				p.Enable()
			}
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "reloaded", "plugin": name})
		return
	}

	http.Error(w, "Plugin name required", http.StatusBadRequest)
}

func (api *PluginAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mux.ServeHTTP(w, r)
}
