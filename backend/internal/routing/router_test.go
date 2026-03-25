package routing

import (
	"context"
	"testing"

	"github.com/udbp/udbproxy/pkg/types"
)

func TestRouter_RoundRobin(t *testing.T) {
	rules := []types.RoutingRule{
		{
			Name:     "default",
			Database: "primary",
		},
	}

	router := NewRouter(rules, StrategyRoundRobin)

	router.RegisterDatabase(&types.DatabaseConfig{
		Name: "primary",
		Host: "localhost",
		Port: 3306,
	})

	qc := &types.QueryContext{
		RawQuery:     "SELECT * FROM users",
		Operation:    types.OpSelect,
		DatabaseType: types.DatabaseTypeMySQL,
	}

	db, err := router.Route(context.Background(), qc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if db.Name != "primary" {
		t.Errorf("expected primary database, got %s", db.Name)
	}
}

func TestRouter_ReadReplica(t *testing.T) {
	rules := []types.RoutingRule{
		{
			Name:         "read-select",
			MatchPattern: "SELECT",
			Database:     "replica",
		},
		{
			Name:     "default",
			Database: "primary",
		},
	}

	router := NewRouter(rules, StrategyReadReplica)

	router.RegisterDatabase(&types.DatabaseConfig{
		Name:          "primary",
		Host:          "localhost",
		Port:          3306,
		IsReadReplica: false,
	})

	router.RegisterDatabase(&types.DatabaseConfig{
		Name:          "replica",
		Host:          "localhost",
		Port:          3307,
		IsReadReplica: true,
	})

	qc := &types.QueryContext{
		RawQuery:     "SELECT * FROM users",
		Operation:    types.OpSelect,
		DatabaseType: types.DatabaseTypeMySQL,
	}

	db, err := router.Route(context.Background(), qc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if db.Name != "replica" {
		t.Errorf("expected replica database for SELECT, got %s", db.Name)
	}
}

func TestRouter_UnhealthyDatabase(t *testing.T) {
	rules := []types.RoutingRule{
		{
			Name:     "default",
			Database: "primary",
		},
	}

	router := NewRouter(rules, StrategyRoundRobin)

	router.RegisterDatabase(&types.DatabaseConfig{
		Name: "primary",
		Host: "localhost",
		Port: 3306,
	})

	router.SetDatabaseHealth("primary", false)

	qc := &types.QueryContext{
		RawQuery:     "SELECT * FROM users",
		Operation:    types.OpSelect,
		DatabaseType: types.DatabaseTypeMySQL,
	}

	_, err := router.Route(context.Background(), qc)
	if err == nil {
		t.Error("expected error when primary database is unhealthy")
	}
}

func TestRouter_NoMatchingDatabase(t *testing.T) {
	rules := []types.RoutingRule{
		{
			Name:         "specific",
			MatchPattern: "admin",
			Database:     "admin-db",
			Priority:     10,
		},
	}

	router := NewRouter(rules, StrategyRoundRobin)

	router.RegisterDatabase(&types.DatabaseConfig{
		Name: "primary",
		Host: "localhost",
		Port: 3306,
	})

	qc := &types.QueryContext{
		RawQuery:     "SELECT * FROM users",
		Operation:    types.OpSelect,
		DatabaseType: types.DatabaseTypeMySQL,
	}

	_, err := router.Route(context.Background(), qc)
	if err == nil {
		t.Error("expected error when no database matches")
	}
}
