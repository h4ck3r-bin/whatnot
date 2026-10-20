package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	CapsolverKey     string            `json:"capsolver_key"`
	HeroSMSKey       string            `json:"herosms"`
	SMSPoolKey       string            `json:"sms_pool"`
	SMSBowerKey      string            `json:"smsbower"`
	SMSOnlineKey     string            `json:"smsonline"`
	HyperKey         string            `json:"hyper"`
	HyperDebug       bool              `json:"hyper_debug"`
	HyperProxy       bool              `json:"hyper_proxy"`
	RandomEmail      bool              `json:"random_email"`
	SuccessWebhook   string            `json:"success_webhook"`
	ErrorWebhook     string            `json:"error_webhook"`
	CheckoutChance   int               `json:"checkout_chance"`
	StartHour        int               `json:"start_hour"`
	EndHour          int               `json:"end_hour"`
	StartMinute      int               `json:"start_minute"`
	EndMinute        int               `json:"end_minute"`
	SMSService       []string          `json:"sms_service"`
	HeroSMSRegions   []string          `json:"herosms_regions"`
	SMSPoolRegions   []string          `json:"smspool_regions"`
	SMSBowerRegions  []string          `json:"smsbower_regions"`
	SMSOnlineRegions []string          `json:"smsonline_regions"`
	AppVersions      AppVersionsConfig `json:"app_versions"`
	Delays           Delays            `json:"delays"`
	Engine           EngineConfig      `json:"engine"`
}

type AppVersionsConfig struct {
	Whatnot AppVersionsWhatnotConfig `json:"whatnot"`
	Stripe  AppVersionsStripeConfig  `json:"stripe"`
	Kasada  AppVersionsKasadaConfig  `json:"kasada"`

	// Legacy flat keys are kept so older config files still load.
	WebAppVersion             string `json:"web_app_version"`
	IOSAppVersion             string `json:"ios_app_version"`
	IOSAppBuild               string `json:"ios_app_build"`
	IOSApolloClientVersion    string `json:"ios_apollo_client_version"`
	BrowserUserAgent          string `json:"browser_user_agent"`
	BrowserSecChUa            string `json:"browser_sec_ch_ua"`
	BrowserSecChUaPlatform    string `json:"browser_sec_ch_ua_platform"`
	KasadaVersion             string `json:"kasada_version"`
	StripeAPIVersion          string `json:"stripe_api_version"`
	StripeMobileAppID         string `json:"stripe_mobile_app_id"`
	StripeIOSVersion          string `json:"stripe_ios_version"`
	StripePaymentUserAgent    string `json:"stripe_payment_user_agent"`
	StripeIntegrationVersion  string `json:"stripe_integration_version"`
	StripeBindingsVersion     string `json:"stripe_bindings_version"`
	WhatnotIOSStripeUserAgent string `json:"whatnot_ios_stripe_user_agent"`
	MobileCheckoutLibraryVer  string `json:"mobile_checkout_library_version"`
	MobileCheckoutRequestTag  string `json:"mobile_checkout_request_tag"`
	ReferralClientLibraryVer  string `json:"referral_client_library_version"`
}

type AppVersionsWhatnotConfig struct {
	WebAppVersion             string `json:"web_app_version"`
	IOSAppVersion             string `json:"ios_app_version"`
	IOSAppBuild               string `json:"ios_app_build"`
	IOSApolloClientVersion    string `json:"ios_apollo_client_version"`
	WhatnotIOSStripeUserAgent string `json:"ios_stripe_user_agent"`
	MobileCheckoutLibraryVer  string `json:"mobile_checkout_library_version"`
	MobileCheckoutRequestTag  string `json:"mobile_checkout_request_tag"`
	ReferralClientLibraryVer  string `json:"referral_client_library_version"`
	BrowserUserAgent          string `json:"browser_user_agent"`
	BrowserSecChUa            string `json:"browser_sec_ch_ua"`
	BrowserSecChUaPlatform    string `json:"browser_sec_ch_ua_platform"`
}

type AppVersionsStripeConfig struct {
	APIVersion         string `json:"api_version"`
	MobileAppID        string `json:"mobile_app_id"`
	IOSVersion         string `json:"ios_version"`
	PaymentUserAgent   string `json:"payment_user_agent"`
	IntegrationVersion string `json:"integration_version"`
	BindingsVersion    string `json:"bindings_version"`
}

type AppVersionsKasadaConfig struct {
	Version    string `json:"version"`
	WebVersion string `json:"web_version"`
}

