package whtnt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"

	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"

	"github.com/google/uuid"
	"status403.com/rsavv/seonsdk-go/devices"

	"rnldo.dev/wnshell/internal/logger"
)

const (
	eventsEndpoint      = "https://api.whatnot.com/events/v1/b"
	eventsWriteKey      = "j3vwvmys3Zpedi0rvXAD4AEOVcOpew0s"
	eventsUserAgent     = "analytics-ios/1.4.7"
	eventsLibraryName   = "analytics-swift"
	eventsLibraryVer    = "1.4.7"
	eventsAppNamespace  = "com.whatnot"
	eventsAppName       = "Whatnot"
	eventsEventSource   = "iOS"
	eventsFormatVersion = 2
)

// EventsClient is a standalone Segment analytics client that mirrors the
// behaviour of the Whatnot iOS app's analytics-swift SDK. Initialize it once
// per registration attempt with NewEventsClient, then call the Send* methods
// at the appropriate flow points. Every method is fire-and-forget: failures
// are logged but never propagated.
type EventsClient struct {
	log    *logger.Logger
	http   tls_client.HttpClient
	device *devices.Device

	appVersion string
	appBuild   string

	anonymousID    string
	instanceID     string
	coldInstanceID string
	iosStableID    string
	appsFlyerID    string
	userSessionID  string
	idfv           string
	webAppsid      string
	webStableId    string

	appsid        string
	userID        string
	username      string
	authenticated bool

	locale         string
	acceptLanguage string
	timezone       string
	machine        string
	iosVersion     string
	webkitUA       string
	screenW        int
	screenH        int
	trackingDenied bool
}

// NewEventsClient creates a ready-to-use analytics client from the current
// device and HTTP session. All session-scoped UUIDs are generated here.
func NewEventsClient(log *logger.Logger, httpClient tls_client.HttpClient, dev *devices.Device, appVersion, appBuild string) *EventsClient {
	if dev == nil || httpClient == nil {
		return nil
	}

	machine := dev.Machine()
	iosVersion := dev.IosVersion()
	loc := dev.Locale()
	idfv := strings.ToUpper(dev.DeviceUdid())
	now := time.Now()

	trackingDenied := true
	idfa := strings.ToUpper(dev.DeviceAdid())
	if idfa != "" && idfa != "00000000-0000-0000-0000-000000000000" {
		trackingDenied = false
	}

	screenW, screenH := 393, 852
	if spec := devices.LookupSpec(machine); spec != nil && spec.ScreenWidth > 0 {
		scale := 3
		if defs := devices.BuildDefaults(machine, iosVersion); defs != nil && defs.ScreenScale > 0 {
			scale = defs.ScreenScale
		}
		screenW = int(spec.ScreenWidth) / scale
		screenH = int(spec.ScreenHeight) / scale
	}

	localeStr := loc.RegionLanguage + "-" + loc.RegionCountry
	acceptLang := loc.RegionLanguage + "-" + loc.RegionCountry + "," + loc.RegionLanguage + ";q=0.9"

	iosUA := buildWebKitUserAgent(iosVersion)

	c := &EventsClient{
		log:    log,
		http:   httpClient,
		device: dev,

		appVersion: appVersion,
		appBuild:   appBuild,

		anonymousID:    strings.ToUpper(uuid.NewString()),
		instanceID:     strings.ToUpper(uuid.NewString()),
		coldInstanceID: strings.ToUpper(uuid.NewString()),
		iosStableID:    strings.ToUpper(uuid.NewString()),
		appsFlyerID:    fmt.Sprintf("%d-%d", now.UnixMilli(), 1000000+rand.Intn(9000000)),
		userSessionID:  strings.ToUpper(uuid.NewString()),
		idfv:           idfv,
		webAppsid:      uuid.NewString(),
		webStableId:    uuid.NewString(),

		locale:         localeStr,
		acceptLanguage: acceptLang,
		timezone:       loc.TimezoneIdentifier,
		machine:        machine,
		iosVersion:     iosVersion,
		webkitUA:       iosUA,
		screenW:        screenW,
		screenH:        screenH,
		trackingDenied: trackingDenied,
	}

	log.Events("Initialized analytics session")
	return c
}

// SetAuthenticated marks the client as logged-in with the data from the
// register response. Call this right after storeSessionCookies.
func (c *EventsClient) SetAuthenticated(userID int, appsid string) {
	if c == nil {
		return
	}
	c.authenticated = true
	c.userID = strconv.Itoa(userID)
	c.appsid = appsid
}

