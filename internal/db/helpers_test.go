package db

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestConfigNormalizedDefaultsAndTrims(t *testing.T) {
	t.Parallel()

	normalized := (Config{
		Driver:      " SQLITE ",
		SQLitePath:  " ./data/ovumcy.db ",
		PostgresURL: " postgres://user:pass@db.example.com/ovumcy ",
	}).normalized()

	if normalized.Driver != DriverSQLite {
		t.Fatalf("expected normalized driver sqlite, got %q", normalized.Driver)
	}
	if normalized.SQLitePath != "./data/ovumcy.db" || normalized.PostgresURL != "postgres://user:pass@db.example.com/ovumcy" {
		t.Fatalf("expected trimmed config values, got %#v", normalized)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{name: "default sqlite missing path", config: Config{}, wantErr: "sqlite requires DB_PATH"},
		{name: "sqlite valid", config: Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "ovumcy.db")}},
		{name: "postgres missing url", config: Config{Driver: DriverPostgres}, wantErr: "postgres requires DATABASE_URL"},
		{name: "postgres valid", config: Config{Driver: DriverPostgres, PostgresURL: "postgres://user:pass@db.example.com/ovumcy"}},
		{name: "unsupported driver", config: Config{Driver: "mysql"}, wantErr: `unsupported DB_DRIVER "mysql"`},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := testCase.config.Validate()
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("expected config to validate, got %v", err)
				}
				return
			}
			if err == nil || err.Error() != testCase.wantErr {
				t.Fatalf("expected error %q, got %v", testCase.wantErr, err)
			}
		})
	}
}

func TestUniqueConstraintErrorHelpers(t *testing.T) {
	t.Parallel()

	wrapped := &UniqueConstraintError{
		Constraint: "users.email",
		Err:        gorm.ErrDuplicatedKey,
	}
	if wrapped.Error() != "unique constraint violation: users.email" {
		t.Fatalf("unexpected unique constraint error string %q", wrapped.Error())
	}
	if !errors.Is(wrapped, gorm.ErrDuplicatedKey) {
		t.Fatal("expected unique constraint error to unwrap to duplicated-key sentinel")
	}
	if wrapped.UniqueConstraint() != "users.email" {
		t.Fatalf("unexpected unique constraint helper value %q", wrapped.UniqueConstraint())
	}

	withoutConstraint := &UniqueConstraintError{}
	if withoutConstraint.Error() != "unique constraint violation" {
		t.Fatalf("unexpected fallback unique constraint error string %q", withoutConstraint.Error())
	}
}

func TestSymptomSeedErrorHelpers(t *testing.T) {
	t.Parallel()

	wrapped := &SymptomSeedError{Err: errors.New("write failed")}
	if wrapped.Error() != "symptom seed write failed" {
		t.Fatalf("unexpected symptom seed error string %q", wrapped.Error())
	}
	if wrapped.Unwrap() == nil || wrapped.Unwrap().Error() != "write failed" {
		t.Fatalf("expected symptom seed error to unwrap the original error, got %v", wrapped.Unwrap())
	}
	if !wrapped.SymptomSeedFailure() {
		t.Fatal("expected symptom seed failure marker to be true")
	}
}

func TestClassifyUniqueConstraintError(t *testing.T) {
	t.Parallel()

	if got := classifyUniqueConstraintError(nil, "users.email"); got != nil {
		t.Fatalf("expected nil error to remain nil, got %v", got)
	}

	duplicated := classifyUniqueConstraintError(gorm.ErrDuplicatedKey, "users.email")
	var uniqueErr *UniqueConstraintError
	if !errors.As(duplicated, &uniqueErr) || uniqueErr.Constraint != "users.email" {
		t.Fatalf("expected duplicated-key sentinel to become UniqueConstraintError, got %T %v", duplicated, duplicated)
	}

	sqliteStyle := errors.New("UNIQUE constraint failed: users.email")
	sqliteClassified := classifyUniqueConstraintError(sqliteStyle, "fallback")
	if !errors.As(sqliteClassified, &uniqueErr) || uniqueErr.Constraint != "users.email" {
		t.Fatalf("expected sqlite-style error to extract users.email, got %T %v", sqliteClassified, sqliteClassified)
	}

	rawErr := errors.New("other failure")
	if got := classifyUniqueConstraintError(rawErr, "users.email"); !errors.Is(got, rawErr) {
		t.Fatalf("expected non-unique error passthrough, got %v", got)
	}
}

func TestClassifyCreateErrorsUseExpectedConstraints(t *testing.T) {
	t.Parallel()

	userErr := classifyUserCreateError(errors.New("UNIQUE constraint failed: users.email"))
	var uniqueErr *UniqueConstraintError
	if !errors.As(userErr, &uniqueErr) || uniqueErr.Constraint != "users.email" {
		t.Fatalf("expected user create error to classify users.email, got %T %v", userErr, userErr)
	}

	oidcErr := classifyOIDCIdentityCreateError(errors.New("UNIQUE constraint failed: oidc_identities.issuer_subject"))
	if !errors.As(oidcErr, &uniqueErr) || uniqueErr.Constraint != "oidc_identities.issuer_subject" {
		t.Fatalf("expected oidc identity create error to classify issuer_subject, got %T %v", oidcErr, oidcErr)
	}
}

func TestNewGORMConfigEnablesTranslateError(t *testing.T) {
	t.Parallel()

	config := newGORMConfig(nil)
	if config == nil || !config.TranslateError {
		t.Fatalf("expected gorm config to enable translated errors, got %#v", config)
	}
	if config.Logger == nil {
		t.Fatal("expected gorm config to configure a logger")
	}
}

func TestOpenSQLiteConnectionRejectsDirectoryCreationFailure(t *testing.T) {
	t.Parallel()

	parentFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parentFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	_, err := openSQLiteConnection(filepath.Join(parentFile, "ovumcy.db"))
	if err == nil || !strings.Contains(err.Error(), "create db directory") {
		t.Fatalf("expected sqlite directory creation failure, got %v", err)
	}
}
