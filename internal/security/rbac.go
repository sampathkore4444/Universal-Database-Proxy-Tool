package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleReadOnly  Role = "read_only"
	RoleOperator  Role = "operator"
	RoleAuditor   Role = "auditor"
)

type Permission string

const (
	PermRead    Permission = "read"
	PermWrite   Permission = "write"
	PermDelete  Permission = "delete"
	PermExecute Permission = "execute"
	PermAdmin   Permission = "admin"
	PermAudit   Permission = "audit"
	PermConfig  Permission = "config"
)

type RBAC struct {
	mu           sync.RWMutex
	users        map[string]*User
	roles        map[Role]*RoleDefinition
	permissions  map[string]map[Permission]bool
	sessionCache map[string]*Session
}

type User struct {
	ID         string
	Username   string
	Password   string
	Roles      []Role
	Attributes map[string]interface{}
	CreatedAt  time.Time
	LastLogin  time.Time
	Enabled    bool
}

type RoleDefinition struct {
	Name        Role
	Permissions []Permission
	Inherits    []Role
	Description string
}

type Session struct {
	ID        string
	UserID    string
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
	IP        string
}

type AccessRequest struct {
	User     string
	Resource string
	Action   Permission
	Database string
}

func NewRBAC() *RBAC {
	rbac := &RBAC{
		users:        make(map[string]*User),
		roles:        make(map[Role]*RoleDefinition),
		permissions:  make(map[string]map[Permission]bool),
		sessionCache: make(map[string]*Session),
	}

	rbac.initDefaultRoles()
	return rbac
}

func (rbac *RBAC) initDefaultRoles() {
	rbac.roles[RoleAdmin] = &RoleDefinition{
		Name:        RoleAdmin,
		Permissions: []Permission{PermRead, PermWrite, PermDelete, PermExecute, PermAdmin, PermAudit, PermConfig},
		Description: "Full administrative access",
	}

	rbac.roles[RoleDeveloper] = &RoleDefinition{
		Name:        RoleDeveloper,
		Permissions: []Permission{PermRead, PermWrite, PermExecute, PermConfig},
		Description: "Developer access to read/write/config",
	}

	rbac.roles[RoleReadOnly] = &RoleDefinition{
		Name:        RoleReadOnly,
		Permissions: []Permission{PermRead},
		Description: "Read-only access",
	}

	rbac.roles[RoleOperator] = &RoleDefinition{
		Name:        RoleOperator,
		Permissions: []Permission{PermRead, PermExecute},
		Description: "Operator access for monitoring",
	}

	rbac.roles[RoleAuditor] = &RoleDefinition{
		Name:        RoleAuditor,
		Permissions: []Permission{PermRead, PermAudit},
		Description: "Auditor access for audit logs",
	}
}

func (rbac *RBAC) AddUser(user *User) error {
	rbac.mu.Lock()
	defer rbac.mu.Unlock()

	if _, exists := rbac.users[user.Username]; exists {
		return fmt.Errorf("user already exists: %s", user.Username)
	}

	rbac.users[user.Username] = user
	return nil
}

func (rbac *RBAC) RemoveUser(username string) error {
	rbac.mu.Lock()
	defer rbac.mu.Unlock()

	if _, exists := rbac.users[username]; !exists {
		return fmt.Errorf("user not found: %s", username)
	}

	delete(rbac.users, username)
	return nil
}

func (rbac *RBAC) GetUser(username string) (*User, bool) {
	rbac.mu.RLock()
	defer rbac.mu.RUnlock()
	user, ok := rbac.users[username]
	return user, ok
}

func (rbac *RBAC) UpdateUser(username string, updates map[string]interface{}) error {
	rbac.mu.Lock()
	defer rbac.mu.Unlock()

	user, exists := rbac.users[username]
	if !exists {
		return fmt.Errorf("user not found: %s", username)
	}

	if roles, ok := updates["roles"].([]Role); ok {
		user.Roles = roles
	}
	if enabled, ok := updates["enabled"].(bool); ok {
		user.Enabled = enabled
	}

	return nil
}

func (rbac *RBAC) HasPermission(username string, permission Permission, resource string) bool {
	rbac.mu.RLock()
	defer rbac.mu.RUnlock()

	user, exists := rbac.users[username]
	if !exists || !user.Enabled {
		return false
	}

	for _, role := range user.Roles {
		if rbac.roleHasPermission(role, permission) {
			return true
		}
	}

	return false
}

