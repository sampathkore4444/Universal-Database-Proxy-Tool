package engines

import (
	"context"
	"testing"

	"github.com/udbp/udbproxy/pkg/types"
)

func TestSecurityEngine_SQLInjectionDetection(t *testing.T) {
	engine := NewSecurityEngine(nil)

	tests := []struct {
		name     string
		query    string
		wantDeny bool
	}{
		{
			name:     "Union select injection",
			query:    "SELECT id, username FROM users UNION SELECT password FROM admin",
			wantDeny: true,
		},
		{
			name:     "DROP TABLE injection",
			query:    "DROP TABLE users",
			wantDeny: true,
		},
		{
			name:     "DELETE injection",
			query:    "DELETE FROM users",
			wantDeny: true,
		},
		{
			name:     "UPDATE injection",
			query:    "UPDATE users SET password='hacked'",
			wantDeny: true,
		},
		{
			name:     "Comment injection",
			query:    "SELECT * FROM users WHERE id=1 --",
			wantDeny: true,
		},
		{
			name:     "Valid SELECT query",
			query:    "SELECT * FROM users WHERE id = 1",
			wantDeny: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc := &types.QueryContext{
				RawQuery:     tt.query,
				Operation:    types.OpSelect,
				DatabaseType: types.DatabaseTypeMySQL,
			}

			result := engine.Process(context.Background(), qc)

			if tt.wantDeny && result.Continue {
				t.Errorf("expected SQL injection to be denied, but it was allowed")
			}
			if !tt.wantDeny && !result.Continue {
				t.Errorf("expected query to be allowed, but it was denied: %v", result.Error)
			}
		})
	}
}

func TestSecurityEngine_DangerousFunctions(t *testing.T) {
	engine := NewSecurityEngine(nil)

	tests := []struct {
		name     string
		query    string
		wantDeny bool
	}{
		{
			name:     "EXEC function",
			query:    "EXEC sp_executesql N'SELECT * FROM users'",
			wantDeny: true,
		},
		{
			name:     "EVAL function",
			query:    "SELECT EVAL('1+1')",
			wantDeny: true,
		},
		{
			name:     "xp_cmdshell",
			query:    "EXEC xp_cmdshell 'dir'",
			wantDeny: true,
		},
		{
			name:     "Safe query",
			query:    "SELECT * FROM users WHERE id = 1",
			wantDeny: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc := &types.QueryContext{
				RawQuery:     tt.query,
				Operation:    types.OpSelect,
				DatabaseType: types.DatabaseTypeMySQL,
			}

			result := engine.Process(context.Background(), qc)

			if tt.wantDeny && result.Continue {
				t.Errorf("expected dangerous function to be denied")
			}
		})
	}
}

func TestSecurityEngine_SecurityRules(t *testing.T) {
	rules := []types.SecurityRule{
		{
			Name:         "block-admin",
			MatchPattern: "admin",
			Action:       types.SecurityActionDeny,
		},
		{
			Name:         "log-sensitive",
			MatchPattern: "password|credit_card",
			Action:       types.SecurityActionLog,
		},
	}

	engine := NewSecurityEngine(rules)

	qc := &types.QueryContext{
		RawQuery:     "SELECT * FROM admin_users",
		Operation:    types.OpSelect,
		DatabaseType: types.DatabaseTypeMySQL,
	}

	result := engine.Process(context.Background(), qc)

	if result.Continue {
		t.Error("expected query matching admin pattern to be denied")
	}
}
