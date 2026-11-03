package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The destination line is the one place a stored webhook URL comes near a page.
// It is host[:port] plus a MASKED path: the first segment in clear, then "/…/",
// then the last four characters. A first segment that looks like a credential
// (a ':' in it, or 20+ characters of [A-Za-z0-9_-]) is masked too, because
// Telegram puts the bot token there. Query, fragment and userinfo never show.
func TestWebhookDestinationDisplayMasksThePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
		// leaks are fragments of the raw value that must never be shown.
		leaks []string
	}{
		{
			name:  "slack example from the decision",
			raw:   "https://hooks.slack.com/services/T000/B000/XXXXabcd",
			want:  "hooks.slack.com/services/…/abcd",
			leaks: []string{"T000", "B000", "XXXX"},
		},
		{
			name:  "telegram bot token in the first segment",
			raw:   "https://api.telegram.org/bot123456789:AAFixtureTokenValue0000/sendMessage?chat_id=42",
			want:  "api.telegram.org/…/sage",
			leaks: []string{"bot", "123456789", "AAFixture", "TokenValue", "0000", "chat_id", "Message"},
		},
		{
			name:  "slack-like",
			raw:   "https://chat-hooks.example.test/services/TEAM0000/BOT00000/fixture-token-wxyz",
			want:  "chat-hooks.example.test/services/…/wxyz",
			leaks: []string{"TEAM0000", "BOT00000", "fixture-token"},
		},
		{
			name:  "single segment is masked, never shown whole",
			raw:   "https://ntfy.sh/my-private-topic",
			want:  "ntfy.sh/…/opic",
			leaks: []string{"my-private", "private-topic"},
		},
		{
			name:  "single short segment shows no tail",
			raw:   "https://ntfy.sh/abc",
			want:  "ntfy.sh/…",
			leaks: []string{"abc"},
		},
		{
			name:  "a tail that would leave fewer hidden characters than shown is dropped",
			raw:   "https://ntfy.sh/abcdefg",
			want:  "ntfy.sh/…",
			leaks: []string{"abc", "defg"},
		},
		{
			name:  "two short segments show the first and no tail",
			raw:   "https://example.com/hooks/ab",
			want:  "example.com/hooks/…",
			leaks: []string{"/ab"},
		},
		{
			name:  "first segment of 20 token characters is credential-shaped",
			raw:   "https://example.com/AbCdEfGhIjKlMnOpQrSt/send/1234abcd",
			want:  "example.com/…/abcd",
			leaks: []string{"AbCdEf", "OpQrSt", "send"},
		},
		{
			name:  "first segment of 19 token characters is a route name",
			raw:   "https://example.com/AbCdEfGhIjKlMnOpQrS/send/1234abcd",
			want:  "example.com/AbCdEfGhIjKlMnOpQrS/…/abcd",
			leaks: []string{"send", "1234"},
		},
		{
			name:  "long segment with a dot is not token-shaped",
			raw:   "https://example.com/api.version-twenty-one/send/1234abcd",
			want:  "example.com/api.version-twenty-one/…/abcd",
			leaks: []string{"send/", "1234"},
		},
		{
			name:  "percent-encoded colon still counts as a colon",
			raw:   "https://example.com/bot123%3AABCDEF/send/1234abcd",
			want:  "example.com/…/abcd",
			leaks: []string{"bot123", "%3A", "ABCDEF"},
		},
		{
			name:  "percent-encoded characters are displayed as stored",
			raw:   "https://example.com/hooks/a%20b/long%2Ftoken%20end",
			want:  "example.com/hooks/…/0end",
			leaks: []string{"a%20b", "long"},
		},
		{
			name:  "trailing slash is ignored",
			raw:   "https://hooks.slack.com/services/T000/B000/XXXXabcd/",
			want:  "hooks.slack.com/services/…/abcd",
			leaks: []string{"abcd/"},
		},
		{
			name: "root path is host only",
			raw:  "https://example.com/",
			want: "example.com",
		},
		{
			name: "repeated slashes are host only",
			raw:  "https://example.com//",
			want: "example.com",
		},
		{
			name: "empty path with a query is host only",
			raw:  "https://example.com?token=SECRETQUERY",
			want: "example.com",
		},
		{
			name: "no path",
			raw:  "https://example.com",
			want: "example.com",
		},
		{
			name:  "non-default port is kept",
			raw:   "https://hooks.example.com:8443/services/abc123/a1b2e5f6c3d4",
			want:  "hooks.example.com:8443/services/…/c3d4",
			leaks: []string{"abc123", "a1b2e5f6"},
		},
		{
			name: "default https port is omitted",
			raw:  "https://hooks.example.com:443/",
			want: "hooks.example.com",
		},
		{
			name: "default http port is omitted",
			raw:  "http://hooks.example.com:80/x",
			want: "hooks.example.com/…",
		},
		{
			name: "port 443 on http is not the default",
			raw:  "http://hooks.example.com:443/",
			want: "hooks.example.com:443",
		},
		{
			name: "ipv6 host keeps its brackets",
			raw:  "https://[2001:db8::1]:8443/a/b",
			want: "[2001:db8::1]:8443/a/…",
		},
		{
			name:  "userinfo, query and fragment are never shown",
			raw:   "https://alice:hunter2@example.com/hook?token=SECRETQUERY#frag",
			want:  "example.com/…",
			leaks: []string{"alice", "hunter2", "@", "SECRETQUERY", "frag", "hook", "?", "#"},
		},
		{
			name: "unparseable value shows nothing",
			raw:  "http://[::1",
			want: "",
		},
		{
			name: "no host shows nothing",
			raw:  "https:///only/a/path",
			want: "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := webhookDestinationDisplay(testCase.raw)
			if got != testCase.want {
				t.Fatalf("want %q, got %q", testCase.want, got)
			}
			for _, leak := range testCase.leaks {
				if strings.Contains(got, leak) {
					t.Fatalf("%q leaked %q", got, leak)
				}
			}
		})
	}
}