type EngineConfig struct {
	DiscordWebhookURL          string        `json:"discord_webhook_url"`
	Timezone                   string        `json:"timezone"`
	ProxyFile                  string        `json:"proxy_file"`
	ProfilesCSVPath            string        `json:"profiles_csv_path"`
	CardsCSVPath               string        `json:"cards_csv_path"`
	FriendCodesPath            string        `json:"friend_codes_path"`
	BasketsJSONPath            string        `json:"baskets_json_path"`
	TaskWindowStart            string        `json:"task_window_start"`
	TaskWindowEnd              string        `json:"task_window_end"`
	OrderCheckWindowStart      string        `json:"order_check_window_start"`
	OrderCheckWindowEnd        string        `json:"order_check_window_end"`
	MasterCandidatePercentage  float64       `json:"master_candidate_percentage"`
	MasterBaseSignupCap        int           `json:"master_base_signup_cap"`
	MasterCapRandomizationPct  float64       `json:"master_cap_randomization_percentage"`
	MasterAgingDurationHours   int           `json:"master_aging_duration_hours"`
	MasterHealthCheckInterval  DurationField `json:"master_health_check_interval"`
	ReviewTextChance           float64       `json:"review_text_chance"`
	ReviewTextJSONPath         string        `json:"review_text_json_path"`
	ReviewPreflightMinFollows  int           `json:"review_preflight_min_follows"`
	ReviewPreflightMaxFollows  int           `json:"review_preflight_max_follows"`
	ReviewUsernameAttempts     int           `json:"review_username_attempts"`
	ReviewSellerFeedID         string        `json:"review_seller_feed_id"`
	ReviewSellerFeedFilter     string        `json:"review_seller_feed_filter"`
	ReviewSellerFeedObjectSize int           `json:"review_seller_feed_object_size"`
	ReviewSellerFeedMaxPages   int           `json:"review_seller_feed_max_pages"`
	CheckoutPreorderMinFollows int           `json:"checkout_preorder_min_follows"`
	CheckoutPreorderMaxFollows int           `json:"checkout_preorder_max_follows"`
	CheckoutLabelCSVPath       string        `json:"checkout_label_csv_path"`
	AccountsPerRun             int           `json:"accounts_per_run"`
	TaskConcurrency            int           `json:"task_concurrency"`
	StaleTaskAfter             DurationField `json:"stale_task_after"`
	StubMode                   bool          `json:"stub_mode"`
	Storage                    StorageConfig `json:"storage"`
}

type StorageConfig struct {
	DatabasePath       string `json:"database_path"`
	AccountsPath       string `json:"accounts_path"`
	OrdersPath         string `json:"orders_path"`
	NormalSessionsPath string `json:"normal_sessions_path"`
	MasterSessionsPath string `json:"master_sessions_path"`
	MastersPath        string `json:"masters_path"`
	RuntimePath        string `json:"runtime_path"`
	TaskQueuePath      string `json:"task_queue_path"`
	PendingReviewsPath string `json:"pending_reviews_path"`
	DiscoveryCachePath string `json:"discovery_cache_path"`
	ErrorDedupePath    string `json:"error_dedupe_path"`
}

type DurationField struct {
	Duration time.Duration
}

func (d *DurationField) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		parsed, err := time.ParseDuration(strings.TrimSpace(text))
		if err != nil {
			return err
		}
		d.Duration = parsed
		return nil
	}

	var hours float64
	if err := json.Unmarshal(raw, &hours); err != nil {
		return err
	}
	d.Duration = time.Duration(hours * float64(time.Hour))
	return nil
}

func (d DurationField) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// Delays is a map of named delay ranges, where each range is [min_secs, max_secs].
// Unknown keys are preserved so new delay names can be added in config.json without
// changing the struct — callers use Range(name) to look up a named delay.
type Delays map[string][]float64

func (d *Delays) UnmarshalJSON(raw []byte) error {
	delays, err := parseDelayBytes(raw)
	if err != nil {
		return err
	}
	*d = delays
	return nil
}

// Range returns the [min, max] seconds for a named delay. If the key is missing
// or malformed, it returns (0, 0, false).
func (d Delays) Range(name string) (float64, float64, bool) {
	if d == nil {
		return 0, 0, false
	}
	values, ok := d[name]
	if !ok || len(values) == 0 {
		return 0, 0, false
	}
	minSecs := values[0]
	maxSecs := minSecs
	if len(values) > 1 {
		maxSecs = values[1]
	}
	if maxSecs < minSecs {
		minSecs, maxSecs = maxSecs, minSecs
	}
	return minSecs, maxSecs, true
}

var (
	once      sync.Once
	cached    *Config
	cachedErr error
)

func Load() (*Config, error) {
	once.Do(func() {
		cached, cachedErr = LoadFresh()
	})

	return cached, cachedErr
}

func LoadFresh() (*Config, error) {
	path, err := resolvePath()
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err = ensureConfigFileDefaults(path, raw)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}

	cfg.loadExternalDelays()
	cfg.normalize()
	return &cfg, nil
}

func (c *Config) normalize() {
	c.CapsolverKey = strings.TrimSpace(c.CapsolverKey)
	c.HeroSMSKey = strings.TrimSpace(c.HeroSMSKey)
	c.SMSPoolKey = strings.TrimSpace(c.SMSPoolKey)
	c.SMSBowerKey = strings.TrimSpace(c.SMSBowerKey)
	c.SMSOnlineKey = strings.TrimSpace(c.SMSOnlineKey)
	c.HyperKey = strings.TrimSpace(c.HyperKey)
	c.SuccessWebhook = strings.TrimSpace(c.SuccessWebhook)
	c.ErrorWebhook = strings.TrimSpace(c.ErrorWebhook)
	c.AppVersions.normalize()
	c.Engine.normalize()
	if c.Engine.DiscordWebhookURL == "" {
		c.Engine.DiscordWebhookURL = c.SuccessWebhook
	}
	if c.Engine.TaskWindowStart == "00:00" && (c.StartHour > 0 || c.StartMinute > 0) {
		c.Engine.TaskWindowStart = formatClock(c.StartHour, c.StartMinute)
	}
	if c.Engine.TaskWindowEnd == "23:59" && (c.EndHour > 0 || c.EndMinute > 0) {
		c.Engine.TaskWindowEnd = formatClock(c.EndHour, c.EndMinute)
	}

	c.SMSService = normalizeList(c.SMSService)
	c.HeroSMSRegions = normalizeList(c.HeroSMSRegions)
	c.SMSPoolRegions = normalizeList(c.SMSPoolRegions)
	c.SMSBowerRegions = normalizeList(c.SMSBowerRegions)
	c.SMSOnlineRegions = normalizeList(c.SMSOnlineRegions)
}

