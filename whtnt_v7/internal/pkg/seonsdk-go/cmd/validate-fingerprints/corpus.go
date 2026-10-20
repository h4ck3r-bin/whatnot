package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

// Corpus-differential validation.
//
// The rule-based checks in main.go assert INVARIANTS ("this field is a bool").
// The checks here assert FIDELITY: they diff freshly generated payloads
// against the real iOS payloads in testdata/freshwhatnot and fail on any
// structural divergence. That is the difference between "our output is
// self-consistent" and "our output looks like the device we captured".

const corpusDir = "testdata/freshwhatnot"

var corpusFiles = []string{"fp_register.json", "fp_setphone_1.json", "fp_setphone_2.json"}

// Corpus device identity, from testdata/freshwhatnot/README.md.
const (
	corpusMachine    = "iPhone18,2"
	corpusIOSVersion = "26.5.2"
)

// corpusOnlyVaries lists the fields that legitimately differ between two
// fingerprints from the same device. Everything else must be stable, and
// anything here that we hold constant is equally wrong.
//
// core_seed and metal_info.duration_ms are per-scan measurements that we
// deliberately re-roll even though the corpus session happens to repeat them;
// pasteboard_hash is deliberately frozen. See devices.Device.NewFingerprint.
var (
	corpusExpectedVarying = map[string]bool{
		"behaviour_results":             true,
		"dynamic_evaluator_results":     true,
		"pasteboard_hash":               true,
		"performance_actual_processing": true,
		"performance_exec_time":         true,
		"performance_network_call":      true,
		"performance_probes":            true,
		"request_id":                    true,
		"session_id":                    true,
		"system_uptime":                 true,
		"timestamp":                     true,
	}
	byDesignVaryExtra = map[string]bool{
		"core_seed": true,
		"experimental_fields.metal_info.duration_ms": true,
	}
	byDesignVaryMissing = map[string]bool{
		"pasteboard_hash": true,
	}

	// captureArtifacts are fields whose corpus value describes the RECORDING
	// RIG, not the handset. The HAR was taken through a mitmproxy on the LAN,
	// so the device reported an active system proxy. Copying these would make
	// every generated payload claim a proxy that is not there.
	captureArtifacts = map[string]bool{
		"proxy_address":                  true,
		"proxy_state":                    true,
		"experimental_fields.proxy_type": true,
	}

	// knownGaps are divergences we have chosen not to close. Reported as
	// warnings so they stay visible without failing the gate.
	knownGaps = map[string]string{
		"experimental_fields.nw_interfaces.ipsec6":  "IMS/Wi-Fi-Calling tunnels, not emitted; both mirror pdp_ip2's address in the capture",
		"experimental_fields.nw_interfaces.ipsec7":  "IMS/Wi-Fi-Calling tunnels, not emitted; both mirror pdp_ip2's address in the capture",
		"experimental_fields.nw_interfaces.pdp_ip2": "second PDP context is modelled as present on ~40% of devices, so absence here is expected variance",
	}
)

// findCorpusDir walks up from the working directory looking for the corpus, so
// the tool works from the module root or a subdirectory.
func findCorpusDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for i := 0; i < 6; i++ {
		candidate := filepath.Join(dir, corpusDir)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not locate %s from the working directory", corpusDir)
}

func loadCorpus() ([]map[string]interface{}, [][]byte, error) {
	dir, err := findCorpusDir()
	if err != nil {
		return nil, nil, err
	}
	out := make([]map[string]interface{}, 0, len(corpusFiles))
	raw := make([][]byte, 0, len(corpusFiles))
	for _, name := range corpusFiles {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", name, err)
		}
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", name, err)
		}
		out = append(out, m)
		raw = append(raw, b)
	}
	return out, raw, nil
}

