package cli

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// TestRunResetPasswordCommandValidatesBeforeReadingStdin covers the exported
// entry point, which is otherwise reached only from main. It also pins the
// ordering that makes the piped form usable in a script: a blank address is
// rejected before anything reads stdin, so a typo does not leave the command
// blocked on input that will never be used. A whitespace-only positional
// argument is treated as absent by parseResetPasswordArgs, same as
// parseUsersDeleteArgs treats one, so this is the same usage refusal as no
// address at all.
func TestRunResetPasswordCommandValidatesBeforeReadingStdin(t *testing.T) {
	if err := RunResetPasswordCommand(db.Config{}, []string{"   "}); err == nil || err.Error() != resetUsage {
		t.Fatalf("expected usage error for a blank address, got %v", err)
	}
}

// TestResetPasswordReaderReadsPipedStdin pins the non-interactive path that
// makes the documented recovery step runnable against the shell-free runtime
// image: `docker exec -i ... reset-password <email>` with the password on
// stdin. Before it existed the command answered "secure password prompt
// requires an interactive terminal" and recovering several accounts after a
// SECRET_KEY rotation could not be scripted at all.
func TestResetPasswordReaderReadsPipedStdin(t *testing.T) {
	t.Parallel()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = read.Close() })

	go func() {
		defer func() { _ = write.Close() }()
		_, _ = io.WriteString(write, "  StrongPass1  \n")
	}()

	password, err := resetPasswordReader(read)()
	if err != nil {
		t.Fatalf("resetPasswordReader on piped stdin: %v", err)
	}
	if string(password) != "StrongPass1" {
		t.Fatalf("password = %q, want %q (surrounding whitespace trimmed to match web auth)", password, "StrongPass1")
	}
}

// TestResetPasswordReaderRejectsMissingStdin covers the typed-nil guard: a nil
// *os.File satisfies io.Reader, so without the explicit check it would reach
// bufio instead of returning an error.
func TestResetPasswordReaderRejectsMissingStdin(t *testing.T) {
	t.Parallel()

	if _, err := resetPasswordReader(nil)(); err == nil {
		t.Fatal("expected an error when stdin is missing")
	}
}

func TestRunResetPasswordCommandRejectsBlankEmail(t *testing.T) {
	t.Parallel()

	err := runResetPasswordCommand(db.Config{}, []string{"   "}, nil, io.Discard)
	if err == nil || err.Error() != resetUsage {
		t.Fatalf("expected usage error for a blank address, got %v", err)
	}
}

func TestRunResetPasswordCommandRejectsInvalidEmail(t *testing.T) {
	t.Parallel()

	err := runResetPasswordCommand(db.Config{}, []string{"not-an-email"}, nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "invalid email address") {
		t.Fatalf("expected invalid email error, got %v", err)
	}
}

// TestParseResetPasswordArgsRejectsNeitherAddress covers the "neither
// supplied" case: no positional email and no --id, mirroring
// parseUsersDeleteArgs's own refusal for the same shape of input.
func TestParseResetPasswordArgsRejectsNeitherAddress(t *testing.T) {
	t.Parallel()

	if _, err := parseResetPasswordArgs(nil); err == nil || err.Error() != resetUsage {
		t.Fatalf("expected usage error for no address, got %v", err)
	}
}

// TestParseResetPasswordArgsRejectsBothAddresses covers the "both supplied"
// case: a bare email and --id together are ambiguous about which one wins,
// so the command refuses rather than picking one silently.
func TestParseResetPasswordArgsRejectsBothAddresses(t *testing.T) {
	t.Parallel()

	if _, err := parseResetPasswordArgs([]string{"owner@example.com", "--id", "7"}); err == nil || err.Error() != resetUsage {
		t.Fatalf("expected usage error for both email and --id, got %v", err)
	}
}

// TestParseResetPasswordArgsAcceptsID mirrors parseUsersDeleteArgs's --id
// parsing exactly: both "--id 7" and "--id=7" spellings, and the id must be a
// positive whole number.
func TestParseResetPasswordArgsAcceptsID(t *testing.T) {
	t.Parallel()

	opts, err := parseResetPasswordArgs([]string{"--id", "7"})
	if err != nil {
		t.Fatalf("parseResetPasswordArgs: %v", err)
	}
	if opts.userID != 7 || opts.email != "" {
		t.Fatalf("expected userID=7 and empty email, got %+v", opts)
	}

	opts, err = parseResetPasswordArgs([]string{"--id=7"})
	if err != nil {
		t.Fatalf("parseResetPasswordArgs: %v", err)
	}
	if opts.userID != 7 {
		t.Fatalf("expected userID=7 from --id=7, got %+v", opts)
	}
}

