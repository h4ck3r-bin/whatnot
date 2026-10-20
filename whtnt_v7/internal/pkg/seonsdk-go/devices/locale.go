package devices

import (
	"fmt"
	"strings"

	seonsdk "status403.com/rsavv/seonsdk-go"
)

// LocalePreset is a curated bundle of (language, country, timezone, calendar,
// declination) values that together describe a coherent iPhone user from a
// specific region. Passing a preset to LocaleFromPreset / ApplyLocalePreset
// fills every related field on FingerprintConfig so timezone, country, and
// Accept-Language can't drift out of sync.
type LocalePreset int

const (
	LocaleUKEnglish LocalePreset = iota
	LocaleUSEnglish
	LocaleIrishEnglish
	LocaleGermanGermany
	LocaleFrenchFrance
	LocaleSpanishSpain
	LocaleItalianItaly
	LocalePolishPoland
	LocaleDutchNetherlands
	LocaleJapaneseJapan
	LocaleKoreanKorea
	LocaleAustraliaEnglish
	LocaleCanadaEnglish
)

// LocaleBundle is the resolved output of a preset: the four LocaleConfig
// fields plus the matching DC `Accept-Language` header and magnetic
// declination. All string values come from iOS's own CFLocale tables
// (regionTimezone / calendar_identifier / etc.) so they match what a real
// user-owned iPhone would emit for that region.
type LocaleBundle struct {
	Locale              seonsdk.LocaleConfig
	DCAcceptLanguage    string
	MagneticDeclination float64 // degrees east, WMM-2025 regional mean

	// MagFieldIntensityUT is the total Earth magnetic field intensity in µT
	// at the region's major city, outdoors, from WMM-2025. Indoor readings
	// attenuate by 5-12 µT; use BehaviourMagField to apply that adjustment
	// plus a per-device bias.
	MagFieldIntensityUT float64
}

var localeBundles = map[LocalePreset]LocaleBundle{
	LocaleUKEnglish: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+00:00",
			RegionLanguage:     "en",
			RegionCountry:      "GB",
			TimezoneIdentifier: "Europe/London",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		DCAcceptLanguage:    "en-GB,en;q=0.9",
		MagneticDeclination: 0.5,
		MagFieldIntensityUT: 49.5, // London WMM-2025
	},
	LocaleUSEnglish: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "-05:00", // EST; adjust TimezoneIdentifier if you target PST/MST/CST
			RegionLanguage:     "en",
			RegionCountry:      "US",
			TimezoneIdentifier: "America/New_York",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		DCAcceptLanguage:    "en-US,en;q=0.9",
		MagneticDeclination: -13.0,
		MagFieldIntensityUT: 52.0, // NYC WMM-2025
	},
	LocaleIrishEnglish: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+00:00",
			RegionLanguage:     "en",
			RegionCountry:      "IE",
			TimezoneIdentifier: "Europe/Dublin",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		DCAcceptLanguage:    "en-IE,en;q=0.9",
		MagneticDeclination: -2.5,
		MagFieldIntensityUT: 49.0, // Dublin WMM-2025
	},
	LocaleGermanGermany: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "de",
			RegionCountry:      "DE",
			TimezoneIdentifier: "Europe/Berlin",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Lautsprecher",
		},
		DCAcceptLanguage:    "de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: 3.5,
		MagFieldIntensityUT: 49.0, // Berlin WMM-2025
	},
	LocaleFrenchFrance: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "fr",
			RegionCountry:      "FR",
			TimezoneIdentifier: "Europe/Paris",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Haut-parleur",
		},
		DCAcceptLanguage:    "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: 1.0,
		MagFieldIntensityUT: 47.0, // Paris WMM-2025
	},
	LocaleSpanishSpain: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "es",
			RegionCountry:      "ES",
			TimezoneIdentifier: "Europe/Madrid",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Altavoz",
		},
		DCAcceptLanguage:    "es-ES,es;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: 0.5,
		MagFieldIntensityUT: 45.0, // Madrid WMM-2025
	},
	LocaleItalianItaly: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "it",
			RegionCountry:      "IT",
			TimezoneIdentifier: "Europe/Rome",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Altoparlante",
		},
		DCAcceptLanguage:    "it-IT,it;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: 3.0,
		MagFieldIntensityUT: 46.0, // Rome WMM-2025
	},
	LocalePolishPoland: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "pl",
			RegionCountry:      "PL",
			TimezoneIdentifier: "Europe/Warsaw",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Głośnik", // iPhone 14 Pro real-device capture
		},
		DCAcceptLanguage:    "pl-PL,pl;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: 6.9727783203125, // Warsaw WMM-2025, taken verbatim
		// from the iPhone 15 / iOS 26.5 corpus (20/20 captures emit this value
		// to float32 precision — the device's own CLHeading already encodes
		// declination at this resolution).
		MagFieldIntensityUT: 50.0, // Warsaw WMM-2025
	},
	LocaleDutchNetherlands: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "nl",
			RegionCountry:      "NL",
			TimezoneIdentifier: "Europe/Amsterdam",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Luidspreker",
		},
		DCAcceptLanguage:    "nl-NL,nl;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: 2.5,
		MagFieldIntensityUT: 49.0, // Amsterdam WMM-2025
	},
	LocaleJapaneseJapan: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+09:00",
			RegionLanguage:     "ja",
			RegionCountry:      "JP",
			TimezoneIdentifier: "Asia/Tokyo",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "スピーカー",
		},
		DCAcceptLanguage:    "ja-JP,ja;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: -7.5,
		MagFieldIntensityUT: 46.0, // Tokyo WMM-2025
	},
	LocaleKoreanKorea: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+09:00",
			RegionLanguage:     "ko",
			RegionCountry:      "KR",
			TimezoneIdentifier: "Asia/Seoul",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "스피커",
		},
		DCAcceptLanguage:    "ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7",
		MagneticDeclination: -8.0,
		MagFieldIntensityUT: 51.0, // Seoul WMM-2025
	},
	LocaleAustraliaEnglish: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "+10:00",
			RegionLanguage:     "en",
			RegionCountry:      "AU",
			TimezoneIdentifier: "Australia/Sydney",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		DCAcceptLanguage:    "en-AU,en;q=0.9",
		MagneticDeclination: 12.5,
		MagFieldIntensityUT: 57.0, // Sydney WMM-2025 (southern high-lat → stronger Z)
	},
	LocaleCanadaEnglish: {
		Locale: seonsdk.LocaleConfig{
			RegionTimezone:     "-05:00",
			RegionLanguage:     "en",
			RegionCountry:      "CA",
			TimezoneIdentifier: "America/Toronto",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		DCAcceptLanguage:    "en-CA,en;q=0.9",
		MagneticDeclination: -10.0,
		MagFieldIntensityUT: 54.0, // Toronto WMM-2025
	},
}

