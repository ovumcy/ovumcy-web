package services

import (
	"net"
	"testing"
)

// The private-address gate used to be a hand-picked list: the ranges someone
// had thought of, each added when a report named it. What that shape cannot
// tell you is which ranges are MISSING — RFC 2544 benchmarking space
// (198.18.0.0/15), IETF protocol assignments (192.0.0.0/24) and the reserved
// 240.0.0.0/4 all route on real networks and were all delivered to as if they
// were the public internet.
//
// The table below is the IANA IPv4/IPv6 Special-Purpose Address Registries
// (plus the two multicast registries), one row per entry, with the verdict the
// gate owes it. It walks the registry rather than the classifier: a range IANA
// registers later is a row, and the row fails until the classifier learns it —
// which is the property a list of "ranges we remembered" cannot have.
//
// The globally reachable entries carry verdictAllowed and are the control arm:
// widening an egress gate is only safe while the addresses that must stay
// deliverable still are. TestIsPrivateIP keeps the wider allowed controls
// (8.8.8.8 and its NAT64 wrapping).

// specialPurposeVerdict is what the gate owes one registry entry.
type specialPurposeVerdict int

const (
	// verdictBlocked: every address in the prefix must be refused. Swept at both
	// prefix bounds, so a rule that is one bit too narrow fails.
	verdictBlocked specialPurposeVerdict = iota
	// verdictAllowed: the entry is globally reachable and must stay deliverable.
	verdictAllowed
	// verdictByEmbeddedIPv4: the prefix wraps an IPv4 destination, so the verdict
	// belongs to the address it wraps and cannot be a property of the prefix.
	// Pinned by the row's two literals instead of by the bounds sweep.
	verdictByEmbeddedIPv4
)

// specialPurposeRange is one registry entry and the verdict it is owed.
type specialPurposeRange struct {
	prefix  string
	name    string
	verdict specialPurposeVerdict
	// blockedExample and allowedExample pin a verdictByEmbeddedIPv4 row: the same
	// prefix wrapping a private IPv4 and wrapping a public one.
	blockedExample string
	allowedExample string
}

// ipv4SpecialPurposeRegistry is the IANA IPv4 Special-Purpose Address Registry
// (RFC 6890 and its updates), plus the IPv4 multicast space, which is a
// registry of its own but is no more a webhook destination than the entries
// above it.
var ipv4SpecialPurposeRegistry = []specialPurposeRange{
	{prefix: "0.0.0.0/8", name: "this network (RFC 1122)", verdict: verdictBlocked},
	{prefix: "0.0.0.0/32", name: "this host on this network (RFC 1122)", verdict: verdictBlocked},
	{prefix: "10.0.0.0/8", name: "private-use (RFC 1918)", verdict: verdictBlocked},
	{prefix: "100.64.0.0/10", name: "shared address space / CGNAT (RFC 6598)", verdict: verdictBlocked},
	{prefix: "127.0.0.0/8", name: "loopback (RFC 1122)", verdict: verdictBlocked},
	{prefix: "169.254.0.0/16", name: "link local (RFC 3927)", verdict: verdictBlocked},
	{prefix: "172.16.0.0/12", name: "private-use (RFC 1918)", verdict: verdictBlocked},
	{prefix: "192.0.0.0/24", name: "IETF protocol assignments (RFC 6890)", verdict: verdictBlocked},
	{prefix: "192.0.0.0/29", name: "IPv4 service continuity / DS-Lite (RFC 7335)", verdict: verdictBlocked},
	{prefix: "192.0.0.8/32", name: "IPv4 dummy address (RFC 7600)", verdict: verdictBlocked},
	// 192.0.0.9 and 192.0.0.10 are registered globally reachable, and are refused
	// anyway as part of the enclosing /24: an egress gate errs toward refusal, and
	// no owner endpoint lives on a protocol anycast address reserved for PCP/TURN.
	{prefix: "192.0.0.9/32", name: "port control protocol anycast (RFC 7723)", verdict: verdictBlocked},
	{prefix: "192.0.0.10/32", name: "TURN anycast (RFC 8155)", verdict: verdictBlocked},
	{prefix: "192.0.0.170/31", name: "NAT64/DNS64 discovery (RFC 8880)", verdict: verdictBlocked},
	{prefix: "192.0.2.0/24", name: "documentation TEST-NET-1 (RFC 5737)", verdict: verdictBlocked},
	{prefix: "192.31.196.0/24", name: "AS112-v4 (RFC 7535)", verdict: verdictAllowed},
	{prefix: "192.52.193.0/24", name: "AMT (RFC 7450)", verdict: verdictAllowed},
	{prefix: "192.88.99.0/24", name: "deprecated 6to4 relay anycast (RFC 7526)", verdict: verdictBlocked},
	{prefix: "192.168.0.0/16", name: "private-use (RFC 1918)", verdict: verdictBlocked},
	{prefix: "192.175.48.0/24", name: "direct delegation AS112 service (RFC 7534)", verdict: verdictAllowed},
	{prefix: "198.18.0.0/15", name: "benchmarking (RFC 2544)", verdict: verdictBlocked},
	{prefix: "198.51.100.0/24", name: "documentation TEST-NET-2 (RFC 5737)", verdict: verdictBlocked},
	{prefix: "203.0.113.0/24", name: "documentation TEST-NET-3 (RFC 5737)", verdict: verdictBlocked},
	{prefix: "240.0.0.0/4", name: "reserved (RFC 1112)", verdict: verdictBlocked},
	{prefix: "255.255.255.255/32", name: "limited broadcast (RFC 8190)", verdict: verdictBlocked},
	{prefix: "224.0.0.0/4", name: "multicast (RFC 5771)", verdict: verdictBlocked},
}