// A Telegram destination carries a usable bot token in its first segment. No
// part of the token -- the id, the secret half, or any 5+ character run of
// either -- may appear anywhere in the shown line.
func TestWebhookDestinationDisplayNeverShowsATelegramToken(t *testing.T) {
	t.Parallel()

	const botID = "123456789"
	const secret = "AAHfixtureSecret_valueXYZ-0123456789"
	got := webhookDestinationDisplay("https://api.telegram.org/bot" + botID + ":" + secret + "/sendMessage?chat_id=42")

	if got != "api.telegram.org/…/sage" {
		t.Fatalf("unexpected line %q", got)
	}
	token := botID + ":" + secret
	for _, piece := range []string{token, botID, secret, "bot" + botID} {
		if strings.Contains(got, piece) {
			t.Fatalf("%q carries the token piece %q", got, piece)
		}
	}
	for start := 0; start+5 <= len(token); start++ {
		if run := token[start : start+5]; strings.Contains(got, run) {
			t.Fatalf("%q carries the token run %q", got, run)
		}
	}
}

// url.Parse refuses a broken escape before a path reaches the mask, so this arm
// is pinned on the segment check itself: an undecodable segment fails closed.
func TestCredentialShapedSegmentFailsClosedOnABrokenEscape(t *testing.T) {
	t.Parallel()

	if !isCredentialShapedSegment("hooks%zz") {
		t.Fatal("a segment whose escape does not decode is shown in clear")
	}
	if isCredentialShapedSegment("services") {
		t.Fatal("a plain route name is treated as a credential")
	}
}

// The heading names a route as switched on while this instance runs no reminder
// pass, and the row beside it says the instance sends nothing from here. Both are
// true together only because the heading is worded as a statement about the
// switches and defers to the rows for what is sent -- so this pins the pair of
// states that can co-occur, and the wording that makes them agree.
func TestEgressSummaryAndOverlaidRowAgreeOnAQuietInstance(t *testing.T) {
	t.Parallel()

	service := NewEgressLedgerService(
		&stubEgressWebhookDisplay{display: WebhookURLDisplay{
			Readability: WebhookURLReadable,
			Host:        "hooks.example.test",
			Destination: "hooks.example.test:8443",
		}},
		&stubEgressFeedStatus{status: CalendarFeedStatus{Known: true}},
		false,
	)
	ledger := service.BuildEgressLedger(context.Background(), EgressLedgerInput{UserID: 1, WebhookEnabled: true, NotifyPeriod: true})

	if ledger.Webhook.State != EgressWebhookOutboundDisabled {
		t.Fatalf("setup: want the overlaid row, got %q", ledger.Webhook.State)
	}
	if ledger.Section != EgressSectionPathsEnabled {
		t.Fatalf("setup: want the armed heading kept (the operator CLI may still deliver), got %q", ledger.Section)
	}
	if ledger.Webhook.Destination != "hooks.example.test:8443" {
		t.Fatalf("the ledger must carry the destination, got %q", ledger.Webhook.Destination)
	}

	for _, locale := range []string{"en", "de", "fr", "es", "it", "ru"} {
		messages := readLocaleFile(t, locale)
		summary := messages["settings.egress.state.section.paths_enabled"]
		// A bare "a route is enabled" sentence contradicts "sends nothing from
		// here"; the heading must carry a second sentence deferring to the rows.
		if strings.Count(summary, ". ") < 1 || !strings.HasSuffix(summary, ".") {
			t.Errorf("%s: the paths_enabled heading must defer to the rows for what is sent, got %q", locale, summary)
		}
	}
}

func TestItalianFeedRevealCopyIsPlain(t *testing.T) {
	t.Parallel()

	messages := readLocaleFile(t, "it")
	for _, key := range []string{"settings.egress.evidence.feed.none", "settings.egress.evidence.feed.recorded"} {
		if strings.Contains(messages[key], "una tantum") {
			t.Errorf("%s still uses the opaque 'una tantum': %q", key, messages[key])
		}
	}
}

func readLocaleFile(t *testing.T, locale string) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "i18n", "locales", locale+".json"))
	if err != nil {
		t.Fatalf("read %s locale: %v", locale, err)
	}
	messages := map[string]string{}
	if err := json.Unmarshal(raw, &messages); err != nil {
		t.Fatalf("parse %s locale: %v", locale, err)
	}
	return messages
}
