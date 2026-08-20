package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestUserRepositoryRefusesAnAccountWithARoleTheProductDoesNotHave holds both
// account-creating repository methods to the owner-role-only privacy boundary.
//
// Ovumcy has one role: every account is the sole owner of its own data, an
// instance may host several independent owners, and there is no viewer or
// partner role at all. The users column, however, still carries
// `CHECK (role IN ('owner', 'partner'))` on both engines, so until now the
// database would have accepted a partner account and only the web policy
// (services.ValidateSupportedWebUser, at login) would have turned it away —
// containment after the fact, not refusal at the write.
//
// The negative cases are what the guard is for; the two positive cases are the
// anchor, because a Create that rejected everything would satisfy the negatives
// alone. Both methods are covered on purpose: they are the only two writes in
// this package that put a role into users, and a class refused at one of two
// write sites is a new defect rather than half a fix.
func TestUserRepositoryRefusesAnAccountWithARoleTheProductDoesNotHave(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		role          string
		expectStored  string
		expectRefusal bool
	}{
		{
			name:          "partner is not a role this product has",
			role:          "partner",
			expectRefusal: true,
		},
		{
			name:          "nor is any other value the column would accept later",
			role:          "viewer",
			expectRefusal: true,
		},
		{
			name:          "a role differing only in case is not the owner role",
			role:          "Owner",
			expectRefusal: true,
		},
		{
			name:         "owner is written through",
			role:         models.RoleOwner,
			expectStored: models.RoleOwner,
		},
		{
			name:         "an unset role takes the column default, which is owner",
			role:         "",
			expectStored: models.RoleOwner,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			for _, writer := range userCreatingRepositoryMethods() {
				t.Run(writer.Name, func(t *testing.T) {
					database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "roles.db"))
					repository := NewUserRepository(database)

					user := &models.User{
						Email:        "role-case@example.com",
						PasswordHash: "hash",
						Role:         testCase.role,
					}
					err := writer.Create(context.Background(), repository, user)

					if testCase.expectRefusal {
						if !errors.Is(err, ErrUnsupportedUserRole) {
							t.Fatalf("%s(role=%q) error = %v, want ErrUnsupportedUserRole", writer.Name, testCase.role, err)
						}
						assertNoUserRowsStored(t, database)
						return
					}

					if err != nil {
						t.Fatalf("%s(role=%q) unexpected error: %v", writer.Name, testCase.role, err)
					}
					assertStoredUserRole(t, database, "role-case@example.com", testCase.expectStored)
				})
			}
		})
	}
}

// TestUserRepositoryRefusesAnAbsentAccountAtTheRoleCheck covers the one input
// that carries no role to inspect at all.
//
// A nil account is invalid input, never a reason to skip the comparison: the
// role check reads a field off the value it is handed, so an implementation
// that let nil through would not fall back to some safe default — it would
// panic inside the repository, on the path that creates accounts. Refusing it
// with the same error as an unsupported role keeps the guard total over its
// input, and the callers below prove the refusal still happens before the
// write.
func TestUserRepositoryRefusesAnAbsentAccountAtTheRoleCheck(t *testing.T) {
	for _, writer := range userCreatingRepositoryMethods() {
		t.Run(writer.Name, func(t *testing.T) {
			database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "absent-account.db"))
			repository := NewUserRepository(database)

			err := writer.Create(context.Background(), repository, nil)

			if !errors.Is(err, ErrUnsupportedUserRole) {
				t.Fatalf("%s(nil) error = %v, want ErrUnsupportedUserRole", writer.Name, err)
			}
			assertNoUserRowsStored(t, database)
		})
	}
}

// userCreatingRepositoryMethod names one repository entry point that writes a
// role into users, so the cases above run against every one of them rather than
// against whichever came to mind.
type userCreatingRepositoryMethod struct {
	Name   string
	Create func(ctx context.Context, repository *UserRepository, user *models.User) error
}

func userCreatingRepositoryMethods() []userCreatingRepositoryMethod {
	return []userCreatingRepositoryMethod{
		{
			Name: "Create",
			Create: func(ctx context.Context, repository *UserRepository, user *models.User) error {
				return repository.Create(ctx, user)
			},
		},
		{
			Name: "CreateUserWithSymptoms",
			Create: func(ctx context.Context, repository *UserRepository, user *models.User) error {
				return repository.CreateUserWithSymptoms(ctx, user, []models.SymptomType{
					{Name: "Cramps", Icon: "cramps", Color: "#ff0000", IsBuiltin: true},
				})
			},
		},
	}
}

// assertNoUserRowsStored proves the refusal happened BEFORE the write: a method
// that returned the error after inserting would leave the row the product
// cannot serve exactly where the guard says it must never be. The seeded
// symptoms are checked with it, since CreateUserWithSymptoms writes both.
func assertNoUserRowsStored(t *testing.T, database *gorm.DB) {
	t.Helper()

	for _, table := range []string{"users", "symptom_types"} {
		var count int64
		if err := database.Table(table).Count(&count).Error; err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("expected the refused account to leave %s empty, found %d row(s)", table, count)
		}
	}
}

func assertStoredUserRole(t *testing.T, database *gorm.DB, email string, expectedRole string) {
	t.Helper()

	var stored struct {
		Role string `gorm:"column:role"`
	}
	if err := database.Raw(`SELECT role FROM users WHERE email = ?`, email).Scan(&stored).Error; err != nil {
		t.Fatalf("read back the stored role: %v", err)
	}
	if stored.Role != expectedRole {
		t.Fatalf("stored role = %q, want %q", stored.Role, expectedRole)
	}
}
