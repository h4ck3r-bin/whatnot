package devices_test

import (
	"testing"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/devices"
)

func TestLocaleFromPresetUK(t *testing.T) {
	b := devices.LocaleFromPreset(devices.LocaleUKEnglish)
	if b.Locale.RegionCountry != "GB" {
		t.Errorf("country: %q", b.Locale.RegionCountry)
	}
	if b.Locale.TimezoneIdentifier != "Europe/London" {
		t.Errorf("tz: %q", b.Locale.TimezoneIdentifier)
	}
	if b.DCAcceptLanguage != "en-GB,en;q=0.9" {
		t.Errorf("accept-lang: %q", b.DCAcceptLanguage)
	}
}

func TestLocaleFromPresetPoland(t *testing.T) {
	b := devices.LocaleFromPreset(devices.LocalePolishPoland)
	if b.Locale.RegionCountry != "PL" {
		t.Errorf("country: %q", b.Locale.RegionCountry)
	}
	if b.Locale.RegionLanguage != "pl" {
		t.Errorf("lang: %q", b.Locale.RegionLanguage)
	}
	if b.DCAcceptLanguage[:5] != "pl-PL" {
		t.Errorf("accept-lang primary: %q", b.DCAcceptLanguage)
	}
}

func TestValidateLocaleCoherent(t *testing.T) {
	b := devices.LocaleFromPreset(devices.LocaleUKEnglish)
	if err := devices.ValidateLocale(b.Locale, b.DCAcceptLanguage); err != nil {
		t.Errorf("coherent bundle should validate, got: %v", err)
	}
}

func TestValidateLocaleTimezoneCountryMismatch(t *testing.T) {
	bad := seonsdk.LocaleConfig{
		RegionCountry:      "GB",
		TimezoneIdentifier: "Asia/Tokyo",
	}
	if err := devices.ValidateLocale(bad, ""); err == nil {
		t.Error("GB + Asia/Tokyo should fail")
	}
}

func TestValidateLocaleAcceptLanguageCountryMismatch(t *testing.T) {
	bad := seonsdk.LocaleConfig{RegionCountry: "PL", RegionLanguage: "pl"}
	// Accept-Language says UK English — mismatch.
	if err := devices.ValidateLocale(bad, "en-GB,en;q=0.9"); err == nil {
		t.Error("RegionCountry=PL + Accept-Language en-GB should fail")
	}
}

func TestValidateLocaleEmptyFieldsSkip(t *testing.T) {
	// All empty → nothing to validate.
	if err := devices.ValidateLocale(seonsdk.LocaleConfig{}, ""); err != nil {
		t.Errorf("empty should be fine: %v", err)
	}
}

func TestNewDeviceLocalePresetFillsCoherently(t *testing.T) {
	dev, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		LocalePreset: devices.LocaleJapaneseJapan,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := dev.NewFingerprint()
	if cfg.Locale.RegionCountry != "JP" {
		t.Errorf("preset locale not applied: %q", cfg.Locale.RegionCountry)
	}
	if cfg.DCAcceptLanguage[:5] != "ja-JP" {
		t.Errorf("preset Accept-Language not applied: %q", cfg.DCAcceptLanguage)
	}
}

func TestNewDeviceRejectsIncoherentLocale(t *testing.T) {
	_, err := devices.NewDevice(devices.DeviceOptions{
		Machine: "iPhone16,2", IOSVersion: "18.2", SkipDC: true,
		Locale: &seonsdk.LocaleConfig{
			RegionCountry:      "GB",
			TimezoneIdentifier: "Asia/Tokyo",
			RegionLanguage:     "en",
			CalendarIdentifier: "gregorian",
		},
	})
	if err == nil {
		t.Fatal("expected error for GB + Asia/Tokyo")
	}
}