// IosStableID returns the Segment SDK's persisted stable ID for this session.
func (c *EventsClient) IosStableID() string {
	if c == nil {
		return ""
	}
	return c.iosStableID
}

// SetUsername records the username so subsequent identify events include it.
func (c *EventsClient) SetUsername(username string) {
	if c == nil {
		return
	}
	c.username = username
}

// ── Segment payload types ──

type segmentBatchPayload struct {
	Batch    []segmentEvent `json:"batch"`
	SentAt   string         `json:"sentAt"`
	WriteKey string         `json:"writeKey"`
}

type segmentEvent struct {
	AnonymousID  string         `json:"anonymousId"`
	Context      segmentContext `json:"context"`
	Event        string         `json:"event,omitempty"`
	Integrations map[string]any `json:"integrations"`
	MessageID    string         `json:"messageId"`
	Properties   map[string]any `json:"properties,omitempty"`
	Timestamp    string         `json:"timestamp"`
	Traits       map[string]any `json:"traits,omitempty"`
	Type         string         `json:"type"`
	UserID       string         `json:"userId,omitempty"`
	Metadata     segmentMeta    `json:"_metadata"`
}

type segmentContext struct {
	App         segmentApp     `json:"app"`
	AppsFlyerID string         `json:"appsFlyerId,omitempty"`
	Device      segmentDevice  `json:"device"`
	InstanceID  string         `json:"instanceId"`
	IosStableID string         `json:"iosStableId,omitempty"`
	Library     segmentLibrary `json:"library"`
	Locale      string         `json:"locale"`
	Network     segmentNetwork `json:"network"`
	OS          segmentOS      `json:"os"`
	Screen      segmentScreen  `json:"screen"`
	Timezone    string         `json:"timezone"`
	Traits      map[string]any `json:"traits,omitempty"`
	UserAgent   string         `json:"userAgent"`
	WebAppsid   string         `json:"webAppsid,omitempty"`
	WebStableId string         `json:"webStableId,omitempty"`
}

