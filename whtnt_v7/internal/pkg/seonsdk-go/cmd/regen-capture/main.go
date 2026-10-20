package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
)

// Regenerate a fingerprint that mirrors the real iPhone 14 Pro capture
// exactly (to the extent our config knobs allow), then byte-diff every
// field.
func main() {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine:             "iPhone15,2",
		IOSVersion:          "18.5",
		UserDeviceName:      "iPhone",
		IcloudSignedIn:      true,
		TrackingGranted:     true,
		LocalePreset:        devices.LocalePolishPoland,
		SkipDC:              true,
		InitialBatteryLevel: 100, // match capture
		InitialUptimeSec:    21812,
		PluggedIn:           false,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg := dev.NewFingerprint()

	// Force the state the captured device was in: returning session →
	// current_hash_provider=1 (iCloud cached), no implicit_device_hash.
	cfg.ReturningSession = true
	// Pin the exact values captured in the real fingerprint so the diff
	// focuses on STRUCTURAL mismatches, not randomised drift.
	cfg.SessionID = "CA1D7B98-73A4-4441-93F4-E8EDB88617CC"
	cfg.Device.DeviceAdid = "A7AC6244-B795-4F52-886D-F3F88BF77A63"
	cfg.Device.DeviceUdid = "E705F82C-97CF-411F-89B4-1A66108DFBBD"
	cfg.Device.IcloudUbiquityToken = "94051589 5b5e1c27 79c2ca45 74bae595 b8fff830"
	cfg.Device.PasteboardHash = "755ae1d28212c4fcb956a8ecadf46a5ba5bda8bfd4a7118c8487b4e8da5035be"
	cfg.Device.ScreenBrightness = 40
	cfg.Hardware.BatteryLevel = 100
	cfg.Hardware.BatteryCharging = false
	cfg.Hardware.SystemUptime = 21812
	cfg.Media.AudioVolumeCurrent = 20
	cfg.Locale.RegionTimezone = "+02:00" // capture was BST
	cfg.Experimental.UserInterfaceStyle = "Dark"
	cfg.Experimental.DateFormat = "d MMM y"

	// Real capture had an active proxy + VPN. Simulate matching state.
	cfg.Network.VpnState = "CONNECTED"
	cfg.Network.ProxyAddress = "198.18.72.253:8080"
	cfg.Network.ProxyState = "CONNECTED"
	cfg.Experimental.VpnProtocol = "utun5"
	cfg.Experimental.ProxyType = "kCFProxyTypeHTTPS"
	cfg.Experimental.NwInterfaces = map[string]interface{}{
		"en0":   "192.168.0.66",
		"lo0":   "127.0.0.1",
		"utun5": "198.18.72.254",
	}
	cfg.Experimental.DefGw = "192.168.0.1"

	wire, err := seonsdk.Fingerprint(*cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, ours, err := seonsdk.Decrypt(wire)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	realBytes, _ := os.ReadFile("testdata/real_iphone14pro_18.5_PL.json")
	var real map[string]interface{}
	json.Unmarshal(realBytes, &real)

	diffFields("TOP-LEVEL", real, ours)

	rExp, _ := real["experimental_fields"].(map[string]interface{})
	oExp, _ := ours["experimental_fields"].(map[string]interface{})
	if rExp != nil && oExp != nil {
		diffFields("EXPERIMENTAL", rExp, oExp)
	}

	rAvOut, _ := rExp["av_output_info"].(map[string]interface{})
	oAvOut, _ := oExp["av_output_info"].(map[string]interface{})
	if rAvOut != nil && oAvOut != nil {
		diffFields("AV_OUTPUT_INFO", rAvOut, oAvOut)
		// Dump our av_output_info so the Polish "Głośnik" names are visible
		b, _ := json.MarshalIndent(oAvOut, "  ", "  ")
		fmt.Printf("\n  our av_output_info:\n  %s\n", string(b))
	}

	rNative, _ := rExp["native_bounds"].(map[string]interface{})
	oNative, _ := oExp["native_bounds"].(map[string]interface{})
	if rNative != nil && oNative != nil {
		diffFields("NATIVE_BOUNDS", rNative, oNative)
	}

	rGeo, _ := real["geolocation_config"].(map[string]interface{})
	oGeo, _ := ours["geolocation_config"].(map[string]interface{})
	if rGeo != nil && oGeo != nil {
		diffFields("GEOLOCATION_CONFIG", rGeo, oGeo)
	}
}

func diffFields(label string, real, ours map[string]interface{}) {
	rKeys := keys(real)
	oKeys := keys(ours)
	sort.Strings(rKeys)
	sort.Strings(oKeys)

	allKeys := map[string]bool{}
	for _, k := range rKeys {
		allKeys[k] = true
	}
	for _, k := range oKeys {
		allKeys[k] = true
	}
	ks := []string{}
	for k := range allKeys {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	fmt.Printf("\n=== %s (real=%d ours=%d) ===\n", label, len(real), len(ours))
	hits, misses := 0, 0
	for _, k := range ks {
		r, rOK := real[k]
		o, oOK := ours[k]
		rs := short(r)
		os := short(o)
		marker := "✓"
		switch {
		case !rOK:
			marker = "+"
			misses++
		case !oOK:
			marker = "-"
			misses++
		case !valueEqual(r, o):
			marker = "≠"
			misses++
		default:
			hits++
		}
		if marker == "✓" {
			continue
		}
		fmt.Printf("  %s %-32s real=%-40s ours=%s\n", marker, k, rs, os)
	}
	fmt.Printf("  ---- %d match, %d differ\n", hits, misses)
}

func keys(m map[string]interface{}) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func short(v interface{}) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return strings.ReplaceAll(s, "\n", "\\n")
}

func valueEqual(a, b interface{}) bool {
	// Loose equality — JSON-unmarshalled scalars always comparable; maps/
	// slices compared by Sprintf.
	if fmt.Sprintf("%T", a) != fmt.Sprintf("%T", b) {
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
