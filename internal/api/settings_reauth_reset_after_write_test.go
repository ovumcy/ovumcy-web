package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Every settings action that writes behind a settings.reauth password clears
// that budget only once its write has committed. A correct password whose write
// was then refused proved nothing lasting, so it keeps the count it found: the
// failures typed before it still stand, and one more wrong password reaches the
// limit. Each writing caller has a refused-write case and a positive control
// (a committed write does clear the count). The write is refused from a gorm
// callback registered on the test's own database, so the handler, the service
// and the repository all run unchanged.

const (
	reauthWriteCorrectPassword = "StrongPass1"
	reauthWriteWrongPassword   = "WrongPassword1"
)

var errReauthWriteRefusedByTest = errors.New("test: the write behind a passed re-auth was refused")

// refuseReauthWriteOnce fails the first matching statement with a storage error,
// before gorm runs it, and reports whether it fired. statement is "update" or
// "delete"; an update matches only when it writes carries into table, which
// singles out the one compare-and-set the action under test performs.
func refuseReauthWriteOnce(t *testing.T, database *gorm.DB, statement string, table string, carries string) *atomic.Bool {
	t.Helper()
	fired := &atomic.Bool{}
	name := "test:settings-reauth-refuse-" + statement + "-" + table
	refuse := func(tx *gorm.DB) {
		if tx.Statement.Table != table {
			return
		}
		if carries != "" {
			updates, ok := tx.Statement.Dest.(map[string]any)
			if !ok {
				return
			}
			if _, writes := updates[carries]; !writes {
				return
			}
		}
		if !fired.CompareAndSwap(false, true) {
			return
		}
		_ = tx.AddError(errReauthWriteRefusedByTest)
	}

	var err error
	switch statement {
	case "update":
		err = database.Callback().Update().Before("gorm:update").Register(name, refuse)
		t.Cleanup(func() { _ = database.Callback().Update().Remove(name) })
	case "delete":
		err = database.Callback().Delete().Before("gorm:delete").Register(name, refuse)
		t.Cleanup(func() { _ = database.Callback().Delete().Remove(name) })
	default:
		t.Fatalf("refuseReauthWriteOnce: unknown statement %q", statement)
	}
	if err != nil {
		t.Fatalf("register the refusing %s callback: %v", statement, err)
	}
	return fired
}

// reauthWriteCase drives one settings.reauth writing caller.
type reauthWriteCase struct {
	// send submits the action with password.
	send func(t *testing.T, password string) *http.Response
	// wantApplied is the status of a committed write.
	wantApplied int
	// refuse arms the refusal of the action's own write.
	refuse func(t *testing.T) *atomic.Bool
	// wantRefused is the status the refused write answers.
	wantRefused int
	// stillIntact asserts the refused write left no trace.
	stillIntact func(t *testing.T)
	// rearm restores what the committed write consumed, so the action can run
	// again: a session at the version the write stored, a disabled 2FA, a
	// second identity, an account.
	rearm func(t *testing.T)
}

func spendSettingsReauthFailures(t *testing.T, action reauthWriteCase, count int, label string) {
	t.Helper()
	for attempt := range count {
		resp := action.send(t, reauthWriteWrongPassword)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: wrong password %d: status = %d, want 401", label, attempt+1, resp.StatusCode)
		}
	}
}