type segmentApp struct {
	Build     string `json:"build"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   string `json:"version"`
}

type segmentDevice struct {
	AdTrackingEnabled bool   `json:"adTrackingEnabled"`
	AdvertisingID     string `json:"advertisingId"`
	DeviceUniqueID    string `json:"deviceUniqueId,omitempty"`
	ID                string `json:"id"`
	Manufacturer      string `json:"manufacturer"`
	Model             string `json:"model"`
	Name              string `json:"name"`
	TrackingStatus    string `json:"trackingStatus"`
	Type              string `json:"type"`
}

type segmentLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type segmentNetwork struct {
	Bluetooth bool `json:"bluetooth"`
	Cellular  bool `json:"cellular"`
	Wifi      bool `json:"wifi"`
}

type segmentOS struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type segmentScreen struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type segmentMeta struct {
	Bundled    []string `json:"bundled"`
	BundledIDs []string `json:"bundledIds"`
	Unbundled  []string `json:"unbundled"`
}

// ── Context builders ──

func (c *EventsClient) context(cold bool) segmentContext {
	trackingStatus := "denied"
	adTracking := false
	if !c.trackingDenied {
		trackingStatus = "authorized"
		adTracking = true
	}

	deviceID := c.idfv
	if !c.trackingDenied {
		deviceID = strings.ToUpper(c.device.DeviceAdid())
	}

	ctx := segmentContext{
		App: segmentApp{
			Build:     c.appBuild,
			Name:      eventsAppName,
			Namespace: eventsAppNamespace,
			Version:   c.appVersion,
		},
		Device: segmentDevice{
			AdTrackingEnabled: adTracking,
			AdvertisingID:     deviceID,
			ID:                deviceID,
			Manufacturer:      "Apple",
			Model:             c.machine,
			Name:              "iPhone",
			TrackingStatus:    trackingStatus,
			Type:              "ios",
		},
		InstanceID: c.instanceID,
		Library:    segmentLibrary{Name: eventsLibraryName, Version: eventsLibraryVer},
		Locale:     c.locale,
		Network:    segmentNetwork{Bluetooth: false, Cellular: false, Wifi: true},
		OS:         segmentOS{Name: "iOS", Version: c.iosVersion},
		Screen:     segmentScreen{Width: c.screenW, Height: c.screenH},
		Timezone:   c.timezone,
		UserAgent:  c.webkitUA,
	}
	if cold {
		ctx.InstanceID = c.coldInstanceID
	} else {
		ctx.Device.DeviceUniqueID = c.idfv
		ctx.AppsFlyerID = c.appsFlyerID
		ctx.IosStableID = c.iosStableID
		ctx.WebAppsid = c.webAppsid
		ctx.WebStableId = c.webStableId
		ctx.Traits = c.contextTraits()
	}
	return ctx
}

func (c *EventsClient) contextTraits() map[string]any {
	t := map[string]any{
		"has_access_token":    c.authenticated,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   c.authenticated,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
	}
	if c.authenticated {
		country := "GB"
		if c.device != nil {
			country = c.device.Locale().RegionCountry
		}
		t["contentSizeCategory"] = "UICTContentSizeCategoryL"
		t["is_employee"] = "false"
		t["seller_util_opt_in"] = "false"
		t["clock_drift_ms"] = rand.Intn(40) - 20
		t["address"] = map[string]any{"country": country}
		t["crash_reporting_segments"] = []string{"NEW_USER_WITHOUT_PURCHASE"}
		if c.username != "" {
			t["username"] = c.username
		}
	}
	return t
}

func (c *EventsClient) identifyTraits() map[string]any {
	if c.authenticated {
		return map[string]any{
			"has_access_token":    true,
			"app_state":           "foreground",
			"build_configuration": "production",
			"has_refresh_token":   true,
			"device_model":        c.machine,
			"user_session_id":     c.userSessionID,
			"impersonator_id":     nil,
			"contentSizeCategory": "UICTContentSizeCategoryL",
		}
	}
	return map[string]any{
		"has_access_token":    false,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   false,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
	}
}

func newMeta() segmentMeta {
	return segmentMeta{
		Bundled:    []string{"AppsFlyer", "Firebase"},
		BundledIDs: []string{},
		Unbundled:  []string{"AWS S3", "Amazon Kinesis", "Amazon Kinesis Firehose"},
	}
}

func tsMillis(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }
func tsNanos(t time.Time) string  { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// ── Base event helpers ──

func (c *EventsClient) base(eventType string) segmentEvent {
	ev := segmentEvent{
		AnonymousID:  c.anonymousID,
		Context:      c.context(false),
		Integrations: map[string]any{},
		MessageID:    strings.ToUpper(uuid.NewString()),
		Timestamp:    tsMillis(time.Now()),
		Type:         eventType,
		Metadata:     newMeta(),
	}
	if c.authenticated {
		ev.UserID = c.userID
	}
	return ev
}

func (c *EventsClient) baseWithContextTraits(eventType string, ctxTraits map[string]any) segmentEvent {
	ctx := c.context(false)
	ctx.Traits = ctxTraits
	ev := segmentEvent{
		AnonymousID:  c.anonymousID,
		Context:      ctx,
		Integrations: map[string]any{},
		MessageID:    strings.ToUpper(uuid.NewString()),
		Timestamp:    tsMillis(time.Now()),
		Type:         eventType,
		Metadata:     newMeta(),
	}
	if c.authenticated {
		ev.UserID = c.userID
	}
	return ev
}

func (c *EventsClient) header(eventName, componentName string) map[string]any {
	h := map[string]any{
		"aux_json":        "",
		"event_name":      eventName,
		"user_session_id": c.userSessionID,
		"component_name":  componentName,
		"event_source":    eventsEventSource,
		"event_timestamp": tsNanos(time.Now()),
	}
	if c.authenticated && c.appsid != "" {
		h["appsid"] = c.appsid
	}
	return h
}

// ── Individual event builders ──

func (c *EventsClient) identify() segmentEvent {
	ctx := c.context(false)
	ctx.Traits = nil
	ev := segmentEvent{
		AnonymousID:  c.anonymousID,
		Context:      ctx,
		Integrations: map[string]any{},
		MessageID:    strings.ToUpper(uuid.NewString()),
		Timestamp:    tsMillis(time.Now()),
		Type:         "identify",
		Metadata:     newMeta(),
		Traits:       c.identifyTraits(),
	}
	if c.authenticated {
		ev.UserID = c.userID
	}
	return ev
}

func (c *EventsClient) identifyWithTraits(traits map[string]any) segmentEvent {
	ctx := c.context(false)
	ctx.Traits = nil
	ev := segmentEvent{
		AnonymousID:  c.anonymousID,
		Context:      ctx,
		Integrations: map[string]any{},
		MessageID:    strings.ToUpper(uuid.NewString()),
		Timestamp:    tsMillis(time.Now()),
		Type:         "identify",
		Metadata:     newMeta(),
		Traits:       traits,
	}
	if c.authenticated {
		ev.UserID = c.userID
	}
	return ev
}

func (c *EventsClient) coldIdentify() segmentEvent {
	return segmentEvent{
		AnonymousID:  c.anonymousID,
		Context:      c.context(true),
		Integrations: map[string]any{},
		MessageID:    strings.ToUpper(uuid.NewString()),
		Timestamp:    tsMillis(time.Now()),
		Type:         "identify",
		Metadata:     newMeta(),
		Traits: map[string]any{
			"has_access_token":    false,
			"app_state":           "background",
			"build_configuration": "production",
			"has_refresh_token":   false,
			"device_model":        c.machine,
			"user_session_id":     c.userSessionID,
			"impersonator_id":     nil,
		},
	}
}

func (c *EventsClient) appBackgrounded() segmentEvent {
	ev := c.base("track")
	ev.Event = "Application Backgrounded"
	return ev
}

func (c *EventsClient) appForegrounded() segmentEvent {
	ev := c.base("track")
	ev.Event = "Application Foregrounded"
	return ev
}

func (c *EventsClient) performanceAppLaunch() segmentEvent {
	ev := c.base("track")
	ev.Event = "performance_v2"

	total := 800.0 + rand.Float64()*1200.0
	binaryLoad := 120.0 + rand.Float64()*120.0
	sceneForeground := 20.0 + rand.Float64()*30.0
	authCheck := 0.5 + rand.Float64()*3.0
	transit := total - binaryLoad - sceneForeground - authCheck

	ev.Properties = map[string]any{
		"header": c.header("performance", "app_launch"),
		"performance": map[string]any{
			"app_launch": map[string]any{
				"is_ios_prewarming":                  false,
				"was_pre_launched_by_notification":   false,
				"opened_from_notification":           false,
				"status":                             "SUCCESS",
				"destination":                        "LOGIN",
				"total_duration_ms":                  total,
				"total_duration_excl_binary_load_ms": fmt.Sprintf("%d", int(total-binaryLoad)),
				"spans": []map[string]any{
					{"name": "transit_to_login", "duration": transit},
					{"name": "binary_load", "duration": binaryLoad},
					{"name": "scene_will_enter_foreground", "duration": sceneForeground},
					{"name": "auth_token_check", "duration": authCheck},
				},
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) authPasskeyResult(trigger, result string) segmentEvent {
	ev := c.base("track")
	ev.Event = "authentication_v2"
	ev.Properties = map[string]any{
		"header": c.header("authentication", "passkey_authentication_result"),
		"authentication": map[string]any{
			"passkey_authentication_result": map[string]any{
				"trigger": trigger,
				"result":  result,
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) authSignup(method, status string) segmentEvent {
	ev := c.base("track")
	ev.Event = "authentication_v2"
	ev.Properties = map[string]any{
		"header": c.header("authentication", "signup"),
		"authentication": map[string]any{
			"signup": map[string]any{
				"method": method,
				"status": status,
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) authPasskeySetupShown() segmentEvent {
	ev := c.base("track")
	ev.Event = "authentication_v2"
	ev.Properties = map[string]any{
		"header": c.header("authentication", "passkey_setup_shown"),
		"authentication": map[string]any{
			"passkey_setup_shown": map[string]any{
				"trigger": "ONBOARDING_PASSKEY_SETUP",
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) authPasskeySetupAction() segmentEvent {
	ev := c.base("track")
	ev.Event = "authentication_v2"
	ev.Properties = map[string]any{
		"header": c.header("authentication", "passkey_setup_action_taken"),
		"authentication": map[string]any{
			"passkey_setup_action_taken": map[string]any{
				"action":  "SKIP",
				"trigger": "ONBOARDING_PASSKEY_SETUP",
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) viewOnboardingStep(page string, start time.Time) segmentEvent {
	ev := c.base("track")
	ev.Event = "view_v2"
	end := time.Now()
	ev.Properties = map[string]any{
		"header": c.header("view", "onboarding_step_view"),
		"view": map[string]any{
			"onboarding_step_view": map[string]any{
				"page":                     page,
				"start_time":               tsNanos(start),
				"outcome":                  "COMPLETE",
				"view_time_ms":             fmt.Sprintf("%d", end.Sub(start).Milliseconds()),
				"how_it_works_step_number": "0",
				"end_time":                 tsNanos(end),
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) viewOnboardingPage(page string) segmentEvent {
	ev := c.base("track")
	ev.Event = "view_v2"
	ev.Properties = map[string]any{
		"header": c.header("view", "onboarding_page"),
		"view": map[string]any{
			"onboarding_page": map[string]any{
				"page": page,
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) viewAutoSelectedUsername(username string) segmentEvent {
	ev := c.base("track")
	ev.Event = "view_v2"
	ev.Properties = map[string]any{
		"header": c.header("view", "auto_selected_username"),
		"view": map[string]any{
			"auto_selected_username": map[string]any{
				"username_source": "SYSTEM_SUGGESTION",
				"username":        username,
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) tap(component string, extra map[string]any) segmentEvent {
	ev := c.base("track")
	ev.Event = "tap_v2"
	body := map[string]any{}
	for k, v := range extra {
		body[k] = v
	}
	ev.Properties = map[string]any{
		"header":         c.header("tap", component),
		"tap":            map[string]any{component: body},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) lifecycleInstalledApps() segmentEvent {
	ev := c.base("track")
	ev.Event = "lifecycle_v2"
	ev.Properties = map[string]any{
		"header": c.header("lifecycle", "installed_business_apps_detected"),
		"lifecycle": map[string]any{
			"installed_business_apps_detected": map[string]any{
				"source":     "AUTHENTICATION",
				"is_present": false,
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) lifecycleContactBook(status string) segmentEvent {
	ev := c.base("track")
	ev.Event = "lifecycle_v2"
	ev.Properties = map[string]any{
		"header":         c.header("lifecycle", "device_contact_book_status"),
		"lifecycle":      map[string]any{"device_contact_book_status": map[string]any{"status": status}},
		"format_version": eventsFormatVersion,
	}
	return ev
}

func (c *EventsClient) impressionOnboardingOption(label, entityID string, index int) segmentEvent {
	ev := c.base("track")
	ev.Event = "impression_v2"
	ev.Properties = map[string]any{
		"header": c.header("impression", "onboarding_option"),
		"impression": map[string]any{
			"onboarding_option": map[string]any{
				"entity_index": index,
				"trigger":      "SIGNUP_NUX",
				"page":         "ONBOARDING_INTEREST_PICKER",
				"label":        label,
				"id":           entityID,
			},
		},
		"format_version": eventsFormatVersion,
	}
	return ev
}

// ── Transport ──

func (c *EventsClient) send(events []segmentEvent) {
	if c == nil || len(events) == 0 {
		return
	}

	payload := segmentBatchPayload{
		Batch:    events,
		SentAt:   tsMillis(time.Now()),
		WriteKey: eventsWriteKey,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		c.log.Events("Failed to marshal batch: " + err.Error())
		return
	}

	req, reqErr := http.NewRequest("POST", eventsEndpoint, bytes.NewBuffer(raw))
	if reqErr != nil {
		c.log.Events("Failed to create event request: " + reqErr.Error())
		return
	}
	req.Header = http.Header{
		"content-type":                 {"application/json; charset=utf-8"},
		"accept":                       {"*/*"},
		"priority":                     {"u=3"},
		"upload-draft-interop-version": {"6"},
		"accept-encoding":              {"gzip"},
		"accept-language":              {c.acceptLanguage},
		"user-agent":                   {eventsUserAgent},
		"upload-complete":              {"?1"},
		"x-whatnot-sent-at":            {strconv.FormatInt(time.Now().UnixNano(), 10)},
		http.HeaderOrderKey: {
			"content-type",
			"accept",
			"priority",
			"upload-draft-interop-version",
			"accept-encoding",
			"accept-language",
			"content-length",
			"user-agent",
			"upload-complete",
			"x-whatnot-sent-at",
		},
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.log.Events(fmt.Sprintf("Failed to send batch of %d events: %s", len(events), err.Error()))
		return
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	c.log.Events(fmt.Sprintf("Sent batch of %d events", len(events)))
}

// ── Public flow methods ──
// Each method composes a realistic batch matching the real app's behaviour
// at that point in the registration/onboarding flow.

// SendAppLaunch sends the initial cold-launch batch (cold identify + app backgrounded).
func (c *EventsClient) SendAppLaunch() {
	if c == nil {
		return
	}
	c.log.Events("Sending app launch events...")
	c.send([]segmentEvent{
		c.coldIdentify(),
		c.appBackgrounded(),
	})
}

// SendAppForeground sends performance + passkey cancel + foreground + cover view.
func (c *EventsClient) SendAppForeground() {
	if c == nil {
		return
	}
	c.log.Events("Sending app foreground events...")
	coverStart := time.Now()
	c.send([]segmentEvent{
		c.performanceAppLaunch(),
		c.authPasskeyResult("PASSKEY_AUTHENTICATION_TRIGGER_MANUAL", "PASSKEY_AUTHENTICATION_RESULT_STATUS_USER_CANCELED"),
		c.appForegrounded(),
		c.viewOnboardingStep("ONBOARDING_COVER", coverStart),
	})
}

// SendPreRegistration sends the sign-up tap + signup page view + auth initiation.
func (c *EventsClient) SendPreRegistration() {
	if c == nil {
		return
	}
	c.log.Events("Sending pre-registration events...")
	c.send([]segmentEvent{
		c.tap("onboarding_sign_up_tap", map[string]any{"page": "ONBOARDING_COVER"}),
		c.viewOnboardingPage("ONBOARDING_SIGNUP"),
		c.tap("onboarding_sign_up_tap", map[string]any{"page": "ONBOARDING_SIGNUP"}),
		c.authSignup("EMAIL", "INITIATED"),
	})
}

// SendPostRegistration sends the large post-registration batch with identify
// events, auth success, lifecycle bindings, and push notification status.
// Mirrors rrr.har entries 11-12: progressive identify events with evolving
// trait sets as the SDK picks up session/username/employee state.
// Track events keep the pre-auth context.traits (has_access_token=false) because
// the Segment SDK updates context traits asynchronously from identify calls.
func (c *EventsClient) SendPostRegistration() {
	if c == nil {
		return
	}
	c.log.Events("Sending post-registration events...")

	fcm := eventsRandomHex(40)
	apns := eventsRandomHex(40)

	preAuthCtxTraits := map[string]any{
		"has_access_token":    false,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   false,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
	}

	baseIdentifyTraits := map[string]any{
		"has_access_token":    true,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   true,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
		"contentSizeCategory": "UICTContentSizeCategoryL",
	}

	sessionIdentifyTraits := map[string]any{
		"session_id":       c.appsid,
		"statsigCustomIDs": []string{"stableID", c.iosStableID},
		"username":         c.username,
	}

	mergedIdentifyTraits := map[string]any{
		"has_access_token":    true,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   true,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
		"contentSizeCategory": "UICTContentSizeCategoryL",
		"username":            c.username,
	}

	fullIdentifyTraits := map[string]any{
		"has_access_token":    true,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   true,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
		"contentSizeCategory": "UICTContentSizeCategoryL",
		"username":            c.username,
		"is_employee":         false,
	}

	laterIdentifyTraits := map[string]any{
		"has_access_token":    true,
		"app_state":           "foreground",
		"build_configuration": "production",
		"has_refresh_token":   true,
		"device_model":        c.machine,
		"user_session_id":     c.userSessionID,
		"impersonator_id":     nil,
	}

	authSwitcher := c.baseWithContextTraits("track", preAuthCtxTraits)
	authSwitcher.Event = "authentication_v2"
	authSwitcher.Properties = map[string]any{
		"header": c.header("authentication", "account_switcher_action"),
		"authentication": map[string]any{
			"account_switcher_action": map[string]any{"action": "SIGN_UP"},
		},
		"format_version": eventsFormatVersion,
	}

	authSuccess := c.baseWithContextTraits("track", preAuthCtxTraits)
	authSuccess.Event = "authentication_v2"
	authSuccess.Properties = map[string]any{
		"header": c.header("authentication", "signup"),
		"authentication": map[string]any{
			"signup": map[string]any{"method": "EMAIL", "status": "SUCCESS"},
		},
		"format_version": eventsFormatVersion,
	}

	idBinding := c.baseWithContextTraits("track", preAuthCtxTraits)
	idBinding.Event = "lifecycle_v2"
	idBinding.Properties = map[string]any{
		"header":         c.header("lifecycle", "logged_out_user_idbinding_frontend"),
		"lifecycle":      map[string]any{"logged_out_user_idbinding_frontend": map[string]any{}},
		"format_version": eventsFormatVersion,
	}

	pushStatus := c.baseWithContextTraits("track", preAuthCtxTraits)
	pushStatus.Event = "lifecycle_v2"
	pushStatus.Properties = map[string]any{
		"header": c.header("lifecycle", "device_push_notifications_status"),
		"lifecycle": map[string]any{
			"device_push_notifications_status": map[string]any{
				"fcm_token": fcm, "source": "AUTHENTICATION", "apns_token": apns, "status": "AUTHORIZED",
			},
		},
		"format_version": eventsFormatVersion,
	}

	c.send([]segmentEvent{
		authSwitcher,
		c.identifyWithTraits(baseIdentifyTraits),
		authSuccess,
		idBinding,
		c.identifyWithTraits(sessionIdentifyTraits),
		c.identifyWithTraits(mergedIdentifyTraits),
		c.identifyWithTraits(fullIdentifyTraits),
		c.identifyWithTraits(nil),
		pushStatus,
		c.identifyWithTraits(laterIdentifyTraits),
		c.identifyWithTraits(laterIdentifyTraits),
		c.identifyWithTraits(laterIdentifyTraits),
		c.identifyWithTraits(laterIdentifyTraits),
		c.identifyWithTraits(laterIdentifyTraits),
	})
}

// SendAcceptTerms sends events after the buyer terms acceptance step.
func (c *EventsClient) SendAcceptTerms(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending accept terms events...")
	c.send([]segmentEvent{
		c.viewOnboardingStep("ONBOARDING_TERMS", stepStart),
		c.lifecycleInstalledApps(),
		c.identify(),
		c.identify(),
	})
}

// SendCountryStep sends events for the country selection step.
func (c *EventsClient) SendCountryStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending country step events...")
	c.send([]segmentEvent{
		c.tap("onboarding_country_continue_tap", map[string]any{"page": "ONBOARDING_COUNTRY_PICKER"}),
		c.viewOnboardingStep("ONBOARDING_COUNTRY_PICKER", stepStart),
		c.viewOnboardingPage("ONBOARDING_PHONE_VERIFICATION"),
	})
}

// SendPhoneStep sends events for the phone verification step.
func (c *EventsClient) SendPhoneStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending phone step events...")
	c.send([]segmentEvent{
		c.tap("onboarding_phone_verification_send_code_tap", map[string]any{"page": "ONBOARDING_PHONE_VERIFICATION"}),
		c.viewOnboardingStep("ONBOARDING_PHONE_VERIFICATION", stepStart),
		c.viewOnboardingPage("ONBOARDING_PHONE_CODE_ENTRY"),
	})
}

// SendPasskeyStep sends passkey setup shown + skip events.
func (c *EventsClient) SendPasskeyStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending passkey step events...")
	c.send([]segmentEvent{
		c.authPasskeySetupShown(),
		c.viewOnboardingStep("ONBOARDING_PASSKEY_SETUP", stepStart),
		c.authPasskeySetupAction(),
	})
}

// SendUsernameStep sends events for the username selection step.
func (c *EventsClient) SendUsernameStep(username string, stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending username step events...")
	c.send([]segmentEvent{
		c.viewOnboardingStep("ONBOARDING_USERNAME", stepStart),
		c.viewOnboardingPage("ONBOARDING_USERNAME"),
		c.viewAutoSelectedUsername(username),
		c.tap("onboarding_username_continue_tap", map[string]any{"page": "ONBOARDING_USERNAME"}),
	})
}

// SendContactSyncStep sends events for the contact sync + gender selection steps.
func (c *EventsClient) SendContactSyncStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending contact sync step events...")
	c.send([]segmentEvent{
		c.viewOnboardingStep("ONBOARDING_CONTACTS_SYNC", stepStart),
		c.tap("onboarding_contact_sync_pre_prompt", map[string]any{"action": "SKIP"}),
		c.viewOnboardingStep("ONBOARDING_GENDER_SELECTION", stepStart),
		c.viewOnboardingPage("ONBOARDING_GENDER_SELECTION"),
		c.tap("onboarding_gender_selection_button", map[string]any{"selection": "PREFER_NOT_TO_SAY"}),
	})
}

// SendInterestPickerStep sends impression events for each visible category
// plus the selection tap and next-button tap.
func (c *EventsClient) SendInterestPickerStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending interest picker events...")

	categories := []struct {
		Label    string
		EntityID string
	}{
		{"Pokémon Cards", "TGl2ZXN0cmVhbVRhZ05vZGU6OTA3"},
		{"Sports Cards", "TGl2ZXN0cmVhbVRhZ05vZGU6MjAy"},
		{"Trading Card Games", "TGl2ZXN0cmVhbVRhZ05vZGU6ODk5"},
		{"Electronics", "TGl2ZXN0cmVhbVRhZ05vZGU6OTQy"},
		{"Sneakers & Streetwear", "TGl2ZXN0cmVhbVRhZ05vZGU6MTMz"},
		{"Coins & Money", "TGl2ZXN0cmVhbVRhZ05vZGU6ODcx"},
		{"Toys", "TGl2ZXN0cmVhbVRhZ05vZGU6Njc3"},
		{"Health & Beauty", "TGl2ZXN0cmVhbVRhZ05vZGU6OTIy"},
		{"Women's Fashion", "TGl2ZXN0cmVhbVRhZ05vZGU6MTUw"},
		{"Men's Fashion", "TGl2ZXN0cmVhbVRhZ05vZGU6MTQ5"},
		{"Jewelry", "TGl2ZXN0cmVhbVRhZ05vZGU6OTQ0"},
		{"Food", "TGl2ZXN0cmVhbVRhZ05vZGU6OTYx"},
	}

	batch := []segmentEvent{
		c.viewOnboardingStep("ONBOARDING_INTEREST_PICKER", stepStart),
		c.viewOnboardingPage("ONBOARDING_INTEREST_PICKER"),
	}
	for i, cat := range categories {
		batch = append(batch, c.impressionOnboardingOption(cat.Label, cat.EntityID, i))
	}
	batch = append(batch,
		c.tap("onboarding_option", map[string]any{
			"page":         "ONBOARDING_INTEREST_PICKER",
			"label":        "Pokémon Cards",
			"id":           pokemonCategoryID,
			"entity_index": 0,
			"is_selected":  true,
		}),
		c.tap("onboarding_next_button", map[string]any{"page": "ONBOARDING_INTEREST_PICKER"}),
		c.tap("onboarding_interests_returned_button", map[string]any{}),
	)
	c.send(batch)
}

// SendFollowSellersStep sends events for the follow-sellers onboarding step.
func (c *EventsClient) SendFollowSellersStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending follow sellers step events...")
	c.send([]segmentEvent{
		c.viewOnboardingStep("ONBOARDING_FOLLOW_TOP_SELLERS", stepStart),
		c.viewOnboardingStep("ONBOARDING_NOTIFICATIONS_PROMPT", stepStart),
		c.viewOnboardingPage("ONBOARDING_NOTIFICATIONS_PROMPT"),
	})
}

// SendOnboardingFinish sends the final onboarding-completion events.
func (c *EventsClient) SendOnboardingFinish(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending onboarding finish events...")
	c.send([]segmentEvent{
		c.viewOnboardingStep("ONBOARDING_COMPLETE", stepStart),
		c.tap("onboarding_finish", map[string]any{"page": "ONBOARDING_COMPLETE"}),
		c.viewOnboardingPage("ONBOARDING_COMPLETE"),
		c.viewOnboardingStep("ONBOARDING_COVER_VIEW", stepStart),
		c.lifecycleContactBook("NOT_DETERMINED"),
	})
}

// SendReferralStep sends events for the referral-credit reveal step.
func (c *EventsClient) SendReferralStep(stepStart time.Time) {
	if c == nil {
		return
	}
	c.log.Events("Sending referral step events...")
	c.send([]segmentEvent{
		c.viewOnboardingStep("ONBOARDING_REFERRAL_CREDIT_REVEAL", stepStart),
		c.viewOnboardingPage("ONBOARDING_REFERRAL_CREDIT_REVEAL"),
	})
}

func eventsRandomHex(length int) string {
	b := make([]byte, length/2+1)
	rand.Read(b)
	s := fmt.Sprintf("%x", b)
	if len(s) > length {
		return s[:length]
	}
	return s
}

// buildWebKitUserAgent returns the iOS WebKit mobile user agent string that
// the Segment Swift SDK reports. Format matches real device captures:
// "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
func buildWebKitUserAgent(iosVersion string) string {
	parts := strings.SplitN(iosVersion, ".", 3)
	major := parts[0]
	minor := "0"
	if len(parts) > 1 {
		minor = parts[1]
	}
	return fmt.Sprintf(
		"Mozilla/5.0 (iPhone; CPU iPhone OS %s_%s like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148",
		major, minor,
	)
}
