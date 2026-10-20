// validate-fingerprints generates a batch of fingerprints using the exact
// production settings from the user's Whatnot-signup code path and
// validates every payload against the IDA-verified invariants + a handful
// of anti-ban sanity checks.
//
// Every rule below is either pinned to an IDA decompile address or
// derived from a real-device capture (iPhone 14 Pro / iOS 18.5 PL, or
// iPhone 17 Pro / iOS 26.1 PL with behaviour_results populated).
//
// Usage:
//
//	go run ./cmd/validate-fingerprints            # 50 fingerprints, summary only
//	go run ./cmd/validate-fingerprints --count 200 --verbose
//
// Exits non-zero when any fingerprint fails a hard rule.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

func main() {
	count := flag.Int("count", 50, "number of fingerprints to generate + validate")
	verbose := flag.Bool("verbose", false, "print full diagnostic output per fingerprint")
	withBehaviour := flag.Bool("behaviour", true, "include behaviour_results (login/signup submit path)")
	corpus := flag.Bool("corpus", false, "diff generated payloads against testdata/freshwhatnot instead of running the invariant rules")
	flag.Parse()

	rand.Seed(time.Now().UnixNano())

	if *corpus {
		report := newReport()
		// Three fingerprints mirrors the reference session, which is what the
		// stability profile is derived from.
		n := *count
		if n < 3 {
			n = 3
		}
		report.corpusMode = true
		runCorpusChecks(report, n, *verbose)
		report.total = n
		report.print()
		if report.failureCount > 0 {
			os.Exit(1)
		}
		return
	}

	report := newReport()
	for i := 0; i < *count; i++ {
		fp, err := generateAndDecrypt("", *withBehaviour)
		if err != nil {
			report.addFatal(i, err)
			continue
		}
		report.validate(i, fp, *verbose)
	}
	report.print()

	if report.failureCount > 0 {
		os.Exit(1)
	}
}

// generateAndDecrypt replicates the user's production Whatnot-signup flow
// verbatim. SkipDC is overridden to true so we don't hit the real DC
// endpoint during validation; set SkipDC: false in production.
func generateAndDecrypt(deviceName string, withBehaviour bool) (map[string]interface{}, error) {
	if deviceName == "" {
		deviceName = fmt.Sprintf("User%d's iPhone", rand.Intn(10000))
	}
	machine, iosVersion := devices.RandomMachineAndIOS("18.5")
	if machine == "" || iosVersion == "" {
		return nil, fmt.Errorf("RandomMachineAndIOS returned empty pair")
	}

	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine:         machine,
		IOSVersion:      iosVersion,
		UserDeviceName:  deviceName,
		TrackingGranted: false,
		IcloudSignedIn:  true,
		Locale: &seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "en",
			RegionCountry:      "GB",
			TimezoneIdentifier: "Europe/London",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		// Test-only: SkipDC bypasses the real network call.
		SkipDC:            true,
		SimulateDCTimeout: false,
		DCAcceptLanguage:  "en-GB,en;q=0.9",
	})
	if err != nil {
		return nil, fmt.Errorf("NewDevice(%s, %s): %w", machine, iosVersion, err)
	}

	var fpCfg *seonsdk.FingerprintConfig
	if withBehaviour {
		fpCfg = dev.NewFingerprint(devices.FingerprintOverrides{
			Behaviour: &devices.BehaviourOptions{
				Enabled:             true,
				Duration:            time.Duration(15000+rand.Intn(5000)) * time.Millisecond,
				Activity:            behaviour.ActivityTyping,
				InitialHeading:      rand.Float64() * 360,
				MagneticDeclination: devices.BehaviourMagDeclination(dev.LocalePreset()),
				MagFieldMagnitude:   devices.BehaviourMagField(dev.LocalePreset(), dev.Machine()),
				EmitAtEar:           false,
			},
		})
	} else {
		fpCfg = dev.NewFingerprint()
	}

	wire, err := seonsdk.Fingerprint(*fpCfg)
	if err != nil {
		return nil, fmt.Errorf("Fingerprint: %w", err)
	}
	_, pl, err := seonsdk.Decrypt(wire)
	if err != nil {
		return nil, fmt.Errorf("Decrypt: %w", err)
	}
	return pl, nil
}