func TestRunResetPasswordCommandRequiresPasswordPrompt(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-nil-prompt@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-nil-prompt@example.com"},
		nil,
		io.Discard,
	)
	if err == nil || err.Error() != "password prompt is required" {
		t.Fatalf("expected nil prompt error, got %v", err)
	}
}

func TestRunResetPasswordCommandRejectsEmptyPromptedPassword(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-empty-password@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-empty-password@example.com"},
		func() ([]byte, error) { return []byte{}, nil },
		io.Discard,
	)
	if err == nil || err.Error() != "password is required" {
		t.Fatalf("expected empty password error, got %v", err)
	}
}

func TestRunResetPasswordCommandRejectsWeakPassword(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-weak-password@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-weak-password@example.com"},
		func() ([]byte, error) { return []byte("weakpass"), nil },
		io.Discard,
	)
	if err == nil || err.Error() != "password does not meet strength requirements" {
		t.Fatalf("expected weak password error, got %v", err)
	}
}

// TestRunResetPasswordCommandRejectsPasswordOverTheByteLimit is the CLI half of
// the too-long/weak split. Without its own arm this refusal falls through to the
// default branch and reaches the operator as the raw sentinel text, and the
// message itself would be untested — which is how a wrong string ships.
//
// The passphrase is the case the split exists for: 37 characters, so it looks
// far short of any limit, but 73 bytes, and it carries an uppercase letter, a
// lowercase letter and a digit, so length is the only rule it breaks. The
// message names bytes on purpose — unlike the owner-facing copy, an operator
// terminal is a place where a reader can count them.
func TestRunResetPasswordCommandRejectsPasswordOverTheByteLimit(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-long-password@example.com", "StrongPass1")

	passphrase := "Пароль1" + strings.Repeat("ы", 30)
	if runes, bytes := len([]rune(passphrase)), len(passphrase); runes > 72 || bytes <= 72 {
		t.Fatalf("test setup: passphrase is %d runes / %d bytes, want <= 72 runes and > 72 bytes", runes, bytes)
	}

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-long-password@example.com"},
		func() ([]byte, error) { return []byte(passphrase), nil },
		io.Discard,
	)
	if err == nil {
		t.Fatal("expected the over-limit passphrase to be refused")
	}
	if err.Error() == "password does not meet strength requirements" {
		t.Fatal("expected the length refusal, not the composition one: the operator would go looking for a missing character class the password already has")
	}
	if got := err.Error(); got != "password is longer than 72 bytes (bcrypt's input limit); note that non-ASCII characters take more than one byte each" {
		t.Fatalf("unexpected message for an over-limit password: %q", got)
	}

	// The account keeps its original password: a refused reset changes nothing.
	unchanged := loadCLIResetUser(t, databasePath, "cli-reset-long-password@example.com")
	if bcrypt.CompareHashAndPassword([]byte(unchanged.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatal("expected the stored password to be untouched after a refused reset")
	}
}

func TestRunResetPasswordCommandReportsMissingUser(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"missing-reset-user@example.com"},
		func() ([]byte, error) { return []byte("StrongPass2"), nil },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "user missing-reset-user@example.com not found") {
		t.Fatalf("expected missing user error, got %v", err)
	}
}

// TestRunResetPasswordCommandAddressesAccountByID covers the id-addressing
// affordance this command gained to close finding DB-2: `users list`'s id is
// the only handle that reaches a legacy row an address-taking command cannot,
// so reset-password needs the same `--id` form `users delete` and
// `users set-email` already have.
func TestRunResetPasswordCommandAddressesAccountByID(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-by-id@example.com", "StrongPass1")
	userID := loadCLIResetUser(t, databasePath, "cli-reset-by-id@example.com").ID

	var output strings.Builder
	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"--id", strconv.FormatUint(uint64(userID), 10)},
		func() ([]byte, error) { return []byte("EvenStronger2"), nil },
		&output,
	)
	if err != nil {
		t.Fatalf("runResetPasswordCommand by id: unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), "Password reset successful") {
		t.Fatalf("expected success output, got %q", output.String())
	}

	updated := loadCLIResetUser(t, databasePath, "cli-reset-by-id@example.com")
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("EvenStronger2")) != nil {
		t.Fatal("expected the id-addressed reset to update the account's password")
	}
}

// TestRunResetPasswordCommandRejectsUnknownID is the "id that does not exist"
// case: an id with no matching row must be refused by name, not treated as a
// no-op or matched against nothing.
func TestRunResetPasswordCommandRejectsUnknownID(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"--id", "999"},
		func() ([]byte, error) { return []byte("EvenStronger2"), nil },
		io.Discard,
	)
	if err == nil || err.Error() != "no account carries id 999 (see ovumcy users list)" {
		t.Fatalf("expected unknown id error, got %v", err)
	}
}