func (c *AppVersionsConfig) normalize() {
	c.Whatnot.WebAppVersion = defaultString(c.Whatnot.WebAppVersion, c.WebAppVersion)
	c.Whatnot.IOSAppVersion = defaultString(c.Whatnot.IOSAppVersion, c.IOSAppVersion)
	c.Whatnot.IOSAppBuild = defaultString(c.Whatnot.IOSAppBuild, c.IOSAppBuild)
	c.Whatnot.IOSApolloClientVersion = defaultString(c.Whatnot.IOSApolloClientVersion, c.IOSApolloClientVersion)
	c.Whatnot.WhatnotIOSStripeUserAgent = defaultString(c.Whatnot.WhatnotIOSStripeUserAgent, c.WhatnotIOSStripeUserAgent)
	c.Whatnot.MobileCheckoutLibraryVer = defaultString(c.Whatnot.MobileCheckoutLibraryVer, c.MobileCheckoutLibraryVer)
	c.Whatnot.MobileCheckoutRequestTag = defaultString(c.Whatnot.MobileCheckoutRequestTag, c.MobileCheckoutRequestTag)
	c.Whatnot.ReferralClientLibraryVer = defaultString(c.Whatnot.ReferralClientLibraryVer, c.ReferralClientLibraryVer)
	c.Whatnot.BrowserUserAgent = defaultString(c.Whatnot.BrowserUserAgent, c.BrowserUserAgent)
	c.Whatnot.BrowserSecChUa = defaultString(c.Whatnot.BrowserSecChUa, c.BrowserSecChUa)
	c.Whatnot.BrowserSecChUaPlatform = defaultString(c.Whatnot.BrowserSecChUaPlatform, c.BrowserSecChUaPlatform)

	c.Stripe.APIVersion = defaultString(c.Stripe.APIVersion, c.StripeAPIVersion)
	c.Stripe.MobileAppID = defaultString(c.Stripe.MobileAppID, c.StripeMobileAppID)
	c.Stripe.IOSVersion = defaultString(c.Stripe.IOSVersion, c.StripeIOSVersion)
	c.Stripe.PaymentUserAgent = defaultString(c.Stripe.PaymentUserAgent, c.StripePaymentUserAgent)
	c.Stripe.IntegrationVersion = defaultString(c.Stripe.IntegrationVersion, c.StripeIntegrationVersion)
	c.Stripe.BindingsVersion = defaultString(c.Stripe.BindingsVersion, c.StripeBindingsVersion)

	c.Kasada.Version = defaultString(c.Kasada.Version, c.KasadaVersion)

	c.Whatnot.WebAppVersion = defaultString(c.Whatnot.WebAppVersion, "20260529-1631")
	c.Whatnot.IOSAppVersion = defaultString(c.Whatnot.IOSAppVersion, "26.22.0")
	c.Whatnot.IOSAppBuild = defaultString(c.Whatnot.IOSAppBuild, "51")
	c.Whatnot.IOSApolloClientVersion = defaultString(c.Whatnot.IOSApolloClientVersion, "2.0.6")
	c.Whatnot.BrowserUserAgent = defaultString(c.Whatnot.BrowserUserAgent, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")
	c.Whatnot.BrowserSecChUa = defaultString(c.Whatnot.BrowserSecChUa, `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`)
	c.Whatnot.BrowserSecChUaPlatform = defaultString(c.Whatnot.BrowserSecChUaPlatform, `"Windows"`)
	c.Kasada.Version = defaultString(c.Kasada.Version, "j-1.2.386")
	c.Kasada.WebVersion = defaultString(c.Kasada.WebVersion, c.Kasada.Version)
	c.Stripe.APIVersion = defaultString(c.Stripe.APIVersion, "2020-08-27")
	c.Stripe.MobileAppID = defaultString(c.Stripe.MobileAppID, "com.whatnot")
	c.Stripe.IOSVersion = defaultString(c.Stripe.IOSVersion, "24.25.0")
	c.Stripe.BindingsVersion = defaultString(c.Stripe.BindingsVersion, c.Stripe.IOSVersion)
	c.Stripe.IntegrationVersion = defaultString(c.Stripe.IntegrationVersion, "stripe-ios/"+c.Stripe.IOSVersion)
	c.Stripe.PaymentUserAgent = defaultString(c.Stripe.PaymentUserAgent, "stripe-ios/"+c.Stripe.IOSVersion+"; variant.paymentsheet; PaymentSheet")
	c.Whatnot.WhatnotIOSStripeUserAgent = defaultString(c.Whatnot.WhatnotIOSStripeUserAgent, "Whatnot/"+c.Whatnot.IOSAppBuild+" CFNetwork/1399 Darwin/22.1.0")
	c.Whatnot.MobileCheckoutLibraryVer = defaultString(c.Whatnot.MobileCheckoutLibraryVer, c.Whatnot.IOSApolloClientVersion)
	c.Whatnot.MobileCheckoutRequestTag = defaultString(c.Whatnot.MobileCheckoutRequestTag, "gql_sdk_version="+c.Whatnot.IOSApolloClientVersion)
	c.Whatnot.ReferralClientLibraryVer = defaultString(c.Whatnot.ReferralClientLibraryVer, "5.0.0-alpha.5-20260208")

	c.WebAppVersion = c.Whatnot.WebAppVersion
	c.IOSAppVersion = c.Whatnot.IOSAppVersion
	c.IOSAppBuild = c.Whatnot.IOSAppBuild
	c.IOSApolloClientVersion = c.Whatnot.IOSApolloClientVersion
	c.BrowserUserAgent = c.Whatnot.BrowserUserAgent
	c.BrowserSecChUa = c.Whatnot.BrowserSecChUa
	c.BrowserSecChUaPlatform = c.Whatnot.BrowserSecChUaPlatform
	c.KasadaVersion = c.Kasada.Version
	c.StripeAPIVersion = c.Stripe.APIVersion
	c.StripeMobileAppID = c.Stripe.MobileAppID
	c.StripeIOSVersion = c.Stripe.IOSVersion
	c.StripePaymentUserAgent = c.Stripe.PaymentUserAgent
	c.StripeIntegrationVersion = c.Stripe.IntegrationVersion
	c.StripeBindingsVersion = c.Stripe.BindingsVersion
	c.WhatnotIOSStripeUserAgent = c.Whatnot.WhatnotIOSStripeUserAgent
	c.MobileCheckoutLibraryVer = c.Whatnot.MobileCheckoutLibraryVer
	c.MobileCheckoutRequestTag = c.Whatnot.MobileCheckoutRequestTag
	c.ReferralClientLibraryVer = c.Whatnot.ReferralClientLibraryVer
}

func ensureConfigFileDefaults(path string, raw []byte) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return raw, err
	}
	if root == nil {
		root = map[string]any{}
	}

	changed := false
	addDefault := func(key string, value any) {
		if _, ok := root[key]; !ok {
			root[key] = value
			changed = true
		}
	}

	addDefault("capsolver_key", "")
	addDefault("herosms", "")
	addDefault("sms_pool", "")
	addDefault("smsbower", "")
	addDefault("smsonline", "")
	addDefault("hyper", "")
	addDefault("hyper_debug", false)
	if _, ok := root["random_email"]; !ok {
		value := false
		if engine, ok := rootObject(root, "engine"); ok {
			if existing, ok := engine["random_email"].(bool); ok {
				value = existing
			}
		}
		root["random_email"] = value
		changed = true
	}
	if engine, ok := rootObject(root, "engine"); ok {
		if _, exists := engine["random_email"]; exists {
			delete(engine, "random_email")
			changed = true
		}
	}
	addDefault("success_webhook", "")
	addDefault("error_webhook", "")
	addDefault("checkout_chance", float64(0))
	addDefault("start_hour", float64(0))
	addDefault("end_hour", float64(23))
	addDefault("start_minute", float64(0))
	addDefault("end_minute", float64(59))
	addDefault("sms_service", []any{"smspool"})
	addDefault("herosms_regions", []any{"NL"})
	addDefault("smspool_regions", []any{"AT"})
	addDefault("smsbower_regions", []any{"AT"})
	addDefault("smsonline_regions", []any{"AT"})
	engine, engineChanged := ensureObject(root, "engine")
	changed = changed || engineChanged
	changed = ensureMapDefaults(engine, map[string]any{
		"discord_webhook_url":                 "",
		"timezone":                            "Europe/London",
		"proxy_file":                          "setup/proxies.txt",
		"profiles_csv_path":                   "setup/profiles.csv",
		"cards_csv_path":                      "setup/cards.csv",
		"friend_codes_path":                   "setup/friend_codes.csv",
		"baskets_json_path":                   "setup/baskets.json",
		"task_window_start":                   "00:00",
		"task_window_end":                     "23:59",
		"order_check_window_start":            "16:00",
		"order_check_window_end":              "22:00",
		"master_candidate_percentage":         float64(1),
		"master_base_signup_cap":              float64(100),
		"master_cap_randomization_percentage": float64(20),
		"master_aging_duration_hours":         float64(168),
		"master_health_check_interval":        "4h",
		"review_text_chance":                  float64(15),
		"review_text_json_path":               "setup/review_texts.json",
		"review_preflight_min_follows":        float64(10),
		"review_preflight_max_follows":        float64(100),
		"review_username_attempts":            float64(3),
		"review_seller_feed_id":               "CATEGORY_FEED_V2:TGl2ZXN0cmVhbVRhZ05vZGU6ODk5",
		"review_seller_feed_filter":           "t__907",
		"review_seller_feed_object_size":      float64(120),
		"review_seller_feed_max_pages":        float64(15),
		"checkout_preorder_min_follows":       float64(2),
		"checkout_preorder_max_follows":       float64(5),
		"checkout_label_csv_path":             "checkouts_label_details.csv",
		"accounts_per_run":                    float64(1),
		"task_concurrency":                    float64(1),
		"stale_task_after":                    "30m",
		"stub_mode":                           false,
	}, changed)

	storage, storageChanged := ensureObject(engine, "storage")
	changed = changed || storageChanged
	changed = ensureMapDefaults(storage, map[string]any{
		"database_path":        "storage/state.sqlite",
		"accounts_path":        "storage/accounts.json",
		"orders_path":          "storage/orders.json",
		"normal_sessions_path": "storage/sessions/normal_sessions.csv",
		"master_sessions_path": "storage/sessions/master_sessions.csv",
		"masters_path":         "storage/masters.json",
		"runtime_path":         "storage/runtime.json",
		"task_queue_path":      "storage/task_queue.json",
		"pending_reviews_path": "storage/pending_reviews.json",
		"discovery_cache_path": "storage/account_discovery_snapshots.json",
		"error_dedupe_path":    "storage/error_webhook_dedupe.json",
	}, changed)

	appVersions, appChanged := ensureObject(root, "app_versions")
	changed = changed || appChanged
	whatnot, whatnotChanged := ensureObject(appVersions, "whatnot")
	changed = changed || whatnotChanged
	stripe, stripeChanged := ensureObject(appVersions, "stripe")
	changed = changed || stripeChanged
	kasada, kasadaChanged := ensureObject(appVersions, "kasada")
	changed = changed || kasadaChanged

	changed = ensureMapDefaults(whatnot, map[string]any{
		"web_app_version":                 firstPresentString(appVersions, "web_app_version", "20260529-1631"),
		"ios_app_version":                 firstPresentString(appVersions, "ios_app_version", "26.22.0"),
		"ios_app_build":                   firstPresentString(appVersions, "ios_app_build", "51"),
		"ios_apollo_client_version":       firstPresentString(appVersions, "ios_apollo_client_version", "2.0.6"),
		"ios_stripe_user_agent":           firstPresentString(appVersions, "whatnot_ios_stripe_user_agent", "Whatnot/51 CFNetwork/1399 Darwin/22.1.0"),
		"mobile_checkout_library_version": firstPresentString(appVersions, "mobile_checkout_library_version", "2.0.6"),
		"mobile_checkout_request_tag":     firstPresentString(appVersions, "mobile_checkout_request_tag", "gql_sdk_version=2.0.6"),
		"referral_client_library_version": firstPresentString(appVersions, "referral_client_library_version", "5.0.0-alpha.5-20260208"),
		"browser_user_agent":              firstPresentString(appVersions, "browser_user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"),
		"browser_sec_ch_ua":               firstPresentString(appVersions, "browser_sec_ch_ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`),
		"browser_sec_ch_ua_platform":      firstPresentString(appVersions, "browser_sec_ch_ua_platform", `"Windows"`),
	}, changed)

	changed = ensureMapDefaults(stripe, map[string]any{
		"api_version":         firstPresentString(appVersions, "stripe_api_version", "2020-08-27"),
		"mobile_app_id":       firstPresentString(appVersions, "stripe_mobile_app_id", "com.whatnot"),
		"ios_version":         firstPresentString(appVersions, "stripe_ios_version", "24.25.0"),
		"payment_user_agent":  firstPresentString(appVersions, "stripe_payment_user_agent", "stripe-ios/24.25.0; variant.paymentsheet; PaymentSheet"),
		"integration_version": firstPresentString(appVersions, "stripe_integration_version", "stripe-ios/24.25.0"),
		"bindings_version":    firstPresentString(appVersions, "stripe_bindings_version", "24.25.0"),
	}, changed)

	changed = ensureMapDefaults(kasada, map[string]any{
		"version": firstPresentString(appVersions, "kasada_version", "j-1.2.386"),
	}, changed)

	repaired, err := marshalConfigRoot(root)
	if err != nil {
		return raw, err
	}
	if bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(repaired)) {
		return raw, nil
	}
	if err := validateJSONObject(repaired); err != nil {
		return raw, err
	}
	if err := writeConfigSafely(path, raw, repaired); err != nil {
		return raw, err
	}
	return repaired, nil
}