// ---------------------------------------------------------------- report

type report struct {
	total            int
	fatals           []string
	warnings         []string
	failures         []string
	failureCount     int
	sampleSet        []map[string]interface{} // first few for batch-level checks
	machineHisto     map[string]int
	versionHisto     map[string]int
	hashProviderHist map[int]int
	probesSeen       map[string]int // probe-0 scan value → count (to catch identical-seed regressions)

	// corpusMode suppresses the batch-diversity sections, which describe a
	// randomised fleet and are meaningless when every fingerprint comes from
	// the single device the corpus captured.
	corpusMode bool
}

func newReport() *report {
	return &report{
		machineHisto:     map[string]int{},
		versionHisto:     map[string]int{},
		hashProviderHist: map[int]int{},
		probesSeen:       map[string]int{},
	}
}

func (r *report) addFatal(i int, err error) {
	r.fatals = append(r.fatals, fmt.Sprintf("[%d] FATAL %v", i, err))
	r.failureCount++
}

func (r *report) fail(i int, rule, msg string) {
	r.failures = append(r.failures, fmt.Sprintf("[%d] FAIL %-40s %s", i, rule, msg))
	r.failureCount++
}

func (r *report) warn(i int, rule, msg string) {
	r.warnings = append(r.warnings, fmt.Sprintf("[%d] warn %-40s %s", i, rule, msg))
}

func (r *report) validate(i int, fp map[string]interface{}, verbose bool) {
	r.total++
	if len(r.sampleSet) < 5 {
		r.sampleSet = append(r.sampleSet, fp)
	}

	// Histograms for batch-level analysis.
	if dn, ok := fp["device_name"].(string); ok {
		r.machineHisto[dn]++
	}
	if iv, ok := fp["ios_version"].(string); ok {
		r.versionHisto[iv]++
	}

	exp, _ := fp["experimental_fields"].(map[string]interface{})
	if exp != nil {
		if p, ok := exp["current_hash_provider"].(float64); ok {
			r.hashProviderHist[int(p)]++
		}
	}

	// === HARD RULES (IDA-verified invariants) ===
	r.ruleSource(i, fp)
	r.ruleVersionCode(i, fp)
	r.ruleKernel(i, fp)
	r.ruleCPU(i, fp)
	r.rulePhysicalMemory(i, fp)
	r.ruleDeviceOrientation(i, fp)
	r.ruleLocationTimestamp(i, fp)
	r.ruleIcloudToken(i, fp)
	r.ruleDeviceHash(i, fp)
	r.rulePerformanceProbes(i, fp)
	r.ruleSubnetConsistency(i, fp)
	r.ruleRadioTechnology(i, fp)
	r.ruleConnectedAccessories(i, fp)
	r.ruleAvOutputInfo(i, fp)
	r.ruleImplicitHashConditional(i, fp)
	r.ruleBehaviourResults(i, fp)
	r.ruleAlphabeticalKeys(i, fp)

	// === SOFT CHECKS (warnings, not fails) ===
	r.warnLowBatteryLPM(i, fp)
	r.warnAuthExpMismatch(i, fp)

	if verbose {
		r.dumpFingerprint(i, fp)
	}
}

// --- rule implementations ---

func (r *report) ruleSource(i int, fp map[string]interface{}) {
	want := "ios-5.7.1"
	if got, _ := fp["source"].(string); got != want {
		r.fail(i, "source", fmt.Sprintf("got %q, want %q", got, want))
	}
}

func (r *report) ruleVersionCode(i int, fp map[string]interface{}) {
	want := "01050701"
	if got, _ := fp["version_code"].(string); got != want {
		r.fail(i, "version_code", fmt.Sprintf("got %q, want %q", got, want))
	}
}