// generateCorpusDevice builds a device matching the capture and emits n
// fingerprints from it, mirroring the whtnt phone-verification path.
func generateCorpusDevice(n int) ([]map[string]interface{}, [][]byte, error) {
	locale := &seonsdk.LocaleConfig{
		RegionTimezone: "+01:00", RegionLanguage: "en", RegionCountry: "GB",
		TimezoneIdentifier: "Europe/London", CalendarIdentifier: "gregorian",
		SpeakerName: "Speaker",
	}
	// The capture is a RETURNING session: all three payloads report
	// current_hash_provider 1 (cached iCloud read) and omit
	// implicit_device_hash entirely. A default device starts fresh —
	// provider 0 on the first call, 1 thereafter, with implicit_device_hash
	// emitted — so comparing against it would flag the harness's own state,
	// not a library defect.
	dev, err := devices.NewRealisticDevice(devices.DeviceOptions{
		Machine: corpusMachine, IOSVersion: corpusIOSVersion,
		UserDeviceName: "Corpus iPhone", Locale: locale,
		SkipDC: true, DCSyntheticSuccess: true, DCDeviceIP: "82.43.170.220",
		DCAcceptLanguage: "en-GB,en;q=0.9",
		DeviceHashCached: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("build corpus device: %w", err)
	}

	out := make([]map[string]interface{}, 0, n)
	rawOut := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		// system_uptime advances in whole seconds with wall-clock time. The
		// real session spaced its three fingerprints 40-70 s apart; generating
		// ours back-to-back inside one second would leave uptime frozen and
		// trip the stability check for the wrong reason.
		if i > 0 {
			time.Sleep(1100 * time.Millisecond)
		}
		bo := &devices.BehaviourOptions{
			Enabled: true, Duration: 6 * time.Second,
			Activity:            behaviour.RandomizeActivity(),
			HeadingActivity:     behaviour.RandomizeHeadingActivity(),
			InitialHeading:      250,
			MagFieldMagnitude:   devices.BehaviourMagField(dev.LocalePreset(), dev.Machine()),
			MagneticDeclination: devices.BehaviourMagDeclination(dev.LocalePreset()),
		}
		cfg := dev.NewFingerprint(devices.FingerprintOverrides{Behaviour: bo})
		cfg.ReturningSession = true

		// FingerprintJSON gives the exact bytes the serializer produces.
		// Decrypt() returns parsed JSON, which throws away the number
		// FORMATTING — the very thing the float check needs to see.
		raw, err := seonsdk.FingerprintJSON(*cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("fingerprint %d: %w", i, err)
		}
		var fp map[string]interface{}
		if err := json.Unmarshal(raw, &fp); err != nil {
			return nil, nil, fmt.Errorf("parse generated %d: %w", i, err)
		}
		out = append(out, fp)
		rawOut = append(rawOut, raw)
	}
	return out, rawOut, nil
}

// flatten renders a payload as dotted paths so nested divergences surface with
// a usable name. Containers whose SHAPE is per-scan data are kept whole.
func flatten(m map[string]interface{}, prefix string) map[string]string {
	opaque := map[string]bool{
		"behaviour_results": true, "performance_probes": true,
		"heading_changes": true, "orientation_changes": true,
	}
	out := map[string]string{}
	for k, v := range m {
		path := prefix + k
		if sub, ok := v.(map[string]interface{}); ok && !opaque[k] {
			for kk, vv := range flatten(sub, path+".") {
				out[kk] = vv
			}
			continue
		}
		b, _ := json.Marshal(v)
		out[path] = string(b)
	}
	return out
}

