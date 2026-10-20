package devices

import (
	"math/rand"

	seonsdk "status403.com/rsavv/seonsdk-go"
)

// NetworkState is the high-level connectivity profile of the simulated
// device. Picking one fills in both `TelephonyConfig.NetworkConfig`
// (which becomes payload `network_config`) and
// `ExperimentalConfig.RadioTechnology` (which becomes
// `experimental_fields.radio_technology`) in matching pairs.
//
// The network_config string set is exactly what SEONTelephonyProbe
// networkConfiguration emits at IDA 0xd028 — 14 possible values ranging
// from "WIFI" down through each CTRadioAccessTechnology mapping.
type NetworkState int

const (
	// NetworkWiFi: device is on WiFi as its primary path.
	//   network_config    = "WIFI"
	//   radio_technology  = depends on whether a SIM has service
	//
	// Per IDA 0xca30 `-[SEONTelephonyProbe getExperimentalFields]`,
	// `radio_technology` is ALWAYS `numberWithBool:isCellServiceAvailable`
	// — never a CTRadioAccessTechnology string.
	//
	// isCellServiceAvailable is NOT a function of the primary data path. A
	// SIM-equipped iPhone on WiFi reports true (testdata/freshwhatnot: all
	// three payloads pair `network_config: "WIFI"` with
	// `radio_technology: true`); a SIM-less or airplane-mode device reports
	// false (an older iPhone 14 Pro capture on WiFi + VPN). Use
	// TelephonyForNetworkWithCellService to say which one you mean —
	// TelephonyForNetwork assumes the SIM-less reading.
	NetworkWiFi NetworkState = iota

	// NetworkLTE: typical 4G.
	//   network_config    = "LTE"
	//   radio_technology  = true
	NetworkLTE

	// Network5GNSA: 5G Non-Standalone (most common 5G deployment today —
	// uses LTE core + 5G radio). Typical for iPhone 12+ on most carriers.
	//   network_config    = "5G (NRNSA)"
	//   radio_technology  = true
	Network5GNSA

	// Network5GSA: 5G Standalone (pure 5G core). Rarer; only available on
	// specific carriers and iPhone 12+.
	//   network_config    = "5G (NR)"
	//   radio_technology  = true
	Network5GSA

	// NetworkHSPA: 3G+ (HSPA/HSDPA). Legacy, mostly only in remote areas.
	NetworkHSPA

	// Network3G: bare UMTS/WCDMA 3G.
	Network3G

	// NetworkEDGE: 2.5G. Very rare on a modern iPhone.
	NetworkEDGE

	// NetworkNone: SIM inserted but no signal, or no SIM at all, or airplane.
	//   network_config    = "None"
	//   radio_technology  = false
	NetworkNone
)

// Telephony is the materialised output of TelephonyForNetwork — lets callers
// set it on a FingerprintConfig directly, or inspect before patching.
type Telephony struct {
	NetworkConfig            string // goes to cfg.Telephony.NetworkConfig
	RadioTechnology          bool   // goes to cfg.Experimental.RadioTechnology (emitted as JSON bool)
	IsCellularDataRestricted bool   // goes to cfg.Experimental.IsCellularDataRestricted
	IsOnCall                 bool   // goes to cfg.Telephony.IsOnCall
}

// TelephonyForNetwork returns the (NetworkConfig, RadioTechnology) pair
// the SEON telephony probe emits for a given network state.
//
//   - `network_config` comes from SEONTelephonyProbe.networkConfiguration
//     (IDA 0xd028) — a 14-value switch mapping reachability + CT radio to
//     a human-readable string.
//   - `radio_technology` comes from SEONTelephonyProbe.getExperimentalFields
//     (IDA 0xca30) which is ALWAYS `numberWithBool:isCellServiceAvailable`.
//     Never a CT* string.
// TelephonyForNetworkWithCellService is TelephonyForNetwork with an explicit
// answer for `isCellServiceAvailable`.
//
// `network_config` describes the PRIMARY DATA PATH; `radio_technology`
// describes whether the SIM has service. They are independent on real
// hardware: an iPhone with an active SIM sitting on WiFi reports
// `network_config: "WIFI"` AND `radio_technology: true`. All three payloads in
// testdata/freshwhatnot do exactly that, alongside live cellular interfaces
// (pdp_ip0 / pdp_ip2).
//
// cellService is only consulted for NetworkWiFi, because every other state
// already determines the answer:
//
//   - on LTE / 5G / HSPA / 3G / EDGE, cell service is necessarily available
//   - on NetworkNone (no SIM, no signal, or airplane mode) it necessarily is not
//
// Passing cellService=false with NetworkWiFi models a SIM-less or
// airplane-mode-with-WiFi device, which is what the older iPhone 14 Pro
// capture behind TelephonyForNetwork showed.
func TelephonyForNetworkWithCellService(n NetworkState, cellService bool) Telephony {
	tel := telephonyForNetwork(n)
	if n == NetworkWiFi {
		tel.RadioTechnology = cellService
	}
	return tel
}

// TelephonyForNetwork assumes no cellular service on WiFi. Prefer
// TelephonyForNetworkWithCellService — a SIM-equipped iPhone on WiFi still
// reports radio_technology: true, which is the common real-world case.
func TelephonyForNetwork(n NetworkState) Telephony {
	return telephonyForNetwork(n)
}

func telephonyForNetwork(n NetworkState) Telephony {
	switch n {
	case NetworkWiFi:
		return Telephony{NetworkConfig: "WIFI", RadioTechnology: false}
	case NetworkLTE:
		return Telephony{NetworkConfig: "LTE", RadioTechnology: true}
	case Network5GNSA:
		return Telephony{NetworkConfig: "5G (NRNSA)", RadioTechnology: true}
	case Network5GSA:
		return Telephony{NetworkConfig: "5G (NR)", RadioTechnology: true}
	case NetworkHSPA:
		return Telephony{NetworkConfig: "HSDPA", RadioTechnology: true}
	case Network3G:
		return Telephony{NetworkConfig: "WCDMA", RadioTechnology: true}
	case NetworkEDGE:
		return Telephony{NetworkConfig: "EDGE", RadioTechnology: true}
	case NetworkNone:
		return Telephony{NetworkConfig: "None", RadioTechnology: false}
	default:
		return Telephony{NetworkConfig: "WIFI", RadioTechnology: false}
	}
}

// Apply writes the telephony state onto a FingerprintConfig in-place,
// touching only the payload fields the SDK populates from the telephony
// probe at IDA 0xc728 / 0xc8b8. Existing values on other fields are
// untouched.
func (t Telephony) Apply(cfg *seonsdk.FingerprintConfig) {
	cfg.Telephony.NetworkConfig = t.NetworkConfig
	cfg.Telephony.IsOnCall = t.IsOnCall
	cfg.Experimental.RadioTechnology = t.RadioTechnology
	cfg.Experimental.IsCellularDataRestricted = t.IsCellularDataRestricted
}

// RandomCellularNetwork returns a plausible non-WiFi network state for a
// modern iPhone. Distribution weighted toward LTE + 5G NSA (the current
// mainstream), with small tails for 5G SA and 3G. Used when simulating
// users actively on cellular.
func RandomCellularNetwork() NetworkState {
	r := rand.Float64()
	switch {
	case r < 0.45:
		return NetworkLTE
	case r < 0.85:
		return Network5GNSA
	case r < 0.95:
		return Network5GSA
	case r < 0.98:
		return NetworkHSPA
	default:
		return Network3G
	}
}