// LocaleFromPreset returns the coherent bundle for a preset. The zero value
// (LocaleUKEnglish) is returned when the preset is unrecognised, so calls
// are always safe — never a nil / zero-struct surprise.
func LocaleFromPreset(p LocalePreset) LocaleBundle {
	if b, ok := localeBundles[p]; ok {
		return b
	}
	return localeBundles[LocaleUKEnglish]
}

// tzCountryPrefix maps a TimezoneIdentifier prefix to the country codes it
// corresponds to, for use in ValidateLocale. Not exhaustive — covers the
// regions we have presets for plus a handful of common near-neighbours.
var tzCountryPrefix = map[string][]string{
	"Europe/London":       {"GB"},
	"Europe/Isle_of_Man":  {"GB"},
	"Europe/Jersey":       {"GB"},
	"Europe/Guernsey":     {"GB"},
	"Europe/Dublin":       {"IE"},
	"Europe/Berlin":       {"DE"},
	"Europe/Paris":        {"FR"},
	"Europe/Madrid":       {"ES"},
	"Europe/Rome":         {"IT"},
	"Europe/Warsaw":       {"PL"},
	"Europe/Amsterdam":    {"NL"},
	"Europe/Brussels":     {"BE"},
	"Europe/Vienna":       {"AT"},
	"Europe/Zurich":       {"CH"},
	"Europe/Stockholm":    {"SE"},
	"Europe/Oslo":         {"NO"},
	"Europe/Copenhagen":   {"DK"},
	"Europe/Helsinki":     {"FI"},
	"Asia/Tokyo":          {"JP"},
	"Asia/Seoul":          {"KR"},
	"Asia/Shanghai":       {"CN"},
	"Asia/Hong_Kong":      {"HK"},
	"Asia/Singapore":      {"SG"},
	"Australia/Sydney":    {"AU"},
	"Australia/Melbourne": {"AU"},
	"Australia/Perth":     {"AU"},
	"America/New_York":    {"US"},
	"America/Chicago":     {"US"},
	"America/Denver":      {"US"},
	"America/Los_Angeles": {"US"},
	"America/Toronto":     {"CA"},
	"America/Vancouver":   {"CA"},
	"America/Mexico_City": {"MX"},
	"America/Sao_Paulo":   {"BR"},
	"Pacific/Auckland":    {"NZ"},
}

// ValidateLocale returns non-nil when the components of a LocaleConfig plus
// an Accept-Language string are mutually inconsistent in a way a real
// iPhone could never produce. The checks are intentionally loose — we only
// fail on combinations that are observably impossible on a stock device
// (e.g. `RegionCountry = "GB"` with `TimezoneIdentifier = "Asia/Tokyo"`).
// Leaving a field empty is allowed; only mismatches between filled fields
// raise errors.
func ValidateLocale(loc seonsdk.LocaleConfig, dcAcceptLanguage string) error {
	if loc.TimezoneIdentifier != "" && loc.RegionCountry != "" {
		if countries, ok := tzCountryPrefix[loc.TimezoneIdentifier]; ok {
			matched := false
			for _, c := range countries {
				if c == loc.RegionCountry {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("locale: timezone %q does not belong to country %q (expected one of %v)",
					loc.TimezoneIdentifier, loc.RegionCountry, countries)
			}
		}
	}

	if dcAcceptLanguage != "" && loc.RegionLanguage != "" {
		// Accept-Language must start with the primary language tag.
		// Loose match: the first two chars of the first entry should
		// equal RegionLanguage (lowercase).
		first := strings.SplitN(dcAcceptLanguage, ",", 2)[0]
		primary := strings.SplitN(first, "-", 2)[0]
		if !strings.EqualFold(primary, loc.RegionLanguage) {
			return fmt.Errorf("locale: Accept-Language %q starts with %q but region_language is %q",
				dcAcceptLanguage, primary, loc.RegionLanguage)
		}
	}

	if dcAcceptLanguage != "" && loc.RegionCountry != "" {
		// If the first Accept-Language entry has a country (en-GB form),
		// the country code should match RegionCountry.
		first := strings.SplitN(dcAcceptLanguage, ",", 2)[0]
		if parts := strings.SplitN(first, "-", 2); len(parts) == 2 {
			if !strings.EqualFold(parts[1], loc.RegionCountry) {
				// Strip semicolon qualifier like ";q=0.9" if present.
				country := strings.SplitN(parts[1], ";", 2)[0]
				if !strings.EqualFold(country, loc.RegionCountry) {
					return fmt.Errorf("locale: Accept-Language country %q != region_country %q",
						country, loc.RegionCountry)
				}
			}
		}
	}

	return nil
}