// assertRefusedWriteKeepsTheSettingsReauthCount spends all but one attempt,
// sends the correct password into a refused write, and proves the count
// survived: one more wrong password reaches the limit, so the correct password
// is then refused as rate limited.
func assertRefusedWriteKeepsTheSettingsReauthCount(t *testing.T, action reauthWriteCase) {
	t.Helper()
	spendSettingsReauthFailures(t, action, services.DefaultSettingsReauthAttemptsLimit-1, "before the refused write")

	fired := action.refuse(t)
	resp := action.send(t, reauthWriteCorrectPassword)
	if !fired.Load() {
		t.Fatalf("anchor: the correct-password request never reached the action's write (status %d)", resp.StatusCode)
	}
	if resp.StatusCode != action.wantRefused {
		t.Fatalf("refused write: status = %d, want %d", resp.StatusCode, action.wantRefused)
	}
	action.stillIntact(t)

	// One failure short of the limit was spent before the refused write. Had
	// that write cleared the count, the correct password below would pass.
	spendSettingsReauthFailures(t, action, 1, "the failure that reaches the limit")
	resp = action.send(t, reauthWriteCorrectPassword)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct password after the refused write kept the count: status = %d, want 429", resp.StatusCode)
	}
	action.stillIntact(t)
}

// assertCommittedWriteClearsTheSettingsReauthCount is the positive control: a
// committed write clears the count, so a second round of all-but-one failures
// still leaves the correct password its pass.
func assertCommittedWriteClearsTheSettingsReauthCount(t *testing.T, action reauthWriteCase) {
	t.Helper()
	for round := range 2 {
		label := "round " + strconv.Itoa(round+1)
		spendSettingsReauthFailures(t, action, services.DefaultSettingsReauthAttemptsLimit-1, label)
		resp := action.send(t, reauthWriteCorrectPassword)
		if resp.StatusCode != action.wantApplied {
			t.Fatalf("%s: correct password: status = %d, want %d — the committed write before it did not clear the count", label, resp.StatusCode, action.wantApplied)
		}
		action.rearm(t)
	}
}

func runSettingsReauthWriteCases(t *testing.T, build func(t *testing.T, email string) reauthWriteCase, slug string) {
	t.Run("refused write keeps the count", func(t *testing.T) {
		assertRefusedWriteKeepsTheSettingsReauthCount(t, build(t, "reauth-refused-"+slug+"@example.com"))
	})
	t.Run("positive control: committed write clears the count", func(t *testing.T) {
		assertCommittedWriteClearsTheSettingsReauthCount(t, build(t, "reauth-committed-"+slug+"@example.com"))
	})
}

func storedSettingsReauthUser(t *testing.T, database *gorm.DB, userID uint) (models.User, bool) {
	t.Helper()
	var user models.User
	err := database.First(&user, userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.User{}, false
	}
	if err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return user, true
}

func TestClearDataResetsSettingsReauthOnlyAfterTheWipeCommits(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		ctx := newSettingsSecurityTestContext(t, email)
		if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Update("cycle_length", 33).Error; err != nil {
			t.Fatalf("seed a non-default cycle length: %v", err)
		}
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				return settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
					"password": {password},
				}, map[string]string{"Accept": "application/json"})
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				return refuseReauthWriteOnce(t, ctx.database, "update", "users", "cycle_length")
			},
			wantRefused: http.StatusInternalServerError,
			stillIntact: func(t *testing.T) {
				if stored, _ := storedSettingsReauthUser(t, ctx.database, ctx.user.ID); stored.CycleLength != 33 {
					t.Fatalf("cycle_length = %d after a refused wipe, want 33", stored.CycleLength)
				}
			},
			rearm: func(t *testing.T) { ctx.refreshAuthCookie(t) },
		}
	}, "clear-data")
}