func (rbac *RBAC) roleHasPermission(role Role, permission Permission) bool {
	roleDef, exists := rbac.roles[role]
	if !exists {
		return false
	}

	for _, perm := range roleDef.Permissions {
		if perm == permission {
			return true
		}
	}

	for _, inherited := range roleDef.Inherits {
		if rbac.roleHasPermission(inherited, permission) {
			return true
		}
	}

	return false
}

func (rbac *RBAC) CreateSession(username, ip string, duration time.Duration) (*Session, error) {
	rbac.mu.Lock()
	defer rbac.mu.Unlock()

	user, exists := rbac.users[username]
	if !exists {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	if !user.Enabled {
		return nil, fmt.Errorf("user account is disabled")
	}

	session := &Session{
		ID:        generateSessionID(),
		UserID:    user.ID,
		Username:  username,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(duration),
		IP:        ip,
	}

	rbac.sessionCache[session.ID] = session
	user.LastLogin = time.Now()

	return session, nil
}

func (rbac *RBAC) ValidateSession(sessionID string) (*Session, bool) {
	rbac.mu.RLock()
	defer rbac.mu.RUnlock()

	session, exists := rbac.sessionCache[sessionID]
	if !exists {
		return nil, false
	}

	if time.Now().After(session.ExpiresAt) {
		delete(rbac.sessionCache, sessionID)
		return nil, false
	}

	return session, true
}

func (rbac *RBAC) RevokeSession(sessionID string) {
	rbac.mu.Lock()
	defer rbac.mu.Unlock()
	delete(rbac.sessionCache, sessionID)
}

func (rbac *RBAC) AddRole(role Role, definition *RoleDefinition) error {
	rbac.mu.Lock()
	defer rbac.mu.Unlock()

	rbac.roles[role] = definition
	return nil
}

func (rbac *RBAC) ListUsers() []*User {
	rbac.mu.RLock()
	defer rbac.mu.RUnlock()

	users := make([]*User, 0, len(rbac.users))
	for _, user := range rbac.users {
		users = append(users, user)
	}
	return users
}

func (rbac *RBAC) ListRoles() []*RoleDefinition {
	rbac.mu.RLock()
	defer rbac.mu.RUnlock()

	roles := make([]*RoleDefinition, 0, len(rbac.roles))
	for _, role := range rbac.roles {
		roles = append(roles, role)
	}
	return roles
}

func generateSessionID() string {
	return fmt.Sprintf("sess-%d-%s", time.Now().UnixNano(), randomString(16))
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

type AuthMiddleware struct {
	rbac          *RBAC
	sessionCookie string
}

func NewAuthMiddleware(rbac *RBAC) *AuthMiddleware {
	return &AuthMiddleware{
		rbac:          rbac,
		sessionCookie: "udbp_session",
	}
}

func (am *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID, err := r.Cookie(am.sessionCookie)
		if err != nil || sessionID == nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		session, valid := am.rbac.ValidateSession(sessionID.Value)
		if !valid {
			http.Error(w, "Invalid or expired session", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), "session", session)
		ctx = context.WithValue(ctx, "username", session.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (am *AuthMiddleware) RequirePermission(permission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			username := r.Context().Value("username").(string)

			path := r.URL.Path
			resource := extractResource(path)

			if !am.rbac.HasPermission(username, permission, resource) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractResource(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

func (am *AuthMiddleware) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	user, exists := am.rbac.GetUser(req.Username)
	if !exists || user.Password != req.Password {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	session, err := am.rbac.CreateSession(req.Username, r.RemoteAddr, 24*time.Hour)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     am.sessionCookie,
		Value:    session.ID,
		Expires:  session.ExpiresAt,
		HttpOnly: true,
	})

	json.NewEncoder(w).Encode(map[string]string{"status": "logged_in"})
}

func (am *AuthMiddleware) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	sessionID, err := r.Cookie(am.sessionCookie)
	if err == nil && sessionID != nil {
		am.rbac.RevokeSession(sessionID.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   am.sessionCookie,
		Value:  "",
		MaxAge: -1,
	})

	json.NewEncoder(w).Encode(map[string]string{"status": "logged_out"})
}
