package devices

import (
	"math/rand"
	"time"

	seonsdk "status403.com/rsavv/seonsdk-go"
)

func nowUnixMs() int64 {
	return time.Now().UnixMilli()
}

// Geolocation enums mirror the CoreLocation values the SDK writes into
// `device_location.auth_status` and `device_location.accuracy_auth_lvl`
// (verified against IDA 0x17308 / the `accuracy_auth_lvl` block at 0x16eb0).
//
// auth_status = CLAuthorizationStatus (Apple Doc):
//
//	0 = NotDetermined
//	1 = Restricted
//	2 = Denied
//	3 = AuthorizedAlways
//	4 = AuthorizedWhenInUse
//
// accuracy_auth_lvl = CLAccuracyAuthorization (Apple Doc):
//
//	0 = FullAccuracy
//	1 = ReducedAccuracy
const (
	CLAuthNotDetermined      = 0
	CLAuthRestricted         = 1
	CLAuthDenied             = 2
	CLAuthAuthorizedAlways   = 3
	CLAuthAuthorizedWhenUsed = 4

	CLAccuracyFull    = 0
	CLAccuracyReduced = 1
)

// LocationOptions configures GenerateDeviceLocation. Every field has a
// sensible default — the common case is to set only Latitude + Longitude.
type LocationOptions struct {
	// Latitude and Longitude in degrees (WGS84). If both are 0, the
	// generator picks one of the built-in CityPreset points based on
	// Preset.
	Latitude  float64
	Longitude float64

	// Preset selects a built-in city to center the simulated location
	// around (with small jitter for realism). Ignored if Latitude or
	// Longitude is non-zero. Valid presets: CityLondon, CityManchester,
	// CityEdinburgh, CityNewYork, CityLosAngeles, CityWarsaw, CityTokyo,
	// CityBerlin, CityParis, CityMadrid, CitySydney.
	Preset CityPreset

	// Accuracy in meters (horizontal). Defaults to ~5 m (good GPS lock).
	Accuracy float64

	// AuthStatus defaults to CLAuthAuthorizedWhenUsed (4) — the value a
	// typical app with "While Using" permission granted would emit.
	AuthStatus int

	// AccuracyAuthLvl defaults to CLAccuracyFull (0).
	AccuracyAuthLvl int

	// Status is a SEON-internal integer reported in `device_location.status`.
	// Observed values: 0 = no scan yet, 1 = success, 2 = timeout, etc.
	// Defaults to 1 (success).
	Status int

	// IsSimulated defaults to false. Setting true reports the CoreLocation
	// `locationServicesEnabled && location.simulated` flag, which for
	// non-jailbroken iPhones should always be false.
	IsSimulated bool

	// IsCached: when non-nil, sets the `is_cached` field explicitly. When
	// nil, defaults to true — most SEON scans finish before
	// CLLocationManager has delivered a fresh fix, so the cached value
	// from the previous run is returned.
	IsCached *bool

	// TimestampMs is the unix-ms when the location was last measured. 0 =
	// auto-populate to "between 5 s and 5 minutes ago" to simulate a
	// slightly-stale cached reading.
	TimestampMs int64

	// Jitter radius in degrees. A random point within this radius of
	// (Latitude, Longitude) is picked to avoid all accounts reporting the
	// exact same coordinates. Defaults to 0.01° (≈ 1.1 km).
	JitterDegrees float64
}

// CityPreset is a built-in latitude/longitude anchor.
type CityPreset int

const (
	CityLondon CityPreset = iota
	CityManchester
	CityEdinburgh
	CityNewYork
	CityLosAngeles
	CityWarsaw
	CityTokyo
	CityBerlin
	CityParis
	CityMadrid
	CitySydney
)

var cityCoords = map[CityPreset][2]float64{
	CityLondon:     {51.5074, -0.1278},
	CityManchester: {53.4808, -2.2426},
	CityEdinburgh:  {55.9533, -3.1883},
	CityNewYork:    {40.7128, -74.0060},
	CityLosAngeles: {34.0522, -118.2437},
	CityWarsaw:     {52.2297, 21.0122},
	CityTokyo:      {35.6762, 139.6503},
	CityBerlin:     {52.5200, 13.4050},
	CityParis:      {48.8566, 2.3522},
	CityMadrid:     {40.4168, -3.7038},
	CitySydney:     {-33.8688, 151.2093},
}

// cityElevation is the mean elevation above sea level (meters) per city —
// used to seed the `altitude` field on experimental_device_location so
// different cities produce distinguishable altitudes.
var cityElevation = map[CityPreset]float64{
	CityLondon:     20,
	CityManchester: 38,
	CityEdinburgh:  47,
	CityNewYork:    10,
	CityLosAngeles: 89,
	CityWarsaw:     115,
	CityTokyo:      40,
	CityBerlin:     34,
	CityParis:      35,
	CityMadrid:     657,
	CitySydney:     58,
}

// cityGeoidOffset is the WGS-84 ellipsoid minus orthometric-altitude delta
// for each city. CoreLocation's `ellipsoidalAltitude` = `altitude` + this
// value. Northern-hemisphere values are positive, Sydney is ~20 m.
var cityGeoidOffset = map[CityPreset]float64{
	CityLondon:     46,
	CityManchester: 50,
	CityEdinburgh:  51,
	CityNewYork:    -32,
	CityLosAngeles: -34,
	CityWarsaw:     33,
	CityTokyo:      40,
	CityBerlin:     43,
	CityParis:      45,
	CityMadrid:     52,
	CitySydney:     20,
}

