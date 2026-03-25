package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Policy struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        PolicyType             `json:"type"`
	Priority    int                    `json:"priority"`
	Enabled     bool                   `json:"enabled"`
	Match       PolicyMatch            `json:"match"`
	Actions     []PolicyAction         `json:"actions"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type PolicyType string

const (
	PolicyTypeSecurity   PolicyType = "security"
	PolicyTypeRouting    PolicyType = "routing"
	PolicyTypeThrottle   PolicyType = "throttle"
	PolicyTypeAudit      PolicyType = "audit"
	PolicyTypeCompliance PolicyType = "compliance"
)

type PolicyMatch struct {
	Database  string `json:"database"`
	User      string `json:"user"`
	Operation string `json:"operation"`
	Pattern   string `json:"pattern"`
	IPRange   string `json:"ip_range"`
}

type PolicyAction struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type PolicyEngine struct {
	mu       sync.RWMutex
	policies []Policy
}

func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		policies: []Policy{},
	}
}

func (pe *PolicyEngine) AddPolicy(policy Policy) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	policy.ID = generatePolicyID()
	policy.CreatedAt = time.Now()
	policy.UpdatedAt = time.Now()

	pe.policies = append(pe.policies, policy)
	return nil
}

func (pe *PolicyEngine) UpdatePolicy(id string, updates map[string]interface{}) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	for i, p := range pe.policies {
		if p.ID == id {
			if name, ok := updates["name"].(string); ok {
				pe.policies[i].Name = name
			}
			if enabled, ok := updates["enabled"].(bool); ok {
				pe.policies[i].Enabled = enabled
			}
			if priority, ok := updates["priority"].(int); ok {
				pe.policies[i].Priority = priority
			}
			pe.policies[i].UpdatedAt = time.Now()
			return nil
		}
	}

	return fmt.Errorf("policy not found: %s", id)
}

func (pe *PolicyEngine) DeletePolicy(id string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	for i, p := range pe.policies {
		if p.ID == id {
			pe.policies = append(pe.policies[:i], pe.policies[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("policy not found: %s", id)
}

func (pe *PolicyEngine) GetPolicy(id string) (*Policy, bool) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	for _, p := range pe.policies {
		if p.ID == id {
			return &p, true
		}
	}
	return nil, false
}

func (pe *PolicyEngine) ListPolicies(policyType PolicyType) []Policy {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	var result []Policy
	for _, p := range pe.policies {
		if policyType == "" || p.Type == policyType {
			result = append(result, p)
		}
	}
	return result
}

func (pe *PolicyEngine) Evaluate(match PolicyMatch) []PolicyAction {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	var matchedPolicies []Policy

	for _, p := range pe.policies {
		if !p.Enabled {
			continue
		}

		if pe.matchPolicy(p.Match, match) {
			matchedPolicies = append(matchedPolicies, p)
		}
	}

	for i := range matchedPolicies {
		for j := i + 1; j < len(matchedPolicies); j++ {
			if matchedPolicies[i].Priority > matchedPolicies[j].Priority {
				matchedPolicies[i], matchedPolicies[j] = matchedPolicies[j], matchedPolicies[i]
			}
		}
	}

	var actions []PolicyAction
	for _, p := range matchedPolicies {
		actions = append(actions, p.Actions...)
	}

	return actions
}

func (pe *PolicyEngine) matchPolicy(policyMatch, queryMatch PolicyMatch) bool {
	if policyMatch.Database != "" && policyMatch.Database != queryMatch.Database {
		return false
	}
	if policyMatch.User != "" && policyMatch.User != queryMatch.User {
		return false
	}
	if policyMatch.Operation != "" && policyMatch.Operation != queryMatch.Operation {
		return false
	}
	return true
}

func generatePolicyID() string {
	return fmt.Sprintf("pol-%d-%s", time.Now().Unix(), randomString(8))
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}

type PolicyAPI struct {
	engine *PolicyEngine
	mux    *http.ServeMux
}

func NewPolicyAPI(engine *PolicyEngine) *PolicyAPI {
	api := &PolicyAPI{
		engine: engine,
		mux:    http.NewServeMux(),
	}

	api.registerRoutes()
	return api
}

func (api *PolicyAPI) registerRoutes() {
	api.mux.HandleFunc("/policies", api.handlePolicies)
	api.mux.HandleFunc("/policies/", api.handlePolicyByID)
	api.mux.HandleFunc("/policies/evaluate", api.handleEvaluate)
}

func (api *PolicyAPI) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		policyType := r.URL.Query().Get("type")
		policies := api.engine.ListPolicies(PolicyType(policyType))
		json.NewEncoder(w).Encode(policies)

	case http.MethodPost:
		var policy Policy
		if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := api.engine.AddPolicy(policy); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "created"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *PolicyAPI) handlePolicyByID(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/policies/"):]

	switch r.Method {
	case http.MethodGet:
		policy, found := api.engine.GetPolicy(id)
		if !found {
			http.Error(w, "Policy not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(policy)

	case http.MethodPut:
		var updates map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		if err := api.engine.UpdatePolicy(id, updates); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

	case http.MethodDelete:
		if err := api.engine.DeletePolicy(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (api *PolicyAPI) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var match PolicyMatch
	if err := json.NewDecoder(r.Body).Decode(&match); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	actions := api.engine.Evaluate(match)
	json.NewEncoder(w).Encode(actions)
}

func (api *PolicyAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mux.ServeHTTP(w, r)
}