// TestRunResetPasswordCommandRejectsEmailAndIDTogether is the "both supplied"
// case at the command entry point: parseResetPasswordArgs's refusal must
// actually be reached before any database work, prompt, or password policy
// check runs.
func TestRunResetPasswordCommandRejectsEmailAndIDTogether(t *testing.T) {
	t.Parallel()

	err := runResetPasswordCommand(db.Config{}, []string{"owner@example.com", "--id", "7"}, nil, io.Discard)
	if err == nil || err.Error() != resetUsage {
		t.Fatalf("expected usage error, got %v", err)
	}
}

// TestRunResetPasswordCommandRejectsNoAddress is the "neither supplied" case
// at the command entry point.
func TestRunResetPasswordCommandRejectsNoAddress(t *testing.T) {
	t.Parallel()

	err := runResetPasswordCommand(db.Config{}, nil, nil, io.Discard)
	if err == nil || err.Error() != resetUsage {
		t.Fatalf("expected usage error, got %v", err)
	}
}

// TestMapResetPasswordErrorFormatsAmbiguousEmail is the CLI half of finding
// DB-2's ambiguity refusal: services.AmbiguousEmailError (proven by
// TestAuthServiceForceResetPasswordByEmail/ambiguous_address to be what the
// service returns when a bare address matches more than one row) must be
// rendered as a refusal that names every matching id and points at --id,
// never silently resolved to one of them. Exercised as a pure function
// because the matched-more-than-one-row shape cannot be produced through a
// real, fully migrated database — idx_users_email_normalized forbids it — so
// this is the layer this behaviour is actually reachable and testable at.
func TestMapResetPasswordErrorFormatsAmbiguousEmail(t *testing.T) {
	t.Parallel()

	err := mapResetPasswordError(
		&services.AmbiguousEmailError{Email: "owner@example.com", IDs: []uint{5, 18}},
		resetPasswordOptions{email: "owner@example.com"},
		"owner@example.com",
	)
	want := "email owner@example.com matches 2 accounts (ids 5, 18); retry with --id (see ovumcy users list)"
	if err == nil || err.Error() != want {
		t.Fatalf("mapResetPasswordError() = %v, want %q", err, want)
	}
}

// TestMapResetPasswordErrorMapsRemainingSentinels covers the arms
// mapResetPasswordError reaches only in shapes the CLI's own argument parsing
// and pre-checks otherwise avoid producing today (ErrAuthUserIDRequired is
// dead behind parseResetPasswordArgs's own userID != 0 guarantee, and the
// default wrap is dead behind the switch's own exhaustive sentinels for every
// error the service can actually return) — exercised directly, the same way
// TestMapResetPasswordErrorFormatsAmbiguousEmail is, so the wording is pinned
// regardless of whether today's call sites can reach it.
func TestMapResetPasswordErrorMapsRemainingSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		opts resetPasswordOptions
		want string
	}{
		{
			name: "id required",
			err:  services.ErrAuthUserIDRequired,
			want: "an account id is required (see ovumcy users list)",
		},
		{
			name: "reset invalid",
			err:  services.ErrAuthResetInvalid,
			want: "password is required",
		},
		{
			name: "unmapped error wraps with context",
			err:  errors.New("db exploded"),
			want: "reset password: db exploded",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := mapResetPasswordError(testCase.err, testCase.opts, "owner@example.com")
			if got == nil || got.Error() != testCase.want {
				t.Fatalf("mapResetPasswordError() = %v, want %q", got, testCase.want)
			}
		})
	}
}

func TestRunResetPasswordCommandWrapsPromptReadFailure(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-read-failure@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"cli-reset-read-failure@example.com"},
		func() ([]byte, error) { return nil, errors.New("terminal unavailable") },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "read new password") {
		t.Fatalf("expected wrapped prompt error, got %v", err)
	}
}

// TestRunResetPasswordCommandReportsDatabaseInitFailure covers the operator
// UX when the configured database cannot be opened (e.g. a bad path/config):
// the command must surface a wrapped "database init failed" error rather than
// panic or leak a raw driver error. A directory path is an unopenable SQLite
// target on every platform.
func TestRunResetPasswordCommandReportsDatabaseInitFailure(t *testing.T) {
	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: t.TempDir()},
		[]string{"owner@example.com"},
		func() ([]byte, error) { return []byte("StrongPass1"), nil },
		io.Discard,
	)
	if err == nil {
		t.Fatal("expected an error when the database cannot be opened")
	}
	if !strings.Contains(err.Error(), "database init failed") {
		t.Fatalf("expected a wrapped database-init error, got %v", err)
	}
}