// GenerateDeviceLocation builds a realistic DeviceLocationData value that
// matches the 9-field NSDictionary the SDK emits in `device_location`
// (verified against IDA 0x16d50).
//
// The struct is wired through seonsdk.FingerprintConfig.DeviceLocation →
// payload.builder's buildDeviceLocation → `device_location` payload key.
//
// Typical usage:
//
//	cfg.DeviceLocation = devices.GenerateDeviceLocation(devices.LocationOptions{
//	    Preset: devices.CityLondon,
//	})
func GenerateDeviceLocation(opts LocationOptions) *seonsdk.DeviceLocationData {
	lat, lon := opts.Latitude, opts.Longitude
	if lat == 0 && lon == 0 {
		if c, ok := cityCoords[opts.Preset]; ok {
			lat, lon = c[0], c[1]
		} else {
			lat, lon = cityCoords[CityLondon][0], cityCoords[CityLondon][1]
		}
	}

	jitter := opts.JitterDegrees
	if jitter == 0 {
		jitter = 0.01
	}
	// Uniform disk jitter around (lat, lon).
	lat += (rand.Float64()*2 - 1) * jitter
	lon += (rand.Float64()*2 - 1) * jitter

	accuracy := opts.Accuracy
	if accuracy == 0 {
		// iPhone GPS indoor: 5-15 m. Outdoor: 2-10 m. 5 m is a good default.
		accuracy = 5.0
	}

	authStatus := opts.AuthStatus
	if authStatus == 0 {
		authStatus = CLAuthAuthorizedWhenUsed
	}

	// IsCached defaults to true for realism — SEON scans complete faster
	// than CoreLocation delivers a fresh fix.
	isCached := true
	if opts.IsCached != nil {
		isCached = *opts.IsCached
	}

	ts := opts.TimestampMs
	if ts == 0 {
		// Default to a cached fix 5 s – 5 minutes old.
		ageMs := int64(5_000 + rand.Intn(295_000))
		ts = nowUnixMs() - ageMs
	}

	status := opts.Status
	if status == 0 {
		status = 1 // success
	}

	return &seonsdk.DeviceLocationData{
		Latitude:        lat,
		Longitude:       lon,
		Accuracy:        accuracy,
		AccuracyAuthLvl: opts.AccuracyAuthLvl, // 0 = FullAccuracy default
		Status:          status,
		AuthStatus:      authStatus,
		IsSimulated:     opts.IsSimulated,
		IsCached:        isCached,
		Timestamp:       ts,
	}
}

// GenerateExperimentalDeviceLocation returns the 9-field companion block
// that `-[SEONGeolocationProbe getExperimentalFields]` emits at IDA 0x173d0
// alongside the main `device_location` dict. Fields mirror CLLocation
// properties:
//
//	altitude                 (m, orthometric — "height above sea level")
//	floor_level              (int, -1 = not in a mapped building)
//	vertical_accuracy        (m, positive = valid, -1 = invalid)
//	speed                    (m/s, 0 = stationary, -1 = invalid)
//	speed_accuracy           (m/s, iOS 10+, -1 = invalid)
//	course                   (degrees 0-360, -1 = invalid)
//	course_accuracy          (degrees, iOS 13.4+, -1 = invalid)
//	is_produced_by_accessory (bool, iOS 15+)
//	ellipsoidal_altitude     (m, WGS-84 height — iOS 15+)
//
// This matches the real SDK's output shape byte-for-byte (verified against
// the 9-key NSDictionary construction at 0x17034 via 9 getSafe: blocks).
// Pair with GenerateDeviceLocation to produce the matching top-level
// `device_location` entry.
//
// Default values model a stationary user with GPS lock (the typical
// SEON-scan scenario — user has the phone out but isn't walking or driving).
func GenerateExperimentalDeviceLocation(opts LocationOptions) map[string]interface{} {
	// Base elevation + jitter ±3 m — urban ground altitude varies by
	// building/street that much without meaningful bias.
	var baseElev, geoidOffset float64
	if opts.Latitude != 0 || opts.Longitude != 0 {
		// Explicit coords — we can't know the exact city, so fall back to
		// a generic Northern-hemisphere default.
		baseElev, geoidOffset = 30, 40
	} else if e, ok := cityElevation[opts.Preset]; ok {
		baseElev = e
		geoidOffset = cityGeoidOffset[opts.Preset]
	} else {
		baseElev, geoidOffset = cityElevation[CityLondon], cityGeoidOffset[CityLondon]
	}

	altitude := baseElev + (rand.Float64()*6 - 3)
	ellipsoidal := altitude + geoidOffset

	// Vertical accuracy: iPhone GPS delivers 3-8 m typical.
	vAcc := 3.0 + rand.Float64()*5.0

	return map[string]interface{}{
		"altitude":                 altitude,
		"floor_level":              int64(-1), // default: not in an indoor mapped building
		"vertical_accuracy":        vAcc,
		"speed":                    0.0, // stationary
		"speed_accuracy":           -1.0,
		"course":                   -1.0, // not moving → heading undefined
		"course_accuracy":          -1.0,
		"is_produced_by_accessory": false,
		"ellipsoidal_altitude":     ellipsoidal,
	}
}