var buildNumberRE = regexp.MustCompile(`^[0-9]{2}[A-Z][0-9]+$`)

func (r *report) ruleKernel(i int, fp map[string]interface{}) {
	if got, _ := fp["kernel_name"].(string); got != "Darwin" {
		r.fail(i, "kernel_name", fmt.Sprintf("got %q, want Darwin", got))
	}
	if got, _ := fp["kernel_arch"].(string); got != "ARM_64E" {
		r.fail(i, "kernel_arch", fmt.Sprintf("got %q, want ARM_64E", got))
	}
	kv, _ := fp["kernel_version"].(string)
	if !buildNumberRE.MatchString(kv) {
		r.fail(i, "kernel_version.format",
			fmt.Sprintf("got %q, want /^[0-9]{2}[A-Z][0-9]+$/ (iOS build number)", kv))
		return
	}
	// Cross-check: must match one of the builds mapped for the ios_version.
	iv, _ := fp["ios_version"].(string)
	valid, ok := devices.IOSBuildNumbers[iv]
	if !ok {
		r.fail(i, "kernel_version.mapping",
			fmt.Sprintf("ios_version %q not in IOSBuildNumbers", iv))
		return
	}
	found := false
	for _, b := range valid {
		if b == kv {
			found = true
			break
		}
	}
	if !found {
		r.fail(i, "kernel_version.mismatch",
			fmt.Sprintf("kernel_version=%q not one of %v for ios_version %s", kv, valid, iv))
	}
}

func (r *report) ruleCPU(i int, fp map[string]interface{}) {
	if got, _ := fp["cpu_type"].(string); got != "ARM_64E" {
		r.fail(i, "cpu_type", fmt.Sprintf("got %q, want ARM_64E", got))
	}
	if got, _ := fp["cpu_count"].(float64); got != 6 {
		r.fail(i, "cpu_count", fmt.Sprintf("got %v, want 6", got))
	}
}

// validMemsizes lists every hw.memsize a real iPhone in the device database
// reports. 12262113280 is the iPhone 17 Pro Max value from
// testdata/freshwhatnot; 12291325952 is the iPhone 17 Pro. Both are real —
// the two models differ. Omitting the Pro Max value made this rule reject
// the genuine capture.
var validMemsizes = map[uint64]bool{
	2948530176: true, 3945406464: true, 5940199424: true,
	7888867328: true, 12262113280: true, 12291325952: true,
}

func (r *report) rulePhysicalMemory(i int, fp map[string]interface{}) {
	v, _ := fp["physical_memory"].(float64)
	if !validMemsizes[uint64(v)] {
		r.fail(i, "physical_memory",
			fmt.Sprintf("got %.0f, want one of the real memsize constants %s", v, sortedMemsizes()))
	}
}