// ipv6SpecialPurposeRegistry is the IANA IPv6 Special-Purpose Address Registry
// plus IPv6 multicast. The transition prefixes carry verdictByEmbeddedIPv4: the
// registry marks their global reachability N/A for exactly the reason the
// classifier decodes them.
var ipv6SpecialPurposeRegistry = []specialPurposeRange{
	{prefix: "::1/128", name: "loopback (RFC 4291)", verdict: verdictBlocked},
	{prefix: "::/128", name: "unspecified (RFC 4291)", verdict: verdictBlocked},
	{
		prefix: "::ffff:0:0/96", name: "IPv4-mapped (RFC 4291)", verdict: verdictByEmbeddedIPv4,
		blockedExample: "::ffff:127.0.0.1", allowedExample: "::ffff:8.8.8.8",
	},
	{
		prefix: "::/96", name: "deprecated IPv4-compatible (RFC 4291)", verdict: verdictByEmbeddedIPv4,
		blockedExample: "::10.0.0.1", allowedExample: "::8.8.8.8",
	},
	{
		prefix: "::ffff:0:0:0/96", name: "IPv4-translated / SIIT (RFC 2765)", verdict: verdictByEmbeddedIPv4,
		blockedExample: "::ffff:0:10.0.0.1", allowedExample: "::ffff:0:8.8.8.8",
	},
	{
		prefix: "64:ff9b::/96", name: "NAT64 well-known prefix (RFC 6052)", verdict: verdictByEmbeddedIPv4,
		blockedExample: "64:ff9b::a00:1", allowedExample: "64:ff9b::808:808",
	},
	{prefix: "64:ff9b:1::/48", name: "local-use NAT64 (RFC 8215)", verdict: verdictBlocked},
	{prefix: "100::/64", name: "discard-only (RFC 6666)", verdict: verdictBlocked},
	{
		prefix: "2001::/32", name: "Teredo (RFC 4380)", verdict: verdictByEmbeddedIPv4,
		// Server 65.54.227.120, client 8.8.8.8 (stored bitwise-inverted as
		// f7f7:f7f7). The textbook Teredo example ends 3fff:fdd2 — client
		// 192.0.2.45 — which this change makes private, so it cannot be the
		// allowed control any more.
		blockedExample: "2001:0:a00:1:0:0:f5ff:fffe", allowedExample: "2001:0:4136:e378:8000:63bf:f7f7:f7f7",
	},
	{prefix: "2001:1::1/128", name: "port control protocol anycast (RFC 7723)", verdict: verdictAllowed},
	{prefix: "2001:1::2/128", name: "TURN anycast (RFC 8155)", verdict: verdictAllowed},
	{prefix: "2001:1::3/128", name: "DNS-SD service registration anycast (RFC 9463)", verdict: verdictAllowed},
	{prefix: "2001:2::/48", name: "benchmarking (RFC 5180)", verdict: verdictBlocked},
	{prefix: "2001:3::/32", name: "AMT (RFC 7450)", verdict: verdictAllowed},
	{prefix: "2001:4:112::/48", name: "AS112-v6 (RFC 7535)", verdict: verdictAllowed},
	{prefix: "2001:20::/28", name: "ORCHIDv2 (RFC 7343)", verdict: verdictBlocked},
	{prefix: "2001:30::/28", name: "drone remote ID (RFC 9374)", verdict: verdictBlocked},
	{prefix: "2001:db8::/32", name: "documentation (RFC 3849)", verdict: verdictBlocked},
	{
		prefix: "2002::/16", name: "6to4 (RFC 3056)", verdict: verdictByEmbeddedIPv4,
		blockedExample: "2002:7f00:1::", allowedExample: "2002:808:808::",
	},
	{prefix: "3fff::/20", name: "documentation (RFC 9637)", verdict: verdictBlocked},
	{prefix: "5f00::/16", name: "segment routing SIDs (RFC 9602)", verdict: verdictBlocked},
	{prefix: "fc00::/7", name: "unique-local (RFC 4193)", verdict: verdictBlocked},
	{prefix: "fe80::/10", name: "link-local unicast (RFC 4291)", verdict: verdictBlocked},
	{prefix: "fec0::/10", name: "deprecated site-local (RFC 3879)", verdict: verdictBlocked},
	{prefix: "ff00::/8", name: "multicast (RFC 4291)", verdict: verdictBlocked},
}

