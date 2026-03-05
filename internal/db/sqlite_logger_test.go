package db

import (
	"bytes"
	"context"
	"testing"
)

type sqliteLoggerParamFilter interface {
	ParamsFilter(ctx context.Context, sql string, params ...interface{}) (string, []interface{})
}

func TestNewSQLiteLoggerUsesParameterizedQueries(t *testing.T) {
	t.Parallel()

	logger := newSQLiteLogger(&bytes.Buffer{})
	filter, ok := logger.(sqliteLoggerParamFilter)
	if !ok {
		t.Fatal("expected sqlite logger to expose ParamsFilter")
	}

	sql, params := filter.ParamsFilter(context.Background(), "SELECT * FROM users WHERE email = ?", "user@example.com")
	if sql != "SELECT * FROM users WHERE email = ?" {
		t.Fatalf("expected SQL statement to be preserved, got %q", sql)
	}
	if params != nil {
		t.Fatalf("expected bind params to be suppressed, got %#v", params)
	}
}
