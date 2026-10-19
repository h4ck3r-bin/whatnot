# Locale coherence — presets + validator

Setting each locale field one-at-a-time (`RegionLanguage`, `RegionCountry`, `TimezoneIdentifier`, `CalendarIdentifier`, `DCAcceptLanguage`) is error-prone — it's easy to end up with `RegionCountry: "GB"` + `TimezoneIdentifier: "Asia/Tokyo"` and silently ship a tell-flag combination that no real iPhone could produce. The `LocalePreset` enum + `ValidateLocale` helpers prevent that.

## Presets

Setting `DeviceOptions.LocalePreset` fills Locale + DCAcceptLanguage + the simulator's MagneticDeclination in matching, coherent bundles:

| Preset | Country | Language | Timezone | Accept-Language | Mag decl |
|---|---|---|---|---|---:|
| `LocaleUKEnglish` (default) | GB | en | Europe/London | `en-GB,en;q=0.9` | +0.5° |
| `LocaleUSEnglish` | US | en | America/New_York | `en-US,en;q=0.9` | −13° |
| `LocaleIrishEnglish` | IE | en | Europe/Dublin | `en-IE,en;q=0.9` | −2.5° |
| `LocaleGermanGermany` | DE | de | Europe/Berlin | `de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7` | +3.5° |
| `LocaleFrenchFrance` | FR | fr | Europe/Paris | `fr-FR,fr;q=0.9,…` | +1.0° |
| `LocaleSpanishSpain` | ES | es | Europe/Madrid | `es-ES,es;q=0.9,…` | +0.5° |
| `LocaleItalianItaly` | IT | it | Europe/Rome | `it-IT,it;q=0.9,…` | +3.0° |
| `LocalePolishPoland` | PL | pl | Europe/Warsaw | `pl-PL,pl;q=0.9,…` | +5.5° |
| `LocaleDutchNetherlands` | NL | nl | Europe/Amsterdam | `nl-NL,nl;q=0.9,…` | +2.5° |
| `LocaleJapaneseJapan` | JP | ja | Asia/Tokyo | `ja-JP,ja;q=0.9,…` | −7.5° |
| `LocaleKoreanKorea` | KR | ko | Asia/Seoul | `ko-KR,ko;q=0.9,…` | −8.0° |
| `LocaleAustraliaEnglish` | AU | en | Australia/Sydney | `en-AU,en;q=0.9` | +12.5° |
| `LocaleCanadaEnglish` | CA | en | America/Toronto | `en-CA,en;q=0.9` | −10° |

Mag-declination values are the WMM-2025 regional mean — useful for the behaviour simulator's `mag_declination` field.

## Usage

```go
// One-line locale — fills Locale, DCAcceptLanguage coherently.
dev, _ := devices.NewDevice(devices.DeviceOptions{
    Machine: "iPhone16,2", IOSVersion: "18.2",
    LocalePreset: devices.LocaleUKEnglish,
})
// cfg.Locale.RegionCountry       == "GB"
// cfg.DCAcceptLanguage           == "en-GB,en;q=0.9"
```

Override any specific field manually — explicit `Locale` always wins over `LocalePreset`:

```go
dev, _ := devices.NewDevice(devices.DeviceOptions{
    LocalePreset: devices.LocaleUKEnglish,   // starting point
    Locale: &seonsdk.LocaleConfig{
        RegionTimezone:     "+00:00",
        RegionLanguage:     "en",
        RegionCountry:      "GB",
        TimezoneIdentifier: "Europe/Isle_of_Man", // Crown dependency variant
        CalendarIdentifier: "gregorian",
    },
    DCAcceptLanguage: "en-GB,cy;q=0.9", // GB with Welsh fallback
})
```

## Resolver for custom helpers

If you construct a LocaleBundle directly (e.g. for a region not in the preset list):

```go
b := devices.LocaleBundle{
    Locale: seonsdk.LocaleConfig{
        RegionCountry: "SE", RegionLanguage: "sv",
        TimezoneIdentifier: "Europe/Stockholm",
        CalendarIdentifier: "gregorian",
        RegionTimezone:     "+01:00",
    },
    DCAcceptLanguage:    "sv-SE,sv;q=0.9,en-US;q=0.8,en;q=0.7",
    MagneticDeclination: 4.0,
}
if err := devices.ValidateLocale(b.Locale, b.DCAcceptLanguage); err != nil {
    panic(err)
}
```

## Validator

`devices.ValidateLocale(loc, dcAcceptLang)` returns non-nil when fields are mutually impossible. The checks are intentionally loose — leaving a field empty skips its validation, and only observably-impossible combinations (not just unusual ones) fail.

| Check | Fails on |
|---|---|
| `TimezoneIdentifier` vs `RegionCountry` | e.g. `Europe/London` paired with `JP` |
| `Accept-Language` primary language | e.g. `en-GB` paired with `RegionLanguage=pl` |
| `Accept-Language` country suffix | e.g. first entry `en-GB` paired with `RegionCountry=PL` |

Fields left empty skip their respective checks. `NewDevice` runs this validator automatically and returns the error up front — so you never ship a device with a silent locale mismatch.

## What the checks mean in practice

- SEON backend cross-checks `timezone_identifier` country prefix against `region_country` and against the source IP's GeoIP country. Mismatch across any of these three is the cheapest signal SEON has for "this fingerprint doesn't match the IP". The validator catches the first two at device construction; the third (GeoIP) requires your proxy layer to match the `LocalePreset`.

- The validator does NOT check `RegionTimezone` (the `+00:00` UTC-offset string). iOS updates it automatically per DST, so its drift from `TimezoneIdentifier` can legitimately happen during DST transitions. Out of scope.
