package api

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResolveRequestLocationPrefersValidHeader(t *testing.T) {
	location, cookieValue := resolveRequestLocation("Europe/Moscow", "UTC", time.UTC)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "Europe/Moscow" {
		t.Fatalf("expected header timezone Europe/Moscow, got %q", location.String())
	}
	if cookieValue != "Europe/Moscow" {
		t.Fatalf("expected timezone cookie value Europe/Moscow, got %q", cookieValue)
	}
}

func TestResolveRequestLocationFallsBackToCookieWhenHeaderInvalid(t *testing.T) {
	location, cookieValue := resolveRequestLocation("Europe/Moscow\nInjected", "UTC", time.FixedZone("UTC+3", 3*60*60))
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "UTC" {
		t.Fatalf("expected cookie timezone UTC, got %q", location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no timezone cookie update from invalid header, got %q", cookieValue)
	}
}

func TestResolveRequestLocationAcceptsEncodedCookieTimezone(t *testing.T) {
	location, cookieValue := resolveRequestLocation("", "Europe%2FMoscow", time.UTC)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "Europe/Moscow" {
		t.Fatalf("expected decoded cookie timezone Europe/Moscow, got %q", location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no cookie rewrite for valid cookie fallback, got %q", cookieValue)
	}
}

func TestResolveRequestLocationFallsBackToDefaultWhenNoValidInput(t *testing.T) {
	fallback := time.FixedZone("UTC+4", 4*60*60)
	location, cookieValue := resolveRequestLocation("bad timezone", "also bad", fallback)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != fallback.String() {
		t.Fatalf("expected fallback location %q, got %q", fallback.String(), location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no cookie update on fallback, got %q", cookieValue)
	}
}

func TestResolveRequestLocationUsesUTCFallbackWhenNil(t *testing.T) {
	location, cookieValue := resolveRequestLocation("", "", nil)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "UTC" {
		t.Fatalf("expected UTC fallback, got %q", location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no cookie update for empty inputs, got %q", cookieValue)
	}
}

// TestIsSafeTimezoneIdentifierCharacterClasses pins the allow-list guarding
// which characters may reach time.LoadLocation. Each row isolates one boundary
// of the switch in isSafeTimezoneIdentifier: a character on a range boundary
// ('a','z','A','Z','0','9') or one of the four literal separators
// ('/','_','+','-') is safe, and a character just outside every allowed set is
// rejected. The mutation report flags every range comparison and separator
// equality here as not-covered, so a shifted boundary or a negated equality
// flips at least one expectation. This is a security guard: it keeps control
// characters and stray separators out of a client-supplied timezone before it
// reaches the stdlib loader.
func TestIsSafeTimezoneIdentifierCharacterClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty is trivially safe", "", true},
		{"lowercase boundaries a and z", "az", true},
		{"uppercase boundaries A and Z", "AZ", true},
		{"digit boundaries 0 and 9", "09", true},
		{"slash separator", "Europe/Moscow", true},
		{"underscore separator", "America/New_York", true},
		{"plus separator", "Etc/GMT+3", true},
		{"minus separator", "Etc/GMT-3", true},
		{"space rejected", "Europe Moscow", false},
		{"dot rejected", "Europe.Moscow", false},
		{"colon just above nine rejected", "GMT:0", false},
		{"at just below uppercase A rejected", "@GMT", false},
		{"bracket just above uppercase Z rejected", "GMT[", false},
		{"backtick just below lowercase a rejected", "gmt`", false},
		{"brace just above lowercase z rejected", "gmt{", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSafeTimezoneIdentifier(tc.value); got != tc.want {
				t.Fatalf("isSafeTimezoneIdentifier(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestParseRequestTimezoneRejectsUnsafeInputs(t *testing.T) {
	if _, _, ok := parseRequestTimezone("Local"); ok {
		t.Fatal("expected Local token to be rejected")
	}
	if _, _, ok := parseRequestTimezone("Europe/Moscow\nInjected"); ok {
		t.Fatal("expected newline-containing timezone to be rejected")
	}

	tooLong := strings.Repeat("A", maxRequestTimezoneLength+1)
	if _, _, ok := parseRequestTimezone(tooLong); ok {
		t.Fatal("expected oversized timezone to be rejected")
	}
}

// countingTimezoneLoader stands in for time.LoadLocation: it answers from a
// fixed set of known names, refuses the rest the way the stdlib does, and
// counts every call so a test can tell a cache hit from a fresh load.
type countingTimezoneLoader struct {
	mu    sync.Mutex
	calls map[string]int
	known map[string]bool
	err   error
}

func (loader *countingTimezoneLoader) load(name string) (*time.Location, error) {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	if loader.calls == nil {
		loader.calls = make(map[string]int)
	}
	loader.calls[name]++
	if loader.err != nil {
		return nil, loader.err
	}
	if loader.known[name] {
		return time.FixedZone(name, 3*60*60), nil
	}
	return nil, fmt.Errorf("unknown time zone %s", name)
}

func (loader *countingTimezoneLoader) callsFor(name string) int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	return loader.calls[name]
}

func (cache *requestTimezoneCache) sizes() (int, int) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return len(cache.loaded), len(cache.refused)
}

func TestRequestTimezoneCacheLoadsARepeatedZoneOnce(t *testing.T) {
	t.Parallel()

	loader := &countingTimezoneLoader{known: map[string]bool{"Europe/Moscow": true}}
	cache := newRequestTimezoneCache(loader.load)

	first, ok := cache.lookup("Europe/Moscow")
	if !ok || first == nil {
		t.Fatal("expected Europe/Moscow to load")
	}
	second, ok := cache.lookup("Europe/Moscow")
	if !ok || second != first {
		t.Fatalf("expected the repeated lookup to return the cached location %p, got %p (ok=%v)", first, second, ok)
	}
	if calls := loader.callsFor("Europe/Moscow"); calls != 1 {
		t.Fatalf("expected one load for a repeated zone name, got %d", calls)
	}
}

func TestRequestTimezoneCacheRemembersAnUnknownZoneOnce(t *testing.T) {
	t.Parallel()

	loader := &countingTimezoneLoader{}
	cache := newRequestTimezoneCache(loader.load)

	for range 3 {
		if location, ok := cache.lookup("Nowhere/Unknown"); ok || location != nil {
			t.Fatalf("expected an unknown zone to be refused, got %v (ok=%v)", location, ok)
		}
	}
	if calls := loader.callsFor("Nowhere/Unknown"); calls != 1 {
		t.Fatalf("expected one load for a repeated unknown zone name, got %d", calls)
	}
}

// A load that fails for any reason other than "no such zone" — a read error,
// a descriptor limit under load — must not be remembered, or one transient
// fault would pin every later request naming that zone to the fallback.
func TestRequestTimezoneCacheRetriesATransientLoadError(t *testing.T) {
	t.Parallel()

	loader := &countingTimezoneLoader{err: errors.New("open /usr/share/zoneinfo/Europe/Moscow: too many open files")}
	cache := newRequestTimezoneCache(loader.load)

	for range 2 {
		if _, ok := cache.lookup("Europe/Moscow"); ok {
			t.Fatal("expected the failing load to be refused")
		}
	}
	if calls := loader.callsFor("Europe/Moscow"); calls != 2 {
		t.Fatalf("expected a transient load error to be retried on the next lookup, got %d loads", calls)
	}
	if loaded, refused := cache.sizes(); loaded != 0 || refused != 0 {
		t.Fatalf("expected nothing cached after a transient error, got loaded=%d refused=%d", loaded, refused)
	}
}

func TestRequestTimezoneCacheStaysBoundedUnderDistinctNames(t *testing.T) {
	t.Parallel()

	const flood = 3 * maxCachedRequestTimezones

	known := make(map[string]bool, flood)
	for index := range flood {
		known["Europe/Moscow/"+strconv.Itoa(index)] = true
	}
	loader := &countingTimezoneLoader{known: known}
	cache := newRequestTimezoneCache(loader.load)

	for index := range flood {
		cache.lookup("Nowhere/Invalid" + strconv.Itoa(index))
	}
	loaded, refused := cache.sizes()
	if refused == 0 || refused > maxCachedRequestTimezones {
		t.Fatalf("expected %d distinct unknown names to leave 1..%d refusals cached, got %d",
			flood, maxCachedRequestTimezones, refused)
	}
	if loaded != 0 {
		t.Fatalf("expected no loaded zones from unknown names, got %d", loaded)
	}

	for name := range known {
		cache.lookup(name)
	}
	if loaded, _ := cache.sizes(); loaded == 0 || loaded > maxCachedRequestTimezones {
		t.Fatalf("expected %d distinct loadable names to leave 1..%d zones cached, got %d",
			flood, maxCachedRequestTimezones, loaded)
	}
}

func TestRequestTimezoneCacheIsSafeForConcurrentLookups(t *testing.T) {
	t.Parallel()

	loader := &countingTimezoneLoader{known: map[string]bool{"Europe/Moscow": true}}
	cache := newRequestTimezoneCache(loader.load)

	var wg sync.WaitGroup
	for worker := range 16 {
		wg.Go(func() {
			for index := range 2 * maxCachedRequestTimezones {
				cache.lookup("Europe/Moscow")
				cache.lookup(fmt.Sprintf("Nowhere/W%dN%d", worker, index))
			}
		})
	}
	wg.Wait()

	if location, ok := cache.lookup("Europe/Moscow"); !ok || location == nil {
		t.Fatal("expected Europe/Moscow to resolve after concurrent lookups")
	}
	if _, refused := cache.sizes(); refused > maxCachedRequestTimezones {
		t.Fatalf("expected concurrent refusals to stay within %d, got %d", maxCachedRequestTimezones, refused)
	}
}

func TestUnknownTimezoneRecognizesTheStdlibRefusal(t *testing.T) {
	t.Parallel()

	_, err := time.LoadLocation("Nowhere/Ovumcy_Nonexistent_Zone")
	if err == nil {
		t.Fatal("expected the stdlib to refuse a nonexistent zone")
	}
	if !isUnknownTimezone(err) {
		t.Fatalf("expected the stdlib refusal %q to be recognized as an unknown zone", err)
	}
}

// The request path, not only the cache type, has to go through the cache:
// parseRequestTimezone is what LanguageMiddleware calls on every request.
func TestParseRequestTimezoneResolvesThroughTheSharedCache(t *testing.T) {
	t.Parallel()

	if _, canonical, ok := parseRequestTimezone("Asia/Tbilisi"); !ok || canonical != "Asia/Tbilisi" {
		t.Fatalf("expected Asia/Tbilisi to resolve, got %q (ok=%v)", canonical, ok)
	}
	requestTimezoneLocations.mu.RLock()
	_, cached := requestTimezoneLocations.loaded["Asia/Tbilisi"]
	requestTimezoneLocations.mu.RUnlock()
	if !cached {
		t.Fatal("expected parseRequestTimezone to leave Asia/Tbilisi in the shared request-timezone cache")
	}
}