// TestIsPrivateIPWalksTheSpecialPurposeRegistries asserts a verdict for every
// entry of the IANA special-purpose registries, so a range the gate does not
// know is a failing row rather than a line nobody wrote.
func TestIsPrivateIPWalksTheSpecialPurposeRegistries(t *testing.T) {
	registry := make([]specialPurposeRange, 0, len(ipv4SpecialPurposeRegistry)+len(ipv6SpecialPurposeRegistry))
	registry = append(registry, ipv4SpecialPurposeRegistry...)
	registry = append(registry, ipv6SpecialPurposeRegistry...)

	for _, entry := range registry {
		t.Run(entry.name+" "+entry.prefix, func(t *testing.T) {
			if entry.verdict == verdictByEmbeddedIPv4 {
				// The prefix itself decides nothing here: both literals sit inside it
				// and must classify differently, which is what "decoded, not blocked
				// wholesale" means.
				assertPrivateVerdict(t, entry, entry.blockedExample, true)
				assertPrivateVerdict(t, entry, entry.allowedExample, false)
				return
			}
			want := entry.verdict == verdictBlocked
			first, last := prefixBounds(t, entry.prefix)
			assertPrivateVerdict(t, entry, first.String(), want)
			assertPrivateVerdict(t, entry, last.String(), want)
		})
	}
}

// TestGloballyReachableControlsStayDeliverable is the control arm outside the
// registry: ordinary public addresses, and the NAT64 wrapping of one. A gate
// that refuses everything would pass every blocked row above and be useless;
// these are what say it did not.
func TestGloballyReachableControlsStayDeliverable(t *testing.T) {
	controls := []string{
		"8.8.8.8",          // ordinary public IPv4
		"64:ff9b::808:808", // NAT64 wrapping 8.8.8.8
		"93.184.216.34",    // ordinary public IPv4, second sample
		"2606:4700::1",     // ordinary public IPv6
	}
	for _, literal := range controls {
		ip := net.ParseIP(literal)
		if ip == nil {
			t.Fatalf("test setup: %q did not parse as an IP", literal)
		}
		if isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%q) = true, want false: a globally reachable address must stay deliverable", literal)
		}
	}
}

// assertPrivateVerdict checks one address against the verdict its registry entry
// is owed, naming the entry so a failure reads as "which range", not "which
// literal".
func assertPrivateVerdict(t *testing.T, entry specialPurposeRange, literal string, want bool) {
	t.Helper()
	ip := net.ParseIP(literal)
	if ip == nil {
		t.Fatalf("test setup: %q did not parse as an IP", literal)
	}
	if got := isPrivateIP(ip); got != want {
		t.Errorf("isPrivateIP(%q) = %v, want %v — %s %s", literal, got, want, entry.name, entry.prefix)
	}
}

// prefixBounds returns the first and last address of a CIDR block, so a blocked
// entry is checked at both edges (a rule one bit too narrow misses one of them)
// and an allowed entry cannot be half-refused.
func prefixBounds(t *testing.T, cidr string) (net.IP, net.IP) {
	t.Helper()
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("test setup: %q is not a CIDR: %v", cidr, err)
	}
	first := make(net.IP, len(network.IP))
	copy(first, network.IP)
	last := make(net.IP, len(network.IP))
	for i := range last {
		last[i] = network.IP[i] | ^network.Mask[i]
	}
	return first, last
}
