package fp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	mathrand "math/rand"
	"sort"
	"strings"
)

type JSONDevice struct {
	BUILD         JSONBuild       `json:"BUILD"`
	SCREEN        JSONScreen      `json:"SCREEN"`
	SENSOR        []JSONSensor    `json:"SENSOR"`
	SENSOR_STATUS map[string]bool `json:"SENSOR_STATUS"`
}

type JSONBuild struct {
	MANUFACTURER string           `json:"MANUFACTURER"`
	HARDWARE     string           `json:"HARDWARE"`
	MODEL        string           `json:"MODEL"`
	BOOTLOADER   string           `json:"BOOTLOADER"`
	VERSION      JSONBuildVersion `json:"VERSION"`
	PRODUCT      string           `json:"PRODUCT"`
	TAGS         string           `json:"TAGS"`
	TYPE         string           `json:"TYPE"`
	USER         string           `json:"USER"`
	DISPLAY      string           `json:"DISPLAY"`
	BOARD        string           `json:"BOARD"`
	BRAND        string           `json:"BRAND"`
	DEVICE       string           `json:"DEVICE"`
	FINGERPRINT  string           `json:"FINGERPRINT"`
	HOST         string           `json:"HOST"`
	ID           string           `json:"ID"`
}

type JSONBuildVersion struct {
	RELEASE     string `json:"RELEASE"`
	CODENAME    string `json:"CODENAME"`
	INCREMENTAL string `json:"INCREMENTAL"`
	SDK_INT     int    `json:"SDK_INT"`
}

type JSONScreen struct {
	HeightPixels  int     `json:"heightPixels"`
	WidthPixels   int     `json:"widthPixels"`
	DensityDPI    int     `json:"densityDpi"`
	ScaledDensity float64 `json:"scaledDensity"`
}

