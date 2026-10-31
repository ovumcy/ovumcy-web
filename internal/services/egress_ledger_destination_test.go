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
// It is host[:port] and nothing else: no path segment is ever shown, because a
// segment's position says nothing about whether it is the credential (Telegram
// puts the bot token in the FIRST one).
func TestWebhookDestinationDisplayShowsNoPartOfThePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want string
		// leaks are fragments of the raw value that must never be shown.
		leaks []string
	}{
		{
			name:  "telegram bot token in the first segment",
			raw:   "https://api.telegram.org/bot123456789:AAFixtureTokenValue0000/sendMessage?chat_id=42",
			want:  "api.telegram.org",
			leaks: []string{"bot", "123456789", "AAFixture", "sendMessage", "chat_id", "/"},
		},
		{
			name:  "slack-like",
			raw:   "https://chat-hooks.example.test/services/TEAM0000/BOT00000/fixture-token-wxyz",
			want:  "chat-hooks.example.test",
			leaks: []string{"services", "TEAM0000", "BOT00000", "fixture-token", "wxyz"},
		},
		{
			name:  "ntfy-like single segment",
			raw:   "https://ntfy.sh/my-topic",
			want:  "ntfy.sh",
			leaks: []string{"my-topic", "/"},
		},
		{
			name: "no path",
			raw:  "https://example.com",
			want: "example.com",
		},
		{
			name:  "non-default port is kept",
			raw:   "https://hooks.example.com:8443/services/abc123/a1b2e5f6c3d4",
			want:  "hooks.example.com:8443",
			leaks: []string{"services", "abc123", "c3d4"},
		},
		{
			name: "default https port is omitted",
			raw:  "https://hooks.example.com:443/",
			want: "hooks.example.com",
		},
		{
			name: "default http port is omitted",
			raw:  "http://hooks.example.com:80/x",
			want: "hooks.example.com",
		},
		{
			name: "port 443 on http is not the default",
			raw:  "http://hooks.example.com:443/",
			want: "hooks.example.com:443",
		},
		{
			name: "ipv6 host keeps its brackets",
			raw:  "https://[2001:db8::1]:8443/a/b",
			want: "[2001:db8::1]:8443",
		},
		{
			name:  "userinfo, query and fragment are never shown",
			raw:   "https://alice:hunter2@example.com/hook?token=SECRETQUERY#frag",
			want:  "example.com",
			leaks: []string{"alice", "hunter2", "@", "SECRETQUERY", "frag"},
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