func TestDeleteAccountResetsSettingsReauthOnlyAfterTheDeleteCommits(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		ctx := newSettingsSecurityTestContext(t, email)
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				return settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{
					"password": {password},
				}, map[string]string{"Accept": "application/json"})
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				return refuseReauthWriteOnce(t, ctx.database, "delete", "users", "")
			},
			wantRefused: http.StatusInternalServerError,
			stillIntact: func(t *testing.T) {
				if _, exists := storedSettingsReauthUser(t, ctx.database, ctx.user.ID); !exists {
					t.Fatal("a refused delete removed the account")
				}
			},
			// The budget is keyed by account id and client, so the account is
			// recreated under the id the delete removed: the budget it finds is
			// the one the delete left behind.
			rearm: func(t *testing.T) {
				if _, exists := storedSettingsReauthUser(t, ctx.database, ctx.user.ID); exists {
					t.Fatal("anchor: the delete answered success but the account is still stored")
				}
				hash, err := bcrypt.GenerateFromPassword([]byte(reauthWriteCorrectPassword), bcrypt.MinCost)
				if err != nil {
					t.Fatalf("hash the recreated owner's password: %v", err)
				}
				recreated := ctx.user
				recreated.PasswordHash = string(hash)
				recreated.AuthSessionVersion = 1
				recreated.CreatedAt = time.Now().UTC()
				if err := ctx.database.Create(&recreated).Error; err != nil {
					t.Fatalf("recreate the owner under id %d: %v", ctx.user.ID, err)
				}
				ctx.user = recreated
				ctx.authCookie = issueAuthCookieForUser(t, recreated)
				ctx.csrfCookie, ctx.csrfToken = loadSettingsCSRFContext(t, ctx.app, ctx.authCookie)
			},
		}
	}, "delete-account")
}

func TestRegenerateRecoveryCodeResetsSettingsReauthOnlyAfterTheRotationCommits(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		ctx := newSettingsSecurityTestContext(t, email)
		priorHash := loadUserRecoveryCodeHash(t, ctx)
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				return settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
					"password": {password},
				}, map[string]string{"Accept": "application/json"})
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				return refuseReauthWriteOnce(t, ctx.database, "update", "users", "recovery_code_hash")
			},
			wantRefused: http.StatusInternalServerError,
			stillIntact: func(t *testing.T) {
				if got := loadUserRecoveryCodeHash(t, ctx); got != priorHash {
					t.Fatal("a refused rotation replaced the recovery-code hash")
				}
			},
			rearm: func(t *testing.T) { ctx.refreshAuthCookie(t) },
		}
	}, "recovery-code")
}

func TestChangePasswordResetsSettingsReauthOnlyAfterTheChangeCommits(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		ctx := newSettingsSecurityTestContext(t, email)
		stored, _ := storedSettingsReauthUser(t, ctx.database, ctx.user.ID)
		priorHash := stored.PasswordHash
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				return settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
					"current_password": {password},
					"new_password":     {"EvenStronger2"},
					"confirm_password": {"EvenStronger2"},
				}, map[string]string{"Accept": "application/json"})
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				return refuseReauthWriteOnce(t, ctx.database, "update", "users", "password_hash")
			},
			wantRefused: http.StatusInternalServerError,
			stillIntact: func(t *testing.T) {
				if stored, _ := storedSettingsReauthUser(t, ctx.database, ctx.user.ID); stored.PasswordHash != priorHash {
					t.Fatal("a refused change replaced the password hash")
				}
			},
			// The committed change stored the new password and revoked the
			// session; the old password is written back directly, outside the
			// service, so the budget the next round finds is the one the change
			// left behind.
			rearm: func(t *testing.T) {
				if stored, _ := storedSettingsReauthUser(t, ctx.database, ctx.user.ID); stored.PasswordHash == priorHash {
					t.Fatal("anchor: the change answered success but the password hash is unchanged")
				}
				if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Update("password_hash", priorHash).Error; err != nil {
					t.Fatalf("restore the owner's password: %v", err)
				}
				ctx.refreshAuthCookie(t)
			},
		}
	}, "password-change")
}