func marshalConfigRoot(root map[string]any) ([]byte, error) {
	var b strings.Builder
	if err := writeOrderedValue(&b, root, "root", 0); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return []byte(b.String()), nil
}

func writeOrderedValue(b *strings.Builder, value any, path string, indent int) error {
	if obj, ok := value.(map[string]any); ok {
		return writeOrderedMap(b, obj, path, indent)
	}

	raw, err := json.MarshalIndent(value, strings.Repeat(" ", indent), "  ")
	if err != nil {
		return err
	}
	b.Write(raw)
	return nil
}

func writeOrderedMap(b *strings.Builder, obj map[string]any, path string, indent int) error {
	b.WriteString("{")
	keys := orderedConfigKeys(obj, path)
	if len(keys) == 0 {
		b.WriteString("}")
		return nil
	}
	b.WriteByte('\n')
	for i, key := range keys {
		if i > 0 {
			b.WriteString(",\n")
		}
		keyJSON, err := json.Marshal(key)
		if err != nil {
			return err
		}
		b.WriteString(strings.Repeat(" ", indent+2))
		b.Write(keyJSON)
		b.WriteString(": ")
		if err := writeOrderedValue(b, obj[key], childConfigPath(path, key), indent+2); err != nil {
			return err
		}
	}
	b.WriteByte('\n')
	b.WriteString(strings.Repeat(" ", indent))
	b.WriteString("}")
	return nil
}

