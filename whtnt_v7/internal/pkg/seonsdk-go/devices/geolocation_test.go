package devices_test

import (
	"math"
	"testing"
	"time"

	"status403.com/rsavv/seonsdk-go/devices"
)

func TestGenerateDeviceLocationLondon(t *testing.T) {
	loc := devices.GenerateDeviceLocation(devices.LocationOptions{Preset: devices.CityLondon})
	if loc == nil {
		t.Fatal("nil location")
	}
	// Jitter around London (51.5074, -0.1278) within ±0.01° by default.
	lat := loc.Latitude.(float64)
	lon := loc.Longitude.(float64)
	if math.Abs(lat-51.5074) > 0.015 {
		t.Errorf("lat outside London jitter window: %v", lat)
	}
	if math.Abs(lon-(-0.1278)) > 0.015 {
		t.Errorf("lon outside London jitter window: %v", lon)
	}
}

func TestGenerateDeviceLocationDefaults(t *testing.T) {
	loc := devices.GenerateDeviceLocation(devices.LocationOptions{Preset: devices.CityLondon})
	if loc.AuthStatus != devices.CLAuthAuthorizedWhenUsed {
		t.Errorf("auth_status: got %v, want %d", loc.AuthStatus, devices.CLAuthAuthorizedWhenUsed)
	}
	if loc.AccuracyAuthLvl != devices.CLAccuracyFull {
		t.Errorf("accuracy_auth_lvl: got %v, want %d", loc.AccuracyAuthLvl, devices.CLAccuracyFull)
	}
	if loc.IsSimulated != false {
		t.Errorf("is_simulated: got %v, want false", loc.IsSimulated)
	}
	if loc.IsCached != true {
		t.Errorf("is_cached: got %v, want true", loc.IsCached)
	}
	if loc.Status != 1 {
		t.Errorf("status: got %v, want 1", loc.Status)
	}
}

func TestGenerateDeviceLocationExplicitCoords(t *testing.T) {
	loc := devices.GenerateDeviceLocation(devices.LocationOptions{
		Latitude:      52.4862, // Birmingham
		Longitude:     -1.8904,
		JitterDegrees: 0.001, // tight
	})
	lat := loc.Latitude.(float64)
	if math.Abs(lat-52.4862) > 0.001 {
		t.Errorf("tight jitter exceeded: %v", lat)
	}
}

func TestGenerateDeviceLocationForceCached(t *testing.T) {
	notCached := false
	loc := devices.GenerateDeviceLocation(devices.LocationOptions{
		Preset:   devices.CityLondon,
		IsCached: &notCached,
	})
	if loc.IsCached != false {
		t.Errorf("IsCached override: got %v, want false", loc.IsCached)
	}
}

func TestGenerateDeviceLocationTimestampRecent(t *testing.T) {
	loc := devices.GenerateDeviceLocation(devices.LocationOptions{Preset: devices.CityLondon})
	ts, ok := loc.Timestamp.(int64)
	if !ok {
		t.Fatalf("timestamp type: got %T, want int64", loc.Timestamp)
	}
	// Default window is 5 s – 5 minutes ago. Verify it's within 10 minutes.
	nowMs := nowTestMs()
	if nowMs-ts > 10*60*1000 || nowMs-ts < 0 {
		t.Errorf("timestamp out of expected window: got %d ms old", nowMs-ts)
	}
}

func nowTestMs() int64 { return time.Now().UnixMilli() }
