package api

import (
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxRequestTimezoneLength = 128

// maxCachedRequestTimezones bounds each half of the request-timezone cache.
// The tz database holds roughly 600 names, so the loaded half does not evict
// in ordinary traffic; the bound exists because the key is client input. Even
// the successes are not a finite set: on Linux "Europe//Moscow" opens the same
// zoneinfo file as "Europe/Moscow" and keeps the spelling it was given.
const maxCachedRequestTimezones = 1024

// requestTimezoneLocations memoizes time.LoadLocation for the timezone names a
// request carries (the X-Ovumcy-Timezone header, the ovumcy_tz cookie, a form
// field). LanguageMiddleware resolves the header on every request, rate-limited
// or not, and an uncached load reads and parses a zoneinfo file each time.
var requestTimezoneLocations = newRequestTimezoneCache(time.LoadLocation)

// requestTimezoneCache remembers loaded zones and names the tz database does
// not know, each in its own bounded map, so a flood of refused names can only
// evict other refused names and never the zones owners actually send. At the
// bound an arbitrary entry is evicted (map iteration order is randomized). A
// name the cache has
// not seen still costs one lookup: the header admits any short identifier, so
// no bounded cache can absorb distinct names — it removes the repeats.
type requestTimezoneCache struct {
	mu      sync.RWMutex
	load    func(string) (*time.Location, error)
	loaded  map[string]*time.Location
	refused map[string]struct{}
}

func newRequestTimezoneCache(load func(string) (*time.Location, error)) *requestTimezoneCache {
	return &requestTimezoneCache{
		load:    load,
		loaded:  make(map[string]*time.Location),
		refused: make(map[string]struct{}),
	}
}

func (cache *requestTimezoneCache) lookup(name string) (*time.Location, bool) {
	cache.mu.RLock()
	location, loaded := cache.loaded[name]
	_, refused := cache.refused[name]
	cache.mu.RUnlock()
	if loaded {
		return location, true
	}
	if refused {
		return nil, false
	}

	location, err := cache.load(name)

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err != nil {
		if isUnknownTimezone(err) {
			insertBounded(cache.refused, name, struct{}{})
		}
		return nil, false
	}
	insertBounded(cache.loaded, name, location)
	return location, true
}

// isUnknownTimezone reports the one refusal worth remembering: every zoneinfo
// source answered that the name does not exist. Any other error — a read
// failure, a descriptor limit reached under load — can pass on the next
// request, so it is not cached and a transient fault cannot pin an owner's
// zone to the fallback. The stdlib returns this error unwrapped, so its text
// is the only handle; TestUnknownTimezoneRecognizesTheStdlibRefusal fails if
// a Go release rewords it.
func isUnknownTimezone(err error) bool {
	return strings.HasPrefix(err.Error(), "unknown time zone ")
}

func insertBounded[V any](entries map[string]V, key string, value V) {
	if _, present := entries[key]; !present && len(entries) >= maxCachedRequestTimezones {
		for evicted := range entries {
			delete(entries, evicted)
			break
		}
	}
	entries[key] = value
}

func resolveRequestLocation(headerValue string, cookieValue string, fallback *time.Location) (*time.Location, string) {
	if fallback == nil {
		fallback = time.UTC
	}

	if location, canonical, ok := parseRequestTimezone(headerValue); ok {
		return location, canonical
	}
	if location, _, ok := parseRequestTimezone(cookieValue); ok {
		return location, ""
	}
	return fallback, ""
}

func parseRequestTimezone(raw string) (*time.Location, string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > maxRequestTimezoneLength {
		return nil, "", false
	}
	if strings.Contains(value, "%") {
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return nil, "", false
		}
		value = strings.TrimSpace(decoded)
	}
	if !isSafeTimezoneIdentifier(value) {
		return nil, "", false
	}
	// Reject the "Local" special token by input. time.LoadLocation("Local")
	// returns the server's own zone (time.Local); the canonical check below only
	// catches it when time.Local.String() == "Local", which holds when TZ is
	// unset (Windows dev, default CI runner) but NOT when an operator sets TZ —
	// common on self-hosted Linux Docker — where the loaded zone stringifies to
	// its real name and slips through, letting a client pin requests to the
	// server's timezone. Guarding the input keeps rejection deterministic across
	// platforms and TZ configs.
	if strings.EqualFold(value, "Local") {
		return nil, "", false
	}

	location, ok := requestTimezoneLocations.lookup(value)
	if !ok {
		return nil, "", false
	}
	canonical := strings.TrimSpace(location.String())
	if canonical == "" || strings.EqualFold(canonical, "Local") {
		return nil, "", false
	}

	return location, canonical, true
}

func isSafeTimezoneIdentifier(value string) bool {
	for _, ch := range value {
		switch {
		case ch >= 'a' && ch <= 'z':
			continue
		case ch >= 'A' && ch <= 'Z':
			continue
		case ch >= '0' && ch <= '9':
			continue
		case ch == '/', ch == '_', ch == '+', ch == '-':
			continue
		default:
			return false
		}
	}
	return true
}
