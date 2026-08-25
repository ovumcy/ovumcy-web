package db

import (
	"errors"
	"fmt"
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

	// What the postgres driver actually hands us: the sentinel wrapped around
	// the pgconn error, never the bare sentinel (gorm.io/driver/postgres
	// error_translator.go returns fmt.Errorf("%w: %w", …)). Classification must
	// see through the wrap, because it is the only thing it looks at.
	wrapped := fmt.Errorf(
		"%w: ERROR: duplicate key value violates unique constraint \"idx_users_email\" (SQLSTATE 23505)",
		gorm.ErrDuplicatedKey,
	)
	wrappedClassified := classifyUniqueConstraintError(wrapped, "users.email")
	if !errors.As(wrappedClassified, &uniqueErr) || uniqueErr.Constraint != "users.email" {
		t.Fatalf("expected wrapped duplicated-key error to become UniqueConstraintError, got %T %v", wrappedClassified, wrappedClassified)
	}

	rawErr := errors.New("other failure")
	if got := classifyUniqueConstraintError(rawErr, "users.email"); !errors.Is(got, rawErr) {
		t.Fatalf("expected non-unique error passthrough, got %v", got)
	}
}

// TestClassifyUniqueConstraintErrorIgnoresDriverMessageText pins the deletion of
// the driver-message fallback: classification is driven by gorm.ErrDuplicatedKey
// alone — i.e. by TranslateError in newGORMConfig — and never by sniffing the
// driver's wording. A message-only error carries no translated sentinel, so it
// is not a unique-constraint violation as far as this layer is concerned and
// passes through untouched.
//
// Sniffing a message was dialect-asymmetric and could never be right for both
// drivers at once: "UNIQUE constraint failed:" is SQLite's wording, which
// postgres does not emit, and the glebarez sqlite translator replaces the
// driver error outright with the bare sentinel, so the wording never survives
// to be matched anyway.
func TestClassifyUniqueConstraintErrorIgnoresDriverMessageText(t *testing.T) {
	t.Parallel()

	driverWorded := errors.New("UNIQUE constraint failed: users.email")
	classified := classifyUniqueConstraintError(driverWorded, "symptom_types.user_id_name")

	var uniqueErr *UniqueConstraintError
	if errors.As(classified, &uniqueErr) {
		t.Fatalf("expected driver message text to be ignored, got UniqueConstraintError %q", uniqueErr.Constraint)
	}
	if !errors.Is(classified, driverWorded) {
		t.Fatalf("expected untranslated driver error to pass through unchanged, got %T %v", classified, classified)
	}
}

func TestClassifyCreateErrorsUseExpectedConstraints(t *testing.T) {
	t.Parallel()

	userErr := classifyUserCreateError(gorm.ErrDuplicatedKey)
	var uniqueErr *UniqueConstraintError
	if !errors.As(userErr, &uniqueErr) || uniqueErr.Constraint != "users.email" {
		t.Fatalf("expected user create error to classify users.email, got %T %v", userErr, userErr)
	}

	oidcErr := classifyOIDCIdentityCreateError(gorm.ErrDuplicatedKey)
	if !errors.As(oidcErr, &uniqueErr) || uniqueErr.Constraint != "oidc_identities.issuer_subject" {
		t.Fatalf("expected oidc identity create error to classify issuer_subject, got %T %v", oidcErr, oidcErr)
	}

	symptomErr := classifySymptomWriteError(gorm.ErrDuplicatedKey)
	if !errors.As(symptomErr, &uniqueErr) || uniqueErr.Constraint != "symptom_types.user_id_name" {
		t.Fatalf("expected symptom write error to classify user_id_name, got %T %v", symptomErr, symptomErr)
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
