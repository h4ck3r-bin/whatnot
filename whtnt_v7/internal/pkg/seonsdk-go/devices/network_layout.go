package devices

import (
	"fmt"
	"math/rand"
	"strings"
)

// Residential subnet distribution. These are the /24s that home-router
// vendors ship as the default LAN subnet (verified against shipping firmware
// for Asus, Netgear, TP-Link, Linksys, D-Link, Xfinity, BT, Sky, Virgin, and
// Apple AirPort). Weighted very roughly by market share — `192.168.1.0/24`
// (TP-Link, D-Link, Xfinity, BT) dominates because it's also the IEEE
// "vanilla" default most consumer gear ships with.
//
// If we pick a /24 that isn't in this set, SEON's WAF or ASN cross-check
// can still clear it, but the pair gets rarer. Keeping the set small +
// common keeps the fingerprint in the trust-cluster centre of mass.
var residentialSubnets = []struct {
	prefix string // "192.168.1" → en0 ends with ".X", def_gw ends with ".1"
	weight int
}{
	{"192.168.1", 40},    // TP-Link / D-Link / Xfinity / BT Home Hub / Sky / most consumer gear
	{"192.168.0", 25},    // Netgear, Belkin, Linksys (varies), many ISPs
	{"192.168.2", 6},     // Belkin (legacy), some Netgear
	{"192.168.100", 4},   // some Telekom / cable routers
	{"192.168.86", 4},    // Google Wifi / Nest Wifi
	{"10.0.0", 4},        // Comcast Xfinity (mixed), Apple AirPort
	{"10.0.1", 3},        // Apple AirPort default
	{"10.0.2", 1},        // Apple AirPort default (multiple bands)
	{"192.168.4", 3},     // Apple AirPort Express
	{"192.168.10", 3},    // Mikrotik, some ISP modems
	{"192.168.50", 2},    // Asus (default after firmware update)
	{"172.20.10", 2},     // iPhone personal hotspot
	{"192.168.8", 1},     // Huawei / ZTE portable hotspots
	{"192.168.43", 1},    // Android personal hotspot
	{"192.168.254", 1},   // some ISP-branded cable modems
}

// pickResidentialSubnet returns the /24 prefix (e.g. "192.168.1") weighted
// by the distribution above. Deterministic output when rand is seeded.
func pickResidentialSubnet() string {
	total := 0
	for _, s := range residentialSubnets {
		total += s.weight
	}
	roll := rand.Intn(total)
	cum := 0
	for _, s := range residentialSubnets {
		cum += s.weight
		if roll < cum {
			return s.prefix
		}
	}
	return residentialSubnets[0].prefix
}

// resolveNwInterfacesAndGateway returns (nw_interfaces, def_gw) that are
// physically consistent: `en0` and `def_gw` share the same /24 subnet, and
// the host-octet for `en0` is NOT `.1` (reserved for the gateway itself)
// nor `.255` (broadcast).
//
// When the caller supplies either value, we honour it as-is. When both are
// nil/empty we synthesise a coherent pair. When only one is set, we derive
// the other from its /24 to keep them consistent.
func resolveNwInterfacesAndGateway(nw map[string]interface{}, gw string) (map[string]interface{}, string) {
	// Both supplied — honour caller.
	if nw != nil && gw != "" {
		return nw, gw
	}

	// Derive the /24 prefix from whichever side the caller provided, or
	// pick a new one from the residential distribution.
	prefix := ""
	if gw != "" {
		if p := hostSlashTwentyFour(gw); p != "" {
			prefix = p
		}
	}
	if prefix == "" && nw != nil {
		if en0, ok := nw["en0"].(string); ok {
			if p := hostSlashTwentyFour(en0); p != "" {
				prefix = p
			}
		}
	}
	if prefix == "" {
		prefix = pickResidentialSubnet()
	}

	// Fill en0 if missing.
	if nw == nil {
		// Host octet in [2, 254] — avoid .0 (network), .1 (gateway), .255 (broadcast).
		host := 2 + rand.Intn(253)
		if host == 1 {
			host = 2
		}
		nw = map[string]interface{}{
			"en0": fmt.Sprintf("%s.%d", prefix, host),
			"lo0": "127.0.0.1",
		}
	} else if _, ok := nw["en0"]; !ok {
		host := 2 + rand.Intn(253)
		nw["en0"] = fmt.Sprintf("%s.%d", prefix, host)
		if _, ok := nw["lo0"]; !ok {
			nw["lo0"] = "127.0.0.1"
		}
	}

	// Fill def_gw if missing.
	if gw == "" {
		gw = prefix + ".1"
	}

	return nw, gw
}

// randomCarrierIP returns a random 10.x.x.x address typical of cellular
// carrier NAT assignments (CGNAT / PDP context).
func randomCarrierIP() string {
	return fmt.Sprintf("10.%d.%d.%d", rand.Intn(256), rand.Intn(256), 1+rand.Intn(254))
}

// NewCellularInterfaces builds the cellular PDP entries for a device: pdp_ip0
// always, plus pdp_ip2 about 40% of the time.
//
// Call this ONCE per device and reuse the result. A carrier-assigned address
// belongs to the PDP context, which survives for the life of the attachment —
// testdata/freshwhatnot reports the same pdp_ip0 (10.211.17.39) and pdp_ip2
// across three fingerprints spanning 116 s. Re-rolling per fingerprint would
// hand SEON a handset whose cellular IP changes every few seconds, and would
// make pdp_ip2 blink in and out of existence.
func NewCellularInterfaces() map[string]interface{} {
	out := map[string]interface{}{"pdp_ip0": randomCarrierIP()}
	if rand.Float64() < 0.4 {
		out["pdp_ip2"] = randomCarrierIP()
	}
	return out
}

// AddCellularInterfaces adds pdp_ip0 (and sometimes pdp_ip2) to an existing
// nw_interfaces map, simulating an active cellular PDP context.
//
// Deprecated: this mutates the map in place and draws fresh random addresses
// on every call. Devices should hold a frozen map from NewCellularInterfaces
// and merge it into a COPY of nw_interfaces per fingerprint.
func AddCellularInterfaces(nw map[string]interface{}) {
	if nw == nil {
		return
	}
	nw["pdp_ip0"] = randomCarrierIP()
	if rand.Float64() < 0.4 {
		nw["pdp_ip2"] = randomCarrierIP()
	}
}

// hostSlashTwentyFour extracts the first three dotted octets of an IPv4
// address. Returns "" when the input doesn't look like an IPv4.
func hostSlashTwentyFour(addr string) string {
	// "192.168.1.169" → "192.168.1"; "192.168.1.1:8080" → "192.168.1".
	if i := strings.IndexByte(addr, ':'); i >= 0 {
		addr = addr[:i]
	}
	parts := strings.Split(addr, ".")
	if len(parts) < 4 {
		return ""
	}
	return strings.Join(parts[:3], ".")
}
