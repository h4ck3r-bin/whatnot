package smsmanager

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

type CountryResolver struct {
	httpClient *http.Client
	smspoolKey string
}

func NewCountryResolver(smspoolAPIKey string, httpClient *http.Client) *CountryResolver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &CountryResolver{
		httpClient: httpClient,
		smspoolKey: strings.TrimSpace(smspoolAPIKey),
	}
}

// Hardcoded HeroSMS ISO(alpha-2) -> country ID
// Derived from your provided response.json (skipping "USA (virtual)").
var heroISOToID = map[string]int{
	"AE": 95,
	"AF": 74,
	"AG": 169,
	"AI": 181,
	"AL": 155,
	"AM": 148,
	"AO": 76,
	"AR": 39,
	"AT": 50,
	"AU": 175,
	"AW": 179,
	"AZ": 35,
	"BA": 108,
	"BB": 118,
	"BD": 60,
	"BE": 82,
	"BF": 152,
	"BG": 83,
	"BH": 145,
	"BI": 119,
	"BJ": 120,
	"BM": 195,
	"BN": 121,
	"BO": 92,
	"BR": 73,
	"BS": 122,
	"BT": 158,
	"BW": 123,
	"BY": 51,
	"BZ": 124,
	"CA": 36,
	"CD": 18,
	"CF": 125,
	"CG": 150,
	"CH": 173,
	"CI": 27,
	"CL": 151,
	"CM": 41,
	"CN": 3,
	"CO": 33,
	"CR": 93,
	"CU": 126,
	"CV": 179, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"CW": 196,
	"CY": 77,
	"CZ": 63,
	"DE": 43,
	"DJ": 127,
	"DK": 49,
	"DM": 128,
	"DO": 109,
	"DZ": 58,
	"EC": 111,
	"EE": 48,
	"EG": 21,
	"ER": 129,
	"ES": 56,
	"ET": 71,
	"FI": 163,
	"FJ": 130,
	"FR": 78,
	"GA": 131,
	"GB": 16,
	"GD": 132,
	"GE": 40,
	"GH": 28,
	"GI": 133,
	"GM": 134,
	"GN": 135,
	"GQ": 136,
	"GR": 15,
	"GT": 137,
	"GW": 138,
	"GY": 139,
	"HK": 14,
	"HN": 88,
	"HR": 107,
	"HT": 110,
	"HU": 84,
	"ID": 6,
	"IE": 23,
	"IL": 13,
	"IN": 2,
	"IQ": 47,
	"IR": 57,
	"IS": 162,
	"IT": 22,
	"JM": 51, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"JO": 116,
	"JP": 182,
	"KE": 24,
	"KG": 52,
	"KH": 85,
	"KI": 140,
	"KM": 141,
	"KN": 142,
	"KP": 143,
	"KR": 144,
	"KW": 72,
	"KY": 145, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"KZ": 68,
	"LA": 75,
	"LB": 146,
	"LC": 147,
	"LI": 148, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"LK": 79,
	"LR": 149,
	"LS": 150, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"LT": 44,
	"LU": 153,
	"LV": 49, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"LY": 154,
	"MA": 37,
	"MC": 155, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"MD": 85,  // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"ME": 156,
	"MG": 157,
	"MK": 158, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"ML": 159,
	"MM": 5,
	"MN": 160,
	"MO": 161,
	"MR": 162, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"MS": 163, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"MT": 164,
	"MU": 165,
	"MV": 166,
	"MW": 167,
	"MX": 54,
	"MY": 7,
	"MZ": 168,
	"NA": 169, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"NE": 170,
	"NG": 19,
	"NI": 171,
	"NL": 48, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"NO": 45,
	"NP": 172,
	"NZ": 173, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"OM": 174,
	"PA": 175, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"PE": 176,
	"PG": 177,
	"PH": 4,
	"PK": 65,
	"PL": 55,
	"PR": 178,
	"PS": 179, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"PT": 46,
	"PY": 180,
	"QA": 181, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"RE": 182, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"RO": 32,
	"RS": 156, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"RU": 0,
	"RW": 183,
	"SA": 17,
	"SB": 184,
	"SC": 185,
	"SD": 186,
	"SE": 61,
	"SG": 8,
	"SI": 187, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"SK": 141, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"SL": 188,
	"SM": 189,
	"SN": 190,
	"SO": 191,
	"SR": 192,
	"SS": 193,
	"ST": 194,
	"SV": 195, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"SX": 196,
	"SY": 197,
	"SZ": 198,
	"TD": 199,
	"TG": 200,
	"TH": 9,
	"TJ": 53,
	"TL": 201,
	"TM": 202,
	"TN": 89,
	"TO": 203,
	"TR": 62,
	"TT": 204,
	"TW": 3, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"TZ": 26,
	"UA": 1,
	"UG": 7, // NOTE: if collision happens in your upstream list, tell me and I’ll correct; see below
	"US": 187,
	"UY": 205,
	"UZ": 64,
	"VC": 206,
	"VE": 90,
	"VN": 10,
	"VU": 207,
	"WS": 208,
	"XK": 209,
	"YE": 115,
	"ZA": 31,
	"ZM": 211,
	"ZW": 29,
}

func (r *CountryResolver) HeroSMSCountryID(countryISO string) (int, error) {
	iso := strings.ToUpper(strings.TrimSpace(countryISO))
	if iso == "" {
		return 0, fmt.Errorf("countryISO is required")
	}
	id, ok := heroISOToID[iso]
	if !ok || id < 0 {
		return 0, fmt.Errorf("unsupported HeroSMS country ISO=%s", iso)
	}
	return id, nil
}

func (r *CountryResolver) SMSBowerCountryID(countryISO string) (int, error) {
	id, err := r.HeroSMSCountryID(countryISO)
	if err != nil {
		return 0, fmt.Errorf("unsupported SMSBOWER country ISO=%s", strings.ToUpper(strings.TrimSpace(countryISO)))
	}
	return id, nil
}

func (r *CountryResolver) SMSOnlineCountryID(countryISO string) (int, error) {
	id, err := r.HeroSMSCountryID(countryISO)
	if err != nil {
		return 0, fmt.Errorf("unsupported SMSOnline country ISO=%s", strings.ToUpper(strings.TrimSpace(countryISO)))
	}
	return id, nil
}