func TestTOTPEnrollmentResetsSettingsReauthOnlyAfterTheEnrollmentCommits(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		ctx := newTOTPSettingsContext(t, email)
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				setupCookie, code, _ := enrollmentFixture(t, ctx)
				return send2FARequest(t, ctx, twoFARequest{
					method:      http.MethodPut,
					contentType: "application/x-www-form-urlencoded",
					body:        url.Values{"password": {password}, "code": {code}, "csrf_token": {ctx.csrfToken}}.Encode(),
					setupCookie: setupCookie,
					withSession: true,
				})
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				return refuseReauthWriteOnce(t, ctx.database, "update", "users", "totp_enabled")
			},
			wantRefused: http.StatusInternalServerError,
			stillIntact: func(t *testing.T) {
				if totpEnabledInDatabase(t, ctx) {
					t.Fatal("a refused enrollment turned 2FA on")
				}
			},
			rearm: func(t *testing.T) {
				if !totpEnabledInDatabase(t, ctx) {
					t.Fatal("anchor: the enrollment answered success but 2FA is off")
				}
				ctx.refreshAuthCookie(t)
				if err := getTOTPServiceForTest(ctx.database).DisableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion); err != nil {
					t.Fatalf("turn 2FA back off: %v", err)
				}
				ctx.refreshAuthCookie(t)
			},
		}
	}, "totp-enroll")
}

func TestOIDCIdentityUnlinkResetsSettingsReauthOnlyAfterTheUnlinkCommits(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		var workflow *realIdentityOIDCWorkflowService
		fixture := newOIDCStepupFixtureWithOptions(t, email, onboardingTestAppOptions{}, func(stub *stubOIDCWorkflowService) OIDCWorkflowService {
			workflow = &realIdentityOIDCWorkflowService{stubOIDCWorkflowService: stub}
			return workflow
		})
		repositories := db.NewRepositories(fixture.database)
		workflow.real = services.NewOIDCLoginService(enabledOIDCProviderForLinkTest{}, repositories.OIDCIdentities, repositories.Users, nil)
		giveLinkFixtureAPassword(t, fixture)

		// A second identity, so the positive control has one left to unlink
		// after the first round's unlink committed.
		second := models.OIDCIdentity{UserID: fixture.user.ID, Issuer: fixture.identity.Issuer, Subject: "reauth-reset-second-subject", CreatedAt: time.Now().UTC()}
		if err := fixture.database.Create(&second).Error; err != nil {
			t.Fatalf("link a second identity: %v", err)
		}
		targets := []uint{fixture.identity.ID, second.ID}

		identityStored := func(t *testing.T, id uint) bool {
			t.Helper()
			var count int64
			if err := fixture.database.Model(&models.OIDCIdentity{}).Where("id = ?", id).Count(&count).Error; err != nil {
				t.Fatalf("count identity %d: %v", id, err)
			}
			return count == 1
		}
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				resp := deleteOIDCIdentity(t, fixture, strconv.FormatUint(uint64(targets[0]), 10), password, true)
				t.Cleanup(func() { _ = resp.Body.Close() })
				return resp
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				return refuseReauthWriteOnce(t, fixture.database, "delete", "oidc_identities", "")
			},
			wantRefused: http.StatusServiceUnavailable,
			stillIntact: func(t *testing.T) {
				if !identityStored(t, targets[0]) {
					t.Fatal("a refused unlink removed the identity")
				}
			},
			rearm: func(t *testing.T) {
				if identityStored(t, targets[0]) {
					t.Fatal("anchor: the unlink answered success but the identity is still stored")
				}
				targets = targets[1:]
				reloaded, _ := storedSettingsReauthUser(t, fixture.database, fixture.user.ID)
				fixture.user = reloaded
				fixture.authCookie = issueAuthCookieForUser(t, reloaded)
			},
		}
	}, "oidc-unlink")
}

