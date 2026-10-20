package smsmanager

import (
	"context"
	"fmt"
	"strings"
)

// Hardcoded SMSPool country IDs (ISO alpha-2 -> SMSPool country ID)
// Source: the mapping list you provided in chat.
//
// Notes:
//   - "US_V" and "AU_V" are NOT ISO alpha-2, so they are intentionally not included.
//   - If you ever want to support those virtual variants, we can add optional aliases like:
//     "USV" -> 22, "AUV" -> 136, but you explicitly asked for ISO-only inputs.
var smspoolISOToID = map[string]int{
	// North America / Caribbean / Central America
	"US": 1,
	"MX": 53,
	"GP": 128,
	"AI": 148,
	"HT": 35,
	"CO": 39,
	"AR": 43,
	"PE": 61,
	"VE": 65,
	"BR": 68,
	"PY": 80,
	"BO": 84,
	"EC": 89,
	"GY": 103,
	"SR": 113,
	"CL": 120,
	"UY": 124,
	"AW": 138,
	"HN": 81,
	"NI": 83,
	"GT": 85,
	"BZ": 100,
	"JM": 142,
	"BB": 143,
	"BS": 144,
	"DM": 145,
	"GD": 146,
	"MS": 147,

	// Europe
	"GB": 2,
	"NL": 3,
	"LV": 5,
	"SE": 6,
	"PT": 8,
	"EE": 10,
	"RO": 13,
	"DK": 19,
	"PL": 21,
	"FR": 23,
	"DE": 24,
	"UA": 25,
	"IE": 32,
	"RS": 37,
	"LT": 47,
	"HR": 48,
	"AT": 50,
	"BY": 51,
	"ES": 55,
	"SI": 57,
	"BE": 75,
	"BG": 76,
	"HU": 77,
	"MD": 78,
	"IT": 79,
	"GR": 102,
	"IS": 104,
	"SK": 112,
	"MC": 115,
	"AL": 123,
	"FI": 130,
	"LU": 131,
	"ME": 133,
	"CH": 134,
	"NO": 135,
	"CZ": 149,
	"AX": 156,
	"GI": 158,
	"BA": 162,
	"MT": 164,

	// Asia / Middle East
	"KZ": 7,
	"ID": 9,
	"VN": 11,
	"PH": 12,
	"IN": 15,
	"KG": 18,
	"MY": 20,
	"IL": 29,
	"KH": 33,
	"LA": 34,
	"YE": 38,
	"UZ": 44,
	"IQ": 49,
	"TH": 52,
	"TW": 54,
	"BD": 58,
	"TR": 60,
	"PK": 62,
	"MN": 67,
	"AF": 69,
	"CY": 72,
	"NP": 74,
	"KW": 88,
	"OM": 91,
	"QA": 92,
	"JO": 95,
	"BN": 98,
	"GE": 101,
	"TJ": 114,
	"AM": 118,
	"LB": 121,
	"BT": 126,
	"MV": 127,
	"TM": 129,
	"SG": 141,
	"HK": 151,
	"AZ": 154,
	"JP": 157,
	"MO": 163,

	// Africa
	"NG": 14,
	"KE": 16,
	"TZ": 27,
	"MG": 30,
	"EG": 31,
	"GM": 36,
	"MA": 41,
	"GH": 42,
	"CM": 45,
	"TD": 46,
	"DZ": 56,
	"SN": 59,
	"GN": 63,
	"ML": 64,
	"ET": 66,
	"UG": 70,
	"AO": 71,
	"MZ": 73,
	"TN": 82,
	"ZW": 86,
	"TG": 87,
	"SZ": 90,
	"MR": 94,
	"BI": 96,
	"BJ": 97,
	"BW": 99,
	"KM": 105,
	"LR": 106,
	"LS": 107,
	"MW": 108,
	"NA": 109,
	"NE": 110,
	"RW": 111,
	"ZM": 117,
	"SO": 119,
	"GA": 122,
	"MU": 125,
	"DJ": 132,
	"ER": 137,
	"SC": 139,
	"ZA": 153,

	// Oceania
	"FJ": 140,
}

func (r *CountryResolver) SMSPoolCountryID(_ context.Context, countryISO string) (int, error) {
	iso := strings.ToUpper(strings.TrimSpace(countryISO))
	if iso == "" {
		return 0, fmt.Errorf("countryISO is required")
	}

	id, ok := smspoolISOToID[iso]
	if !ok || id <= 0 {
		return 0, fmt.Errorf("unsupported SMSPool country ISO=%s", iso)
	}
	return id, nil
}