func orderedConfigKeys(obj map[string]any, path string) []string {
	preferred := configKeyOrder(path)
	seen := map[string]bool{}
	keys := make([]string, 0, len(obj))
	for _, key := range preferred {
		if _, ok := obj[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}

	extras := make([]string, 0, len(obj))
	for key := range obj {
		if !seen[key] {
			extras = append(extras, key)
		}
	}
	sort.Strings(extras)
	return append(keys, extras...)
}

func configKeyOrder(path string) []string {
	switch path {
	case "root":
		return []string{
			"capsolver_key",
			"herosms",
			"sms_pool",
			"smsbower",
			"smsonline",
			"hyper",
			"hyper_debug",
			"success_webhook",
			"error_webhook",
			"checkout_chance",
			"random_email",
			"start_hour",
			"end_hour",
			"start_minute",
			"end_minute",
			"sms_service",
			"herosms_regions",
			"smspool_regions",
			"smsbower_regions",
			"smsonline_regions",
			"engine",
			"app_versions",
		}
	case "root.engine":
		return []string{
			"timezone",
			"proxy_file",
			"profiles_csv_path",
			"cards_csv_path",
			"friend_codes_path",
			"baskets_json_path",
			"discord_webhook_url",
			"task_window_start",
			"task_window_end",
			"order_check_window_start",
			"order_check_window_end",
			"accounts_per_run",
			"task_concurrency",
			"master_candidate_percentage",
			"master_base_signup_cap",
			"master_cap_randomization_percentage",
			"master_aging_duration_hours",
			"master_health_check_interval",
			"review_text_chance",
			"review_text_json_path",
			"review_preflight_min_follows",
			"review_preflight_max_follows",
			"review_username_attempts",
			"review_seller_feed_id",
			"review_seller_feed_filter",
			"review_seller_feed_object_size",
			"review_seller_feed_max_pages",
			"checkout_label_csv_path",
			"stale_task_after",
			"stub_mode",
			"storage",
		}
	case "root.engine.storage":
		return []string{
			"accounts_path",
			"orders_path",
			"normal_sessions_path",
			"master_sessions_path",
			"masters_path",
			"runtime_path",
			"task_queue_path",
		}
	case "root.app_versions":
		return []string{"whatnot", "stripe", "kasada"}
	case "root.app_versions.whatnot":
		return []string{
			"web_app_version",
			"ios_app_version",
			"ios_app_build",
			"ios_apollo_client_version",
			"ios_stripe_user_agent",
			"mobile_checkout_library_version",
			"mobile_checkout_request_tag",
			"referral_client_library_version",
			"browser_user_agent",
			"browser_sec_ch_ua",
			"browser_sec_ch_ua_platform",
		}
	case "root.app_versions.stripe":
		return []string{
			"api_version",
			"mobile_app_id",
			"ios_version",
			"payment_user_agent",
			"integration_version",
			"bindings_version",
		}
	case "root.app_versions.kasada":
		return []string{"version"}
	default:
		return nil
	}
}

func childConfigPath(parent string, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func ensureMapDefaults(target map[string]any, defaults map[string]any, changed bool) bool {
	for key, value := range defaults {
		if _, ok := target[key]; !ok {
			target[key] = value
			changed = true
		}
	}
	return changed
}

func ensureObject(parent map[string]any, key string) (map[string]any, bool) {
	if existing, ok := parent[key].(map[string]any); ok {
		return existing, false
	}
	value := map[string]any{}
	parent[key] = value
	return value, true
}

func rootObject(parent map[string]any, key string) (map[string]any, bool) {
	value, ok := parent[key].(map[string]any)
	return value, ok
}

func firstPresentString(values map[string]any, key string, fallback string) string {
	if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func validateJSONObject(raw []byte) error {
	var value map[string]any
	return json.Unmarshal(raw, &value)
}

func writeConfigSafely(path string, oldRaw []byte, newRaw []byte) error {
	backupPath := path + ".bak"
	tmpPath := path + ".tmp"
	if err := os.WriteFile(backupPath, oldRaw, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, newRaw, 0600); err != nil {
		return err
	}
	if err := validateJSONObject(newRaw); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.WriteFile(path, oldRaw, 0600)
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (c *Config) loadExternalDelays() {
	if len(c.Delays) == 0 {
		c.Delays = defaultDelays()
	}

	path := filepath.Join("setup", "delays.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if exe, exeErr := os.Executable(); exeErr == nil {
			path = filepath.Join(filepath.Dir(exe), "setup", "delays.json")
			raw, err = os.ReadFile(path)
		}
	}
	if err != nil {
		path = filepath.Join("setup", "delays.json")
		raw, err = writeDelayConfigSafely(path, nil, defaultDelaySections())
		if err != nil {
			return
		}
	}
	if updatedRaw, updateErr := ensureDelayDefaults(path, raw); updateErr == nil {
		raw = updatedRaw
	}

	delays, err := parseDelayBytes(raw)
	if err != nil {
		return
	}
	for name, value := range delays {
		c.Delays[name] = value
	}
}

func defaultDelays() Delays {
	out := Delays{}
	for _, section := range defaultDelaySections() {
		for name, value := range section {
			out[name] = value
		}
	}
	return out
}

func defaultDelaySections() map[string]Delays {
	return map[string]Delays{
		"engine": {
			"retry":                 {5, 10},
			"thread_start_cooldown": {5, 5},
		},
		"signup": {
			"after_seon":               {1, 3},
			"after_pow":                {1, 2},
			"after_register":           {20, 30},
			"onboarding_step":          {2, 9},
			"after_accept_buyer_terms": {2, 5},
			"after_update_country":     {2, 5},
			"after_follow_pokemons":    {2, 5},
			"after_update_username":    {160, 200},
			"after_set_phone_number":   {2, 5},
			"after_apply_referral":     {2, 5},
			"post_onboarding_step":     {3, 8},
		},
		"orders": {
			"after_shipping_address":   {3, 8},
			"after_payment_method":     {3, 8},
			"before_checkout_basket":   {3, 8},
			"before_place_order":       {5, 10},
			"between_checkout_follows": {2, 5},
			"after_follow_seller":      {60, 60},
			"between_basket_orders":    {2, 5},
			"after_order":              {5, 12},
		},
		"bulk": {
			"bulk_checkout_between_rows":  {0, 0},
			"bulk_review_between_reviews": {0, 0},
		},
		"reviews": {
			"before_review_username_update": {1, 3},
			"after_review_username_update":  {1, 3},
			"after_review_feed_lookup":      {1, 3},
			"between_review_follows":        {2, 5},
			"before_review_submit":          {2, 5},
			"after_review_submit":           {2, 5},
			"between_review_submissions":    {30, 90},
			"review_preflight_follow":       {2, 5},
		},
	}
}

func ensureDelayDefaults(path string, raw []byte) ([]byte, error) {
	existing, err := parseDelayBytes(raw)
	if err != nil {
		return nil, err
	}
	defaults := defaultDelays()
	missing := false
	for name := range defaults {
		if _, ok := existing[name]; !ok {
			missing = true
			break
		}
	}
	if isGroupedDelayConfig(raw) && !missing {
		return raw, nil
	}
	grouped := defaultDelaySections()
	for section, delays := range grouped {
		for name, value := range delays {
			if existingValue, ok := existing[name]; ok {
				grouped[section][name] = existingValue
			} else {
				grouped[section][name] = value
			}
		}
	}
	for name, value := range existing {
		if _, ok := defaults[name]; ok {
			continue
		}
		if grouped["custom"] == nil {
			grouped["custom"] = Delays{}
		}
		grouped["custom"][name] = value
	}
	return writeDelayConfigSafely(path, raw, grouped)
}

func parseDelayBytes(raw []byte) (Delays, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	delays := Delays{}
	for key, value := range root {
		if parsed, ok := parseDelayRange(value); ok {
			delays[key] = parsed
			continue
		}
		var section map[string]json.RawMessage
		if err := json.Unmarshal(value, &section); err != nil {
			continue
		}
		for name, sectionValue := range section {
			if parsed, ok := parseDelayRange(sectionValue); ok {
				delays[name] = parsed
			}
		}
	}
	return delays, nil
}

func parseDelayRange(raw json.RawMessage) ([]float64, bool) {
	var values []float64
	if err := json.Unmarshal(raw, &values); err != nil || len(values) == 0 {
		return nil, false
	}
	return values, true
}

func isGroupedDelayConfig(raw []byte) bool {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return false
	}
	if len(root) == 0 {
		return false
	}
	for _, value := range root {
		if _, ok := parseDelayRange(value); ok {
			return false
		}
	}
	return true
}

func writeDelayConfigSafely(path string, oldRaw []byte, value any) ([]byte, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(oldRaw) > 0 && bytes.Equal(bytes.TrimSpace(oldRaw), bytes.TrimSpace(raw)) {
		return oldRaw, nil
	}
	if len(oldRaw) == 0 {
		oldRaw = []byte("{}\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	if err := writeConfigSafely(path, oldRaw, raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *EngineConfig) normalize() {
	c.DiscordWebhookURL = strings.TrimSpace(c.DiscordWebhookURL)
	if c.DiscordWebhookURL == "" {
		c.DiscordWebhookURL = strings.TrimSpace(c.DiscordWebhookURL)
	}
	if strings.TrimSpace(c.Timezone) == "" {
		c.Timezone = "Europe/London"
	}
	if strings.TrimSpace(c.ProxyFile) == "" {
		c.ProxyFile = "setup/proxies.txt"
	}
	if strings.TrimSpace(c.ProfilesCSVPath) == "" {
		c.ProfilesCSVPath = "setup/profiles.csv"
	}
	if strings.TrimSpace(c.CardsCSVPath) == "" {
		c.CardsCSVPath = "setup/cards.csv"
	}
	if strings.TrimSpace(c.FriendCodesPath) == "" {
		c.FriendCodesPath = "setup/friend_codes.csv"
	}
	if strings.TrimSpace(c.BasketsJSONPath) == "" {
		c.BasketsJSONPath = "setup/baskets.json"
	}
	if strings.TrimSpace(c.TaskWindowStart) == "" {
		c.TaskWindowStart = "00:00"
	}
	if strings.TrimSpace(c.TaskWindowEnd) == "" {
		c.TaskWindowEnd = "23:59"
	}
	if strings.TrimSpace(c.OrderCheckWindowStart) == "" {
		c.OrderCheckWindowStart = "16:00"
	}
	if strings.TrimSpace(c.OrderCheckWindowEnd) == "" {
		c.OrderCheckWindowEnd = "22:00"
	}
	if c.MasterCandidatePercentage <= 0 {
		c.MasterCandidatePercentage = 1
	}
	if c.MasterBaseSignupCap <= 0 {
		c.MasterBaseSignupCap = 100
	}
	if c.MasterCapRandomizationPct <= 0 {
		c.MasterCapRandomizationPct = 20
	}
	if c.MasterAgingDurationHours <= 0 {
		c.MasterAgingDurationHours = 24 * 7
	}
	if c.MasterHealthCheckInterval.Duration <= 0 {
		c.MasterHealthCheckInterval.Duration = 4 * time.Hour
	}
	if c.ReviewTextChance <= 0 {
		c.ReviewTextChance = 15
	}
	if strings.TrimSpace(c.ReviewTextJSONPath) == "" {
		c.ReviewTextJSONPath = "setup/review_texts.json"
	}
	if c.ReviewPreflightMinFollows <= 0 {
		c.ReviewPreflightMinFollows = 10
	}
	if c.ReviewPreflightMaxFollows <= 0 {
		c.ReviewPreflightMaxFollows = 100
	}
	if c.ReviewPreflightMaxFollows < c.ReviewPreflightMinFollows {
		c.ReviewPreflightMinFollows, c.ReviewPreflightMaxFollows = c.ReviewPreflightMaxFollows, c.ReviewPreflightMinFollows
	}
	if c.ReviewUsernameAttempts <= 0 {
		c.ReviewUsernameAttempts = 3
	}
	if strings.TrimSpace(c.ReviewSellerFeedID) == "" {
		c.ReviewSellerFeedID = "CATEGORY_FEED_V2:TGl2ZXN0cmVhbVRhZ05vZGU6ODk5"
	}
	if strings.TrimSpace(c.ReviewSellerFeedFilter) == "" {
		c.ReviewSellerFeedFilter = "t__907"
	}
	if c.ReviewSellerFeedObjectSize <= 0 {
		c.ReviewSellerFeedObjectSize = 120
	}
	if c.ReviewSellerFeedMaxPages <= 0 {
		c.ReviewSellerFeedMaxPages = 15
	}
	if c.CheckoutPreorderMinFollows < 0 {
		c.CheckoutPreorderMinFollows = 0
	}
	if c.CheckoutPreorderMaxFollows < 0 {
		c.CheckoutPreorderMaxFollows = 0
	}
	if c.CheckoutPreorderMinFollows == 0 && c.CheckoutPreorderMaxFollows == 0 {
		c.CheckoutPreorderMinFollows = 2
		c.CheckoutPreorderMaxFollows = 5
	}
	if c.CheckoutPreorderMaxFollows < c.CheckoutPreorderMinFollows {
		c.CheckoutPreorderMinFollows, c.CheckoutPreorderMaxFollows = c.CheckoutPreorderMaxFollows, c.CheckoutPreorderMinFollows
	}
	if strings.TrimSpace(c.CheckoutLabelCSVPath) == "" {
		c.CheckoutLabelCSVPath = "checkouts_label_details.csv"
	}
	if c.AccountsPerRun <= 0 {
		c.AccountsPerRun = 1
	}
	if c.TaskConcurrency <= 0 {
		c.TaskConcurrency = 1
	}
	if c.StaleTaskAfter.Duration <= 0 {
		c.StaleTaskAfter.Duration = 30 * time.Minute
	}

	c.Storage.normalize()
}

func (c *StorageConfig) normalize() {
	if strings.TrimSpace(c.DatabasePath) == "" {
		c.DatabasePath = "storage/state.sqlite"
	}
	if strings.TrimSpace(c.AccountsPath) == "" {
		c.AccountsPath = "storage/accounts.json"
	}
	if strings.TrimSpace(c.OrdersPath) == "" {
		c.OrdersPath = "storage/orders.json"
	}
	if strings.TrimSpace(c.NormalSessionsPath) == "" {
		c.NormalSessionsPath = "storage/sessions/normal_sessions.csv"
	}
	if strings.TrimSpace(c.MasterSessionsPath) == "" {
		c.MasterSessionsPath = "storage/sessions/master_sessions.csv"
	}
	if strings.TrimSpace(c.MastersPath) == "" {
		c.MastersPath = "storage/masters.json"
	}
	if strings.TrimSpace(c.RuntimePath) == "" {
		c.RuntimePath = "storage/runtime.json"
	}
	if strings.TrimSpace(c.TaskQueuePath) == "" {
		c.TaskQueuePath = "storage/task_queue.json"
	}
	if strings.TrimSpace(c.PendingReviewsPath) == "" {
		c.PendingReviewsPath = "storage/pending_reviews.json"
	}
	if strings.TrimSpace(c.DiscoveryCachePath) == "" {
		c.DiscoveryCachePath = "storage/account_discovery_snapshots.json"
	}
	if strings.TrimSpace(c.ErrorDedupePath) == "" {
		c.ErrorDedupePath = "storage/error_webhook_dedupe.json"
	}
}

func (c *StorageConfig) NormalizeForStore() {
	c.normalize()
}

func (c *Config) PrimarySMSService() string {
	if len(c.SMSService) == 0 {
		return "smspool"
	}
	return strings.ToLower(c.SMSService[0])
}

func resolvePath() (string, error) {
	candidates := []string{
		filepath.Join("setup", "config.json"),
		"config.json",
	}

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "setup", "config.json"),
			filepath.Join(exeDir, "config.json"),
		)
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("setup/config.json not found")
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func formatClock(hour int, minute int) string {
	if hour < 0 {
		hour = 0
	}
	if hour > 23 {
		hour = 23
	}
	if minute < 0 {
		minute = 0
	}
	if minute > 59 {
		minute = 59
	}
	return fmt.Sprintf("%02d:%02d", hour, minute)
}