type JSONSensor struct {
	Type int     `json:"type"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Z    float64 `json:"z"`
}

type DeviceRecord struct {
	Manufacturer       string
	Hardware           string
	Model              string
	Bootloader         string
	VersionRelease     string
	VersionSDK         int
	VersionIncremental string
	Product            string
	Tags               string
	BuildType          string
	Display            string
	Board              string
	Brand              string
	Device             string
	BuildFingerprint   string
	BuildID            string

	ScreenHeight int
	ScreenWidth  int
	ScreenScale  float64 // densityDpi / 160.0

	// Sensor readings grouped by type
	Accel []JSONSensor // type 1
	Mag   []JSONSensor // type 2
	Gyro  []JSONSensor // type 4

	SensorBitmask int
}

type TimezoneInfo struct {
	Identifier string
	Offset     string
	Country    string
}

type CarrierInfo struct {
	Name          string
	Country       string
	CountryCode   string
	OperatorCode  string
	CarrierIDName string
	CarrierID     string
}

var tzList = []TimezoneInfo{
	{"America/New_York", "-05:00", "US"},
	{"America/Los_Angeles", "-08:00", "US"},
	{"America/Chicago", "-06:00", "US"},
	{"America/Denver", "-07:00", "US"},
	{"America/Phoenix", "-07:00", "US"},
	{"America/Detroit", "-05:00", "US"},
	{"Europe/London", "+00:00", "GB"},
	{"Europe/Paris", "+01:00", "FR"},
	{"Europe/Berlin", "+01:00", "DE"},
	{"Europe/Warsaw", "+01:00", "PL"},
	{"Europe/Amsterdam", "+01:00", "NL"},
	{"Europe/Madrid", "+01:00", "ES"},
	{"Asia/Tokyo", "+09:00", "JP"},
	{"Asia/Seoul", "+09:00", "KR"},
	{"Australia/Sydney", "+11:00", "AU"},
}

var carriersByCountry = map[string][]CarrierInfo{
	"US": {
		{"T-Mobile", "US", "us", "310260", "T-Mobile - US", "1"},
		{"AT&T", "US", "us", "310410", "AT&T - US", "6"},
		{"Verizon", "US", "us", "311480", "Verizon - US", "7"},
		{"Sprint", "US", "us", "310120", "Sprint - US", "4"},
	},
	"GB": {
		{"Vodafone UK", "GB", "gb", "23415", "Vodafone UK", "280"},
		{"EE", "GB", "gb", "23430", "EE", "281"},
		{"O2", "GB", "gb", "23410", "O2 UK", "282"},
	},
	"FR": {
		{"Orange", "FR", "fr", "20801", "Orange France", "340"},
		{"SFR", "FR", "fr", "20810", "SFR France", "341"},
	},
	"DE": {
		{"Telekom", "DE", "de", "26201", "Telekom.de", "360"},
		{"Vodafone DE", "DE", "de", "26202", "Vodafone Germany", "361"},
	},
	"PL": {
		{"T-Mobile PL", "PL", "pl", "26002", "T-Mobile Poland", "321"},
		{"Orange PL", "PL", "pl", "26003", "Orange Poland", "322"},
		{"Play", "PL", "pl", "26006", "Play PL", "323"},
	},
	"NL": {
		{"KPN", "NL", "nl", "20408", "KPN NL", "400"},
	},
	"ES": {
		{"Movistar", "ES", "es", "21407", "Movistar Spain", "410"},
	},
	"JP": {
		{"NTT DOCOMO", "JP", "jp", "44010", "NTT DOCOMO", "440"},
		{"au", "JP", "jp", "44050", "au (KDDI)", "441"},
	},
	"KR": {
		{"SK Telecom", "KR", "kr", "45005", "SK Telecom KR", "450"},
	},
	"AU": {
		{"Telstra", "AU", "au", "50501", "Telstra AU", "500"},
	},
}

var tacByManufacturer = map[string][]string{
	"samsung":  {"35328510", "35329310", "35435309", "35893510", "35824005", "35317808", "35326708", "86871003"},
	"google":   {"35303308", "35444409", "35671810", "35786612", "35917412"},
	"motorola": {"35366507", "35255104", "35316809", "35348310"},
	"oneplus":  {"86861903", "86862803", "86862904", "35924110"},
	"lge":      {"35279905", "35325004", "35279906", "35420906"},
	"asus":     {"35396708", "35396807", "35397008", "35498408"},
	"htc":      {"35341507", "35266907", "35265708"},
	"huawei":   {"86704503", "86826803", "35898009", "86828501"},
	"xiaomi":   {"86765203", "86804603", "86803603"},
}

var inputDeviceTemplates = map[string]string{
	"samsung": "{gpio-keys=%d, sec_touchscreen=%d, sec_touchkey=0, sec_e-compass=0, proximity_sensor=0}",
	"google":  "{gpio-keys=%d, uinput-fpc=0, Synaptics_DSX=%d, msm8998-tavil-snd-card Button Jack=0}",
	"default": "{gpio-keys=%d, synaptics_dsx=%d, accelerometer=0, gyroscope=0, proximity=0}",
}

// ─── Utility helpers ─────────────────────────────────────────────────────────

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randHex32() string { return randHex(32) }

func randUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

func randBase64(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func randInt(min, max int) int { return min + mathrand.Intn(max-min+1) }

func randChoice[T any](s []T) T { return s[mathrand.Intn(len(s))] }

func isTabletModel(model string) bool {
	m := strings.ToUpper(model)
	return strings.Contains(m, "SM-T") || strings.Contains(m, "TAB") ||
		strings.Contains(m, "PAD") || strings.Contains(m, "TABLET")
}

func kernelArchForBoard(board string) string {
	b := strings.ToLower(board)
	for _, prefix := range []string{"msm8960", "sc7730", "mt6735", "mt6753", "exynos5"} {
		if strings.HasPrefix(b, prefix) {
			return "armv7l"
		}
	}
	return "aarch64"
}

func kernelVersionForSDK(sdk int) string {
	m := map[int]string{
		19: "3.4.67-android-4.4", 21: "3.10.49-g4b2def9",
		22: "3.10.108-android-5.1", 23: "3.18.20-android-6.0",
		24: "3.10.84-android-7.0", 25: "4.4.21-android-7.1",
		26: "4.4.88-android-8.0", 27: "4.4.153-android-8.1",
		28: "4.9.84-android-9.0", 29: "4.14.171-android-10.0",
		30: "4.14.180-android-11.0", 31: "5.4.86-android-11.0",
		33: "5.15.41-android13",
	}
	if v, ok := m[sdk]; ok {
		return v
	}
	return fmt.Sprintf("4.9.%d-android", randInt(150, 220))
}

func cpuTypeForBoard(board string) string {
	b := strings.ToUpper(board)
	for prefix, cpu := range map[string]string{
		"MSM8960":       "ARMv7 Processor rev 4 (v7l)",
		"MSM8974":       "ARMv7 Processor rev 3 (v7l)",
		"MSM8996":       "AArch64 Processor rev 4 (aarch64)",
		"MSM8998":       "AArch64 Processor rev 4 (aarch64)",
		"SDM845":        "AArch64 Processor rev 12 (aarch64)",
		"SDM660":        "AArch64 Processor rev 14 (aarch64)",
		"SM8150":        "AArch64 Processor rev 1 (aarch64)",
		"SM8250":        "AArch64 Processor rev 1 (aarch64)",
		"SC7730":        "ARMv7 Processor rev 4 (v7l)",
		"UNIVERSAL8895": "AArch64 Processor rev 1 (aarch64)",
		"UNIVERSAL7885": "AArch64 Processor rev 4 (aarch64)",
		"UNIVERSAL9810": "AArch64 Processor rev 1 (aarch64)",
		"KIRIN":         "AArch64 Processor rev 4 (aarch64)",
		"MT6755":        "AArch64 Processor rev 4 (aarch64)",
	} {
		if strings.HasPrefix(b, prefix) {
			return cpu
		}
	}
	return "AArch64 Processor rev 4 (aarch64)"
}

func cpuCountForBoard(board string) int {
	b := strings.ToUpper(board)
	for _, p := range []string{"MSM8996", "MSM8998", "SDM845", "SDM660", "SM8150", "SM8250", "UNIVERSAL8895", "UNIVERSAL9810", "KIRIN"} {
		if strings.HasPrefix(b, p) {
			return 8
		}
	}
	return randChoice([]int{4, 8})
}

func tacForManufacturer(mfr string) string {
	tacs, ok := tacByManufacturer[strings.ToLower(mfr)]
	if !ok {
		tacs = []string{"35328510", "35329310", "86860001"}
	}
	return randChoice(tacs)
}

func smsPackageForManufacturer(mfr string) string {
	if strings.ToLower(mfr) == "samsung" {
		return "com.samsung.android.messaging"
	}
	if strings.ToLower(mfr) == "google" {
		return "com.google.android.apps.messaging"
	}
	return randChoice([]string{"com.google.android.apps.messaging", "com.android.mms"})
}

func cameraAppsForManufacturer(mfr string) []string {
	switch strings.ToLower(mfr) {
	case "samsung":
		return []string{"com.samsung.android.camera", "com.sec.android.app.camera"}
	case "google":
		return []string{"com.google.android.GoogleCamera", "com.android.camera.VideoCamera", "com.android.camera.CaptureActivity"}
	default:
		return []string{"com.android.camera.VideoCamera", "com.android.camera.CaptureActivity"}
	}
}

func inputDeviceStr(mfr string) string {
	tmpl, ok := inputDeviceTemplates[strings.ToLower(mfr)]
	if !ok {
		tmpl = inputDeviceTemplates["default"]
	}
	return fmt.Sprintf(tmpl, randInt(1, 8), randInt(800, 3000))
}

func sdkToOSVersion(sdk int) int {
	m := map[int]int{
		19: 44200, 21: 50100, 22: 51100, 23: 60000, 24: 70000,
		25: 71100, 26: 80000, 27: 81000, 28: 90000, 29: 100000,
		30: 110000, 31: 120000, 32: 120100, 33: 130000, 34: 140000,
	}
	if v, ok := m[sdk]; ok {
		return v
	}
	return sdk * 10000
}

func sdkToPatchLevel(sdk int) int {
	base := map[int]int{
		19: 201410, 21: 201502, 22: 201509, 23: 201601, 24: 201610,
		25: 201701, 26: 201803, 27: 201901, 28: 201904, 29: 201911,
		30: 202103, 31: 202201, 32: 202208, 33: 202403, 34: 202410,
	}
	v, ok := base[sdk]
	if !ok {
		v = 202403
	}
	months := mathrand.Intn(4)
	yr, mo := v/100, v%100+months
	for mo > 12 {
		mo -= 12
		yr++
	}
	return yr*100 + mo
}

func randomPrivateIP() (string, string) {
	subnets := []string{"192.168.1", "192.168.0", "192.168.2", "10.0.0", "10.0.1"}
	sub := randChoice(subnets)
	host := randInt(2, 253)
	return fmt.Sprintf("%s.%d", sub, host), fmt.Sprintf("%s.1", sub)
}

func sensorMagnitudes(readings []JSONSensor) []float64 {
	mags := make([]float64, len(readings))
	for i, r := range readings {
		mags[i] = math.Sqrt(r.X*r.X + r.Y*r.Y + r.Z*r.Z)
	}
	return mags
}

func consecutiveDiffs(readings []JSONSensor, n int) []float64 {
	var diffs []float64
	for i := 1; i < len(readings); i++ {
		dx := math.Abs(readings[i].X - readings[i-1].X)
		dy := math.Abs(readings[i].Y - readings[i-1].Y)
		dz := math.Abs(readings[i].Z - readings[i-1].Z)
		diffs = append(diffs, math.Sqrt(dx*dx+dy*dy+dz*dz))
	}
	// Take a window of n diffs in time order (last n if more than n).
	// Real devices return diffs in acquisition order, NOT sorted by magnitude.
	if len(diffs) > n {
		diffs = diffs[len(diffs)-n:]
	}
	// Pad with realistic noise-floor values if not enough readings.
	for len(diffs) < n {
		base := mathrand.Float64() * 0.008
		if mathrand.Float64() < 0.15 {
			base += 0.05 + mathrand.Float64()*0.4
		}
		diffs = append(diffs, base)
	}
	return diffs
}

func statsFromMagnitudes(mags []float64) (min, max, med, p95 float64) {
	if len(mags) == 0 {
		return 0, 0, 0, 0
	}
	sorted := make([]float64, len(mags))
	copy(sorted, mags)
	sort.Float64s(sorted)
	min = sorted[0]
	max = sorted[len(sorted)-1]
	med = sorted[len(sorted)/2]
	p95idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if p95idx < 0 {
		p95idx = 0
	}
	p95 = sorted[p95idx]
	return
}

// jitterSensorReadings returns a copy of readings with small random per-axis
// noise applied, so repeated calls on the same device profile produce different
// consecutive diffs. scale controls amplitude (e.g. 0.012 for gyro rad/s,
// 0.035 for accel m/s²).
func jitterSensorReadings(readings []JSONSensor, scale float64) []JSONSensor {
	out := make([]JSONSensor, len(readings))
	for i, r := range readings {
		out[i] = JSONSensor{
			Type: r.Type,
			X:    r.X + (mathrand.Float64()*2-1)*scale,
			Y:    r.Y + (mathrand.Float64()*2-1)*scale,
			Z:    r.Z + (mathrand.Float64()*2-1)*scale,
		}
	}
	return out
}

// shufflePermissions swaps ~4 random adjacent pairs in the permission list so
// the order varies slightly between fingerprints without changing which
// permissions are present.
func shufflePermissions(perms []string) []string {
	out := make([]string, len(perms))
	copy(out, perms)
	swaps := 4
	for i := 0; i < swaps; i++ {
		idx := mathrand.Intn(len(out) - 1)
		out[idx], out[idx+1] = out[idx+1], out[idx]
	}
	return out
}

// buildNwIfList returns a realistic network interface list for the given
// manufacturer. Real devices vary considerably — Samsung/Google often have eth0,
// some have dummy0, rmnet index varies.
func buildNwIfList(mfr string) []string {
	ifaces := []string{"lo:1", "wlan0:1"}
	rmnetIdx := randInt(0, 3)
	switch strings.ToLower(mfr) {
	case "samsung":
		ifaces = append(ifaces, fmt.Sprintf("rmnet_data%d:%d", rmnetIdx, randChoice([]int{0, 1})))
		if mathrand.Float64() < 0.3 {
			ifaces = append(ifaces, "dummy0:1")
		}
	case "google":
		ifaces = append(ifaces, fmt.Sprintf("rmnet_data%d:%d", rmnetIdx, randChoice([]int{0, 1})))
		if mathrand.Float64() < 0.4 {
			ifaces = append(ifaces, "eth0:1")
		}
	case "motorola", "oneplus":
		ifaces = append(ifaces, fmt.Sprintf("rmnet_ipa%d:%d", rmnetIdx, randChoice([]int{0, 1})))
	default:
		ifaces = append(ifaces, fmt.Sprintf("rmnet_data%d:%d", rmnetIdx, randChoice([]int{0, 1})))
	}
	return ifaces
}

// attestParamsForSDK returns attestation fields appropriate for the given
// Android SDK level. Older devices use SOFTWARE keymaster; newer use
// TRUSTED_ENVIRONMENT (StrongBox capable).
func attestParamsForSDK(sdk int) (kmVersion, version int, kmLevel, secLevel, bootState, isUnlocked string) {
	if sdk <= 28 {
		return 100, 100, "SOFTWARE", "SOFTWARE", "UNVERIFIED", "UNLOCKED_SW"
	} else if sdk <= 31 {
		return 200, 200, "TRUSTED_ENVIRONMENT", "TRUSTED_ENVIRONMENT", "VERIFIED", "LOCKED"
	}
	return 400, 400, "TRUSTED_ENVIRONMENT", "TRUSTED_ENVIRONMENT", "VERIFIED", "LOCKED"
}

// buildLvlInfM returns the LvlInf.M slice. On devices with SDK >= 33, there is
// a ~40% chance of having 1–2 prior API level entries (reflecting previously
// installed OS versions).
func buildLvlInfM(sdk int) []any {
	if sdk >= 33 && mathrand.Float64() < 0.40 {
		candidates := []int{}
		for _, v := range []int{30, 31, 32} {
			if v < sdk {
				candidates = append(candidates, v)
			}
		}
		count := randInt(1, 2)
		mathrand.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
		if count > len(candidates) {
			count = len(candidates)
		}
		out := make([]any, count)
		for i := 0; i < count; i++ {
			out[i] = candidates[i]
		}
		return out
	}
	return []any{}
}

// roamingState returns a realistic DataRoamingState for a given country.
// Non-domestic markets (non-US/GB) have a 30% chance of being ENABLED.
func roamingState(country string) string {
	switch strings.ToUpper(country) {
	case "US", "GB", "AU":
		return "DISABLED"
	default:
		if mathrand.Float64() < 0.30 {
			return "ENABLED"
		}
		return "DISABLED"
	}
}

// langForCountry maps a two-letter country code to a BCP-47 language tag.
func langForCountry(country string) string {
	m := map[string]string{
		"FR": "fr", "DE": "de", "PL": "pl",
		"NL": "nl", "ES": "es", "JP": "ja",
		"KR": "ko",
	}
	if lang, ok := m[strings.ToUpper(country)]; ok {
		return lang
	}
	return "en"
}