func jsonType(raw string) string {
	switch {
	case raw == "null":
		return "null"
	case raw == "true" || raw == "false":
		return "bool"
	case strings.HasPrefix(raw, `"`):
		return "string"
	case strings.HasPrefix(raw, "{"):
		return "object"
	case strings.HasPrefix(raw, "["):
		return "array"
	default:
		return "number"
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// runCorpusChecks is the entry point for --corpus.
func runCorpusChecks(r *report, n int, verbose bool) {
	corpus, rawCorpus, err := loadCorpus()
	if err != nil {
		r.addFatal(0, err)
		return
	}
	mine, rawMine, err := generateCorpusDevice(n)
	if err != nil {
		r.addFatal(0, err)
		return
	}

	corpusKeySets(r, corpus, mine)
	corpusFieldTypes(r, corpus, mine)
	corpusFloatFormat(r, rawCorpus, rawMine)
	corpusMetalGrid(r, mine)
	corpusStabilityProfile(r, corpus, mine)

	if verbose {
		fmt.Printf("\ncorpus: %d reference payloads, %d generated\n", len(corpus), len(mine))
	}
}

// corpusKeySets asserts we emit exactly the keys a real device does — no
// missing field, no invented one, at every nesting level.
func corpusKeySets(r *report, corpus, mine []map[string]interface{}) {
	want := flatten(corpus[0], "")
	for i, fp := range mine {
		got := flatten(fp, "")
		for _, k := range sortedKeys(want) {
			if _, ok := got[k]; !ok {
				if why, known := knownGaps[k]; known {
					r.warn(i, "corpus/key-missing", fmt.Sprintf("%s absent (known: %s)", k, why))
					continue
				}
				r.fail(i, "corpus/key-missing", fmt.Sprintf("%s present in the real payload, absent from ours", k))
			}
		}
		for _, k := range sortedKeys(got) {
			if _, ok := want[k]; !ok {
				r.fail(i, "corpus/key-extra", fmt.Sprintf("%s emitted by us, absent from the real payload", k))
			}
		}
	}
}

// corpusFieldTypes catches wrong-TYPE regressions, which are a stronger tell
// than a wrong value — e.g. vpn_protocol emitted as null instead of a string.
func corpusFieldTypes(r *report, corpus, mine []map[string]interface{}) {
	want := flatten(corpus[0], "")
	for i, fp := range mine {
		got := flatten(fp, "")
		for _, k := range sortedKeys(want) {
			gv, ok := got[k]
			if !ok {
				continue // already reported by corpusKeySets
			}
			if captureArtifacts[k] {
				continue // describes the mitmproxy rig, not the handset
			}
			wt, gt := jsonType(want[k]), jsonType(gv)
			if wt == gt {
				continue
			}
			// device_location and wifi_* are legitimately null on a
			// geo-disabled, non-WiFi-detail device in both directions.
			if wt == "null" || gt == "null" {
				if want[k] == "null" && gv == "null" {
					continue
				}
			}
			r.fail(i, "corpus/type", fmt.Sprintf("%s is %s, real payload has %s (%s)", k, gt, wt, want[k]))
		}
	}
}

var corpusFloatLiteral = regexp.MustCompile(`:\s(-?\d+\.\d+(?:[eE][-+]?\d+)?)`)

// corpusFloatFormat checks number FORMATTING in both directions.
//
// Every float literal in the reference bytes must be exactly what %.17g
// produces (that is the format NSJSONSerialization uses), and every float
// literal WE emit must satisfy the same property. The second half is the part
// that matters: it exercises the real serializer, so swapping payload's
// formatFloat back to Go's shortest round-trip fails here rather than passing
// a restated hypothesis.
func corpusFloatFormat(r *report, rawCorpus, rawMine [][]byte) {
	check := func(idx int, raw []byte, label string) int {
		n := 0
		for _, m := range corpusFloatLiteral.FindAllStringSubmatch(string(raw), -1) {
			literal := m[1]
			f, err := strconv.ParseFloat(literal, 64)
			if err != nil {
				continue
			}
			n++
			if want := strconv.FormatFloat(f, 'g', 17, 64); want != literal {
				r.fail(idx, "corpus/float-format",
					fmt.Sprintf("%s literal %s is not %%.17g (want %s) — serializer diverges from NSJSONSerialization",
						label, literal, want))
			}
		}
		return n
	}

	refCount := 0
	for _, raw := range rawCorpus {
		refCount += check(0, raw, "reference")
	}
	if refCount < 100 {
		r.fail(0, "corpus/float-format",
			fmt.Sprintf("only %d reference float literals scanned; corpus looks truncated", refCount))
	}

	mineCount := 0
	for i, raw := range rawMine {
		mineCount += check(i, raw, "generated")
	}
	if mineCount < 20 {
		r.fail(0, "corpus/float-format",
			fmt.Sprintf("only %d generated float literals scanned; expected behaviour_results to supply many more", mineCount))
	}
}

// corpusMetalGrid asserts metal_info.duration_ms sits on the
// CACurrentMediaTime subtraction grid for the payload's own uptime.
func corpusMetalGrid(r *report, mine []map[string]interface{}) {
	for i, fp := range mine {
		exp, _ := fp["experimental_fields"].(map[string]interface{})
		if exp == nil {
			continue
		}
		metal, _ := exp["metal_info"].(map[string]interface{})
		if metal == nil {
			continue
		}
		d, ok := metal["duration_ms"].(float64)
		if !ok {
			r.fail(i, "corpus/metal-grid", "duration_ms is not a number")
			continue
		}
		uptime, ok := fp["system_uptime"].(float64)
		if !ok || uptime <= 0 {
			continue
		}
		ulp := math.Nextafter(uptime, math.Inf(1)) - uptime
		ticks := (d / 1000) / ulp
		if math.Abs(ticks-math.Round(ticks)) > 1e-6 {
			r.fail(i, "corpus/metal-grid",
				fmt.Sprintf("duration_ms %v is off the clock grid at uptime %.0f (ticks=%.4f)", d, uptime, ticks))
		}
	}
}

// corpusStabilityProfile compares which fields move between fingerprints of
// one device against which move in the real session. Both directions matter:
// jitter we invent is a cross-call correlation tell, and stability we invent
// where the device varies is equally wrong.
func corpusStabilityProfile(r *report, corpus, mine []map[string]interface{}) {
	if len(mine) < 2 {
		return
	}
	flatMine := make([]map[string]string, len(mine))
	for i, fp := range mine {
		flatMine[i] = flatten(fp, "")
	}

	varying := map[string]bool{}
	for k := range flatMine[0] {
		for i := 1; i < len(flatMine); i++ {
			if flatMine[i][k] != flatMine[0][k] {
				varying[k] = true
				break
			}
		}
	}

	for _, k := range sortedKeys(flatMine[0]) {
		realVaries := corpusExpectedVarying[k]
		oursVaries := varying[k]
		switch {
		case oursVaries && !realVaries && !byDesignVaryExtra[k]:
			r.fail(0, "corpus/stability",
				fmt.Sprintf("%s changes between our fingerprints but is stable across the real session", k))
		case !oursVaries && realVaries && !byDesignVaryMissing[k]:
			r.fail(0, "corpus/stability",
				fmt.Sprintf("%s is constant in ours but changes across the real session", k))
		}
	}
}