// TestClearDataResetsSettingsReauthWhenOnlyTheSessionCannotFollowTheWipe covers
// the wipe that commits but cannot carry this device's session past it: the data
// is gone, so the password it was gated on counts as spent and the budget clears,
// as it does after an enrollment or an unlink whose re-issue fails. The re-issue
// is refused through the role gate, like
// TestClearAllDataAnswersARefusedSessionReissueByFormat, which is also why the
// probe injects the session user directly: no route carries a non-owner session
// into ClearAllData.
func TestClearDataResetsSettingsReauthWhenOnlyTheSessionCannotFollowTheWipe(t *testing.T) {
	_, database, handler := newSettingsMutationStepupApp(t, newStubOIDCWorkflowService(true))
	hash, err := bcrypt.GenerateFromPassword([]byte(reauthWriteCorrectPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash the probe password: %v", err)
	}
	user := models.User{
		Email:               "reauth-reset-wipe-signed-out@example.com",
		LocalAuthEnabled:    true,
		PasswordHash:        string(hash),
		Role:                "partner",
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create the probe account: %v", err)
	}
	app := fiber.New()
	app.Post("/__probe/clear-data", func(c fiber.Ctx) error {
		c.Locals(contextUserKey, &user)
		return handler.ClearAllData(c)
	})

	wantVersion := 1
	assertCommittedWriteClearsTheSettingsReauthCount(t, reauthWriteCase{
		send: func(t *testing.T, password string) *http.Response {
			request := httptest.NewRequest(http.MethodPost, "/__probe/clear-data", strings.NewReader(url.Values{"password": {password}}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")
			resp := mustAppResponse(t, app, request)
			t.Cleanup(func() { _ = resp.Body.Close() })
			return resp
		},
		// The signed-out refusal's JSON arm; the wipe behind it is proven in rearm.
		wantApplied: http.StatusUnauthorized,
		rearm: func(t *testing.T) {
			wantVersion++
			stored, _ := storedSettingsReauthUser(t, database, user.ID)
			if stored.AuthSessionVersion != wantVersion {
				t.Fatalf("anchor: auth_session_version = %d, want %d: the wipe never committed, so the refusal was not the session re-issue's", stored.AuthSessionVersion, wantVersion)
			}
		},
	})
}

// TestClearDataPrecheckResetsSettingsReauthAtOnce pins the pre-check's own
// reset: it writes nothing, so the correct password is all it authorised and the
// count clears as soon as it is confirmed.
func TestClearDataPrecheckResetsSettingsReauthAtOnce(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "reauth-reset-precheck@example.com")
	assertCommittedWriteClearsTheSettingsReauthCount(t, reauthWriteCase{
		send: func(t *testing.T, password string) *http.Response {
			return settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe/validate", url.Values{
				"password": {password},
			}, map[string]string{"Accept": "application/json"})
		},
		wantApplied: http.StatusOK,
		rearm:       func(*testing.T) {},
	})
}

// TestOIDCIdentityLinkStepupStartResetsSettingsReauthOnlyOnceItRedirects pins
// the step-up start's reset: the start writes nothing and the provider callback
// that writes the link does not re-ask the password, so the count clears once
// the redirect is issued, and a start the provider refused keeps it.
func TestOIDCIdentityLinkStepupStartResetsSettingsReauthOnlyOnceItRedirects(t *testing.T) {
	runSettingsReauthWriteCases(t, func(t *testing.T, email string) reauthWriteCase {
		fixture := newOIDCStepupFixture(t, email)
		giveLinkFixtureAPassword(t, fixture)
		var reached *atomic.Bool
		return reauthWriteCase{
			send: func(t *testing.T, password string) *http.Response {
				resp := postOIDCIdentityLinkStepupStartWithPassword(t, fixture, password)
				t.Cleanup(func() { _ = resp.Body.Close() })
				if reached != nil && fixture.oidcStub.lastReauthState != "" {
					reached.Store(true)
				}
				return resp
			},
			wantApplied: http.StatusOK,
			refuse: func(t *testing.T) *atomic.Bool {
				reached = &atomic.Bool{}
				fixture.oidcStub.lastReauthState = ""
				fixture.oidcStub.reauthStartErr = services.ErrOIDCUnavailable
				return reached
			},
			wantRefused: http.StatusServiceUnavailable,
			// The start writes nothing, refused or not.
			stillIntact: func(*testing.T) {},
			rearm:       func(*testing.T) {},
		}
	}, "oidc-link-start")
}