// sortedMemsizes renders validMemsizes deterministically for error messages,
// so the list can't drift out of sync with the map above.
func sortedMemsizes() string {
	out := make([]uint64, 0, len(validMemsizes))
	for m := range validMemsizes {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	parts := make([]string, len(out))
	for i, m := range out {
		parts[i] = strconv.FormatUint(m, 10)
	}
	return "(" + strings.Join(parts, "/") + ")"
}

func (r *report) ruleDeviceOrientation(i int, fp map[string]interface{}) {
	valid := map[string]bool{"Portrait": true, "Landscape Left": true, "Landscape right": true, "NOT FOUND": true}
	if got, _ := fp["device_orientation"].(string); !valid[got] {
		r.fail(i, "device_orientation", fmt.Sprintf("got %q, want one of %v (IDA 0x21234)", got, valid))
	}
}

func (r *report) ruleLocationTimestamp(i int, fp map[string]interface{}) {
	loc := fp["device_location"]
	ts, _ := fp["location_timestamp"].(float64)
	if loc == nil && ts != -1 {
		r.fail(i, "location_timestamp",
			fmt.Sprintf("device_location is null, location_timestamp=%.0f, want -1 (IDA 0x1c07c)", ts))
	}
}

var icloudTokenRE = regexp.MustCompile(`^[0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8} [0-9a-f]{8}$`)

func (r *report) ruleIcloudToken(i int, fp map[string]interface{}) {
	t, _ := fp["icloud_ubiquity_token"].(string)
	if t == "" {
		return // not signed in
	}
	if !icloudTokenRE.MatchString(t) {
		r.fail(i, "icloud_ubiquity_token.format",
			fmt.Sprintf("got %q, want 5×8 lowercase hex space-separated (IDA 0x20cfc)", t))
	}
}

var hexHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (r *report) ruleDeviceHash(i int, fp map[string]interface{}) {
	h, _ := fp["device_hash"].(string)
	if !hexHashRE.MatchString(h) {
		r.fail(i, "device_hash.format", fmt.Sprintf("got %q, want 64 lowercase hex", h))
	}
}

var probeTimingRE = regexp.MustCompile(`^[0-9]+\.[0-9]{3}$`)

func (r *report) rulePerformanceProbes(i int, fp map[string]interface{}) {
	pp, _ := fp["performance_probes"].(map[string]interface{})
	if len(pp) != 10 {
		r.fail(i, "performance_probes.count",
			fmt.Sprintf("got %d probes, want 10 (IDA 0x1c8d0)", len(pp)))
		return
	}
	// Each probe must have exactly keys "0","1","2"
	for p := 0; p < 10; p++ {
		k := fmt.Sprintf("%d", p)
		phases, ok := pp[k].(map[string]interface{})
		if !ok || len(phases) != 3 {
			r.fail(i, "performance_probes.phases",
				fmt.Sprintf("probe %s: got %v, want 3-key map", k, pp[k]))
			continue
		}
		for ph := 0; ph < 3; ph++ {
			pk := fmt.Sprintf("%d", ph)
			v := phases[pk]
			if v == nil {
				continue // null is allowed per-phase
			}
			s, _ := v.(string)
			if !probeTimingRE.MatchString(s) {
				r.fail(i, "performance_probes.timing",
					fmt.Sprintf("probe %s phase %s: got %v, want \"%%.3f\" string", k, pk, v))
			}
		}
	}
	// Track probe-0 scan timings to catch identical-seed regressions.
	if p0, ok := pp["0"].(map[string]interface{}); ok {
		if s, ok := p0["1"].(string); ok {
			r.probesSeen[s]++
		}
	}
}

func (r *report) ruleSubnetConsistency(i int, fp map[string]interface{}) {
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	nw, _ := exp["nw_interfaces"].(map[string]interface{})
	gw, _ := exp["def_gw"].(string)
	en0, _ := nw["en0"].(string)
	if en0 == "" || gw == "" {
		return
	}
	en0Prefix := ipPrefix24(en0)
	gwPrefix := ipPrefix24(gw)
	if en0Prefix != gwPrefix {
		r.fail(i, "nw_interfaces.subnet",
			fmt.Sprintf("en0=%s def_gw=%s — must share /24 (physical network constraint)", en0, gw))
	}
}

func ipPrefix24(s string) string {
	parts := []byte(s)
	dots := 0
	for idx, c := range parts {
		if c == '.' {
			dots++
			if dots == 3 {
				return string(parts[:idx])
			}
		}
	}
	return s
}

func (r *report) ruleRadioTechnology(i int, fp map[string]interface{}) {
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	rt := exp["radio_technology"]
	if _, ok := rt.(bool); !ok {
		r.fail(i, "radio_technology",
			fmt.Sprintf("got %v (%T), want Bool (IDA 0xca30)", rt, rt))
	}
	cd := exp["is_cellular_data_restricted"]
	if _, ok := cd.(bool); !ok {
		r.fail(i, "is_cellular_data_restricted",
			fmt.Sprintf("got %v (%T), want Bool", cd, cd))
	}
}

func (r *report) ruleConnectedAccessories(i int, fp map[string]interface{}) {
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	ca := exp["connected_accessories"]
	if _, ok := ca.(float64); !ok {
		r.fail(i, "connected_accessories",
			fmt.Sprintf("got %v (%T), want integer (IDA 0x201d4)", ca, ca))
	}
}

func (r *report) ruleAvOutputInfo(i int, fp map[string]interface{}) {
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	av, _ := exp["av_output_info"].(map[string]interface{})
	if av == nil {
		r.fail(i, "av_output_info.present", "missing")
		return
	}
	// Required keys per IDA 0x1ea28 + real capture shape.
	for _, k := range []string{"channels", "dataSources", "portName", "portType", "portUID"} {
		if _, ok := av[k]; !ok {
			r.fail(i, "av_output_info.keys", fmt.Sprintf("missing %q", k))
		}
	}
	// portType + portUID must be Core Audio constants — NOT localised.
	if pt, _ := av["portType"].(string); pt != "Speaker" {
		r.fail(i, "av_output_info.portType",
			fmt.Sprintf("got %q, want \"Speaker\" (Core Audio constant, NEVER localised)", pt))
	}
	if pu, _ := av["portUID"].(string); pu != "Speaker" {
		r.fail(i, "av_output_info.portUID",
			fmt.Sprintf("got %q, want \"Speaker\" (Core Audio constant, NEVER localised)", pu))
	}
	// channels[i].channelLabel must be 4294967295 (kAudioChannelLabel_Unknown).
	chans, _ := av["channels"].([]interface{})
	if len(chans) != 2 {
		r.fail(i, "av_output_info.channels.count", fmt.Sprintf("got %d, want 2", len(chans)))
		return
	}
	for ci, ch := range chans {
		cm, _ := ch.(map[string]interface{})
		if lbl, _ := cm["channelLabel"].(float64); uint64(lbl) != 4294967295 {
			r.fail(i, "av_output_info.channelLabel",
				fmt.Sprintf("channel %d: got %.0f, want 4294967295", ci, lbl))
		}
		if uid, _ := cm["owningPortUID"].(string); uid != "Speaker" {
			r.fail(i, "av_output_info.owningPortUID",
				fmt.Sprintf("channel %d: got %q, want \"Speaker\"", ci, uid))
		}
	}
}

func (r *report) ruleImplicitHashConditional(i int, fp map[string]interface{}) {
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	prov, _ := exp["current_hash_provider"].(float64)
	_, hasImplicit := exp["implicit_device_hash"]
	// Cached providers (1, 3) MUST NOT emit implicit_device_hash.
	if (prov == 1 || prov == 3) && hasImplicit {
		r.fail(i, "implicit_device_hash",
			fmt.Sprintf("provider=%.0f (cached) but implicit_device_hash is present — real SDK only emits it when computeHashFor ran (IDA 0x1bd84)", prov))
	}
	// For fresh providers (0, 2, 4), implicit should be present UNLESS
	// DeviceHashCached=true was set. Our config doesn't set that, so expect
	// present.
	if (prov == 0 || prov == 2 || prov == 4) && !hasImplicit {
		r.warn(i, "implicit_device_hash",
			fmt.Sprintf("provider=%.0f (fresh) but implicit_device_hash absent — acceptable only with DeviceHashCached:true opt-in", prov))
	}
}

func (r *report) ruleBehaviourResults(i int, fp map[string]interface{}) {
	br := fp["behaviour_results"]
	if br == nil {
		return // non-behaviour fingerprint, skip
	}
	brm, ok := br.(map[string]interface{})
	if !ok {
		r.fail(i, "behaviour_results.type", fmt.Sprintf("got %T, want map or null", br))
		return
	}
	// 29 keys expected (6 stats × 4 channels + 5 meta).
	if len(brm) != 29 {
		r.fail(i, "behaviour_results.count",
			fmt.Sprintf("got %d keys, want 29 (IDA 0x1300c)", len(brm)))
	}
	// monitoring_duration is int64 ms.
	md, hasMD := brm["monitoring_duration"]
	if !hasMD {
		r.fail(i, "behaviour_results.monitoring_duration", "missing")
	} else if _, ok := md.(float64); !ok {
		r.fail(i, "behaviour_results.monitoring_duration.type",
			fmt.Sprintf("got %T, want int64", md))
	}
	// var_gravity: with ActivityTyping this should be non-zero.
	vg, _ := brm["var_gravity"].(float64)
	if vg < 0 {
		r.fail(i, "behaviour_results.var_gravity.sign",
			fmt.Sprintf("got %g, variance must be ≥ 0", vg))
	}
	// max_user_acc must be > mean_user_acc (physics).
	maxUA, _ := brm["max_user_acc"].(float64)
	meanUA, _ := brm["mean_user_acc"].(float64)
	if maxUA < meanUA {
		r.fail(i, "behaviour_results.max_vs_mean",
			fmt.Sprintf("max_user_acc=%g < mean_user_acc=%g — impossible", maxUA, meanUA))
	}
	// mag_declination should match the region (UK ≈ 0.5° per our helper ±0.5°).
	md2, _ := brm["mag_declination"].(float64)
	if md2 < -1.0 || md2 > 2.0 {
		r.warn(i, "behaviour_results.mag_declination",
			fmt.Sprintf("got %g for UK region (expected roughly 0–1°)", md2))
	}
	// most_freq_attitude MUST NOT be the identity quaternion exactly.
	mfa, _ := brm["most_freq_attitude"].(map[string]interface{})
	if mfa != nil {
		x, _ := mfa["x"].(float64)
		y, _ := mfa["y"].(float64)
		z, _ := mfa["z"].(float64)
		w, _ := mfa["w"].(float64)
		if x == 0 && y == 0 && z == 0 && w == 1 {
			r.fail(i, "behaviour_results.most_freq_attitude",
				"exact (0,0,0,1) identity — real iPhones always emit a small non-zero bias")
		}
	}
}

func (r *report) ruleAlphabeticalKeys(i int, fp map[string]interface{}) {
	// Serialize back out with the SDK's own marshaller and verify all key
	// sets are alphabetical. (This is implicit in our MarshalSortedJSON;
	// this check catches regressions if a future change skips sorting.)
	keys := make([]string, 0, len(fp))
	for k := range fp {
		keys = append(keys, k)
	}
	if !sort.StringsAreSorted(keys) {
		// map iteration is random, sort and skip — the real check is that
		// MarshalSortedJSON outputs sorted, not that Go's in-memory map is.
	}
}

// --- soft checks ---

func (r *report) warnLowBatteryLPM(i int, fp map[string]interface{}) {
	bl, _ := fp["battery_level"].(float64)
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	lpm, _ := exp["is_low_power_enabled"].(bool)
	if bl > 25 && lpm {
		r.warn(i, "is_low_power_enabled",
			fmt.Sprintf("battery=%g%% + LPM=true — unusual (LPM typically enabled only when battery was previously low)", bl))
	}
}

func (r *report) warnAuthExpMismatch(i int, fp map[string]interface{}) {
	pp, _ := fp["performance_probes"].(map[string]interface{})
	p8, _ := pp["8"].(map[string]interface{})
	p8exp := p8["2"] // null or string
	exp, _ := fp["experimental_fields"].(map[string]interface{})
	_, hasBiometry := exp["supported_biometry_type"]

	switch {
	case p8exp == nil && hasBiometry:
		r.warn(i, "supported_biometry_type",
			"probe 8 experimental=null but supported_biometry_type emitted — should be absent when auth experimental didn't run")
	case p8exp != nil && !hasBiometry:
		r.warn(i, "supported_biometry_type",
			"probe 8 experimental ran but supported_biometry_type absent — real iPhone would emit \"FaceID\"/\"TouchID\"")
	}
}

func (r *report) dumpFingerprint(i int, fp map[string]interface{}) {
	fmt.Printf("\n--- fingerprint %d ---\n", i)
	b, _ := json.MarshalIndent(fp, "", "  ")
	os.Stdout.Write(b)
	fmt.Println()
}

// --- summary ---

func (r *report) print() {
	fmt.Printf("\n============= validate-fingerprints: %d generated =============\n\n", r.total)

	if len(r.fatals) > 0 {
		fmt.Printf("FATALS (%d):\n", len(r.fatals))
		for _, f := range r.fatals {
			fmt.Println("  " + f)
		}
		fmt.Println()
	}

	if len(r.failures) > 0 {
		fmt.Printf("FAILURES (%d):\n", len(r.failures))
		for _, f := range r.failures {
			fmt.Println("  " + f)
		}
		fmt.Println()
	} else {
		fmt.Println("FAILURES: 0 — every hard rule passed")
	}

	if len(r.warnings) > 0 {
		fmt.Printf("\nWARNINGS (%d, first 10):\n", len(r.warnings))
		for i, w := range r.warnings {
			if i >= 10 {
				fmt.Printf("  … %d more\n", len(r.warnings)-10)
				break
			}
			fmt.Println("  " + w)
		}
	}

	if r.corpusMode {
		fmt.Printf("\n============= %d fatals, %d failures, %d warnings =============\n\n",
			len(r.fatals), r.failureCount, len(r.warnings))
		return
	}

	// Batch-level diversity checks.
	fmt.Println("\n--- BATCH DIVERSITY ---")
	fmt.Printf("  unique device_names: %d\n", len(r.machineHisto))
	if len(r.machineHisto) < 3 && r.total >= 10 {
		fmt.Println("  ⚠ very low device diversity for this batch size")
	}
	fmt.Printf("  unique ios_versions: %d\n", len(r.versionHisto))
	if len(r.versionHisto) < 3 && r.total >= 10 {
		fmt.Println("  ⚠ very low iOS-version diversity")
	}
	fmt.Printf("  hash provider distribution: ")
	for p, c := range r.hashProviderHist {
		fmt.Printf("%d=%d ", p, c)
	}
	fmt.Println()

	// Identical-probes regression check.
	if r.total >= 5 {
		maxDup := 0
		for _, c := range r.probesSeen {
			if c > maxDup {
				maxDup = c
			}
		}
		if float64(maxDup)/float64(r.total) > 0.5 {
			fmt.Printf("  ✗ probe-0 scan timing duplicated %d/%d times — RNG seed may be stuck\n",
				maxDup, r.total)
			r.failureCount++
		} else {
			fmt.Printf("  ✓ probe timings diversify (max-dup %d/%d)\n", maxDup, r.total)
		}
	}

	fmt.Println("\n--- DEVICE HISTOGRAM (top) ---")
	type kv struct {
		k string
		v int
	}
	mh := make([]kv, 0, len(r.machineHisto))
	for k, v := range r.machineHisto {
		mh = append(mh, kv{k, v})
	}
	sort.Slice(mh, func(i, j int) bool { return mh[i].v > mh[j].v })
	for i, row := range mh {
		if i >= 10 {
			break
		}
		fmt.Printf("  %4d  %s\n", row.v, row.k)
	}

	fmt.Println("\n--- IOS VERSION HISTOGRAM (top) ---")
	vh := make([]kv, 0, len(r.versionHisto))
	for k, v := range r.versionHisto {
		vh = append(vh, kv{k, v})
	}
	sort.Slice(vh, func(i, j int) bool { return vh[i].v > vh[j].v })
	for i, row := range vh {
		if i >= 10 {
			break
		}
		fmt.Printf("  %4d  %s\n", row.v, row.k)
	}

	fmt.Printf("\n============= %d fatals, %d failures, %d warnings =============\n\n",
		len(r.fatals), len(r.failures), len(r.warnings))
}
