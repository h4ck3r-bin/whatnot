package seonsdk

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"status403.com/rsavv/seonsdk-go/crypto"
	"status403.com/rsavv/seonsdk-go/dc"
	"status403.com/rsavv/seonsdk-go/payload"
	"status403.com/rsavv/seonsdk-go/session"
	"status403.com/rsavv/seonsdk-go/version"
)

func randomCoreSeed() string {
	return strconv.FormatUint(uint64(rand.Uint32()), 10)
}

func Fingerprint(cfg FingerprintConfig) (string, error) {
	if cfg.SessionID == "" {
		cfg.SessionID = session.NewSessionID()
	}
	if cfg.CoreSeed == "" {
		cfg.CoreSeed = randomCoreSeed()
	}

	geoConfig := cfg.GeolocationConfig
	if geoConfig == nil {
		geoConfig = DefaultGeolocationConfig()
	}

	geoConfigMap := map[string]interface{}{
		"geolocationEnabled":          geoConfig.GeolocationEnabled,
		"geolocationServiceTimeoutMs": geoConfig.GeolocationServiceTimeoutMs,
		"maxGeoLocationCacheAgeSec":   geoConfig.MaxGeoLocationCacheAgeSec,
		"prefetchEnabled":             geoConfig.PrefetchEnabled,
	}

	localeMap := payload.BuildProbeMap(payload.KeysLocale, []interface{}{
		cfg.Locale.RegionTimezone,
		cfg.Locale.RegionLanguage,
		cfg.Locale.RegionCountry,
		cfg.Locale.TimezoneIdentifier,
		cfg.Locale.CalendarIdentifier,
	})

	deviceMap := payload.BuildProbeMap(payload.KeysDevice, []interface{}{
		cfg.Device.AccessoriesCount,
		cfg.Device.DeviceAdid,
		cfg.Device.DeviceOrientation,
		cfg.Device.DeviceUdid,
		cfg.Device.IosVersion,
		cfg.Device.PasteboardHash,
		cfg.Device.ScreenBrightness,
		cfg.Device.ScreenHeight,
		cfg.Device.ScreenWidth,
		cfg.Device.IcloudUbiquityToken,
		cfg.Device.IsScreenCaptured,
	})

	hardwareMap := payload.BuildProbeMap(payload.KeysHardware, []interface{}{
		cfg.Hardware.BatteryLevel,
		cfg.Hardware.BatteryCharging,
		cfg.Hardware.CpuType,
		cfg.Hardware.CpuCount,
		cfg.Hardware.IsEmulator,
		cfg.Hardware.PhysicalMemory,
		cfg.Hardware.SystemUptime,
	})

	mediaMap := payload.BuildProbeMap(payload.KeysMedia, []interface{}{
		cfg.Media.AudioMuteStatus,
		cfg.Media.AudioVolumeCurrent,
		cfg.Media.CanSendMail,
		cfg.Media.CanSendText,
	})

	networkMap := payload.BuildProbeMap(payload.KeysNetwork, []interface{}{
		cfg.Network.VpnState,
		cfg.Network.ProxyAddress,
		cfg.Network.ProxyState,
		cfg.Network.WifiSsid,
		cfg.Network.WifiMacAddress,
	})

	osMap := payload.BuildProbeMap(payload.KeysOS, []interface{}{
		cfg.OS.KernelVersion,
		cfg.OS.KernelName,
		cfg.OS.KernelArch,
		cfg.OS.IsJailbroken,
		cfg.OS.IsIosAppOnMac,
		cfg.OS.IsDebuggerAttached,
	})

	telephonyMap := payload.BuildProbeMap(payload.KeysTelephony, []interface{}{
		cfg.Telephony.NetworkConfig,
		cfg.Telephony.IsOnCall,
	})

	accessibilityMap := payload.BuildProbeMap(payload.KeysAccessibility, []interface{}{
		cfg.Accessibility.IsBoldTextEnabled,
		cfg.Accessibility.IsReduceMotionEnabled,
		cfg.Accessibility.IsVideoAutoplayEnabled,
		cfg.Accessibility.IsAssisstiveTouchRunning,
		cfg.Accessibility.IsReduceTransparencyEnabled,
		cfg.Accessibility.IsDarkerSystemColorsEnabled,
		cfg.Accessibility.PreferredContentSize,
		cfg.Accessibility.MfiHearingAidsStatus,
	})

	authMap := payload.BuildProbeMap(payload.KeysAuth, []interface{}{
		cfg.Auth.IsPasscodeEnabled,
		cfg.Auth.IsBiometricsEnabled,
	})

	screenSize := payload.ComputeScreenSize(
		interfaceToFloat(cfg.Device.ScreenWidth),
		interfaceToFloat(cfg.Device.ScreenHeight),
	)

	physicalMemoryFormatted := ""
	if pm, ok := cfg.Hardware.PhysicalMemory.(uint64); ok {
		physicalMemoryFormatted = payload.FormatPhysicalMemory(pm)
	} else if pm, ok := cfg.Hardware.PhysicalMemory.(int64); ok {
		physicalMemoryFormatted = payload.FormatPhysicalMemory(uint64(pm))
	} else if pm, ok := cfg.Hardware.PhysicalMemory.(int); ok {
		physicalMemoryFormatted = payload.FormatPhysicalMemory(uint64(pm))
	}

	stableDict := payload.BuildStableDict(
		cfg.Device.DeviceName,
		cfg.Device.IosDeviceName,
		cfg.Hardware.CpuType,
		cfg.Hardware.CpuCount,
		physicalMemoryFormatted,
		cfg.Locale.RegionLanguage,
		cfg.Locale.RegionCountry,
		cfg.Locale.CalendarIdentifier,
		cfg.Media.CanSendMail,
		cfg.Media.CanSendText,
		cfg.Auth.IsBiometricsEnabled,
		cfg.Experimental.UserInterfaceStyle,
		cfg.Experimental.DateFormat,
		cfg.Accessibility.IsDarkerSystemColorsEnabled,
		cfg.Accessibility.IsBoldTextEnabled,
		cfg.Accessibility.IsReduceMotionEnabled,
		cfg.Accessibility.IsVideoAutoplayEnabled,
		cfg.Accessibility.IsReduceTransparencyEnabled,
		cfg.Accessibility.PreferredContentSize,
		screenSize,
	)

	deviceHash, implicitHash, hashProvider := payload.ComputeDeviceHash(
		cfg.Device.IcloudUbiquityToken,
		cfg.Device.DeviceUdid,
		stableDict,
		cfg.ReturningSession,
	)
	// `implicit_device_hash` is set inside `computeHashFor` at IDA 0x1bd84
	// ONLY when `device_hash` was NOT in the keychain. A fresh iCloud
	// token fetch (provider=0) can still land on the cached-device_hash
	// branch, in which case implicit stays nil and the field isn't
	// emitted. DeviceHashCached models that state.
	if cfg.DeviceHashCached {
		implicitHash = ""
	}

	exp := buildExperimentalFields(&cfg)

	var devLoc interface{}
	if cfg.DeviceLocation != nil {
		devLoc = buildDeviceLocation(cfg.DeviceLocation)
	}

	var behaviourResults interface{}
	if cfg.BehaviourResults != nil {
		behaviourResults = buildBehaviourResults(cfg.BehaviourResults)
	}

	var portScanResult interface{}
	if cfg.PortScanResult != nil {
		arr := make([]interface{}, len(cfg.PortScanResult))
		for i, v := range cfg.PortScanResult {
			arr[i] = v
		}
		portScanResult = arr
	} else {
		portScanResult = []interface{}{}
	}

	var dcResult *dc.Response
	var dcActualMs float64
	if !cfg.SkipDC {
		dcClient, err := dc.NewDCClient(cfg.HTTPClient, cfg.Proxy)
		if err == nil {
			if cfg.DCAcceptLanguage != "" {
				dcClient.AcceptLanguage = cfg.DCAcceptLanguage
			}
			dcClient.Source = version.SourceFor(resolveBundleVersion(cfg.SDKVersion))
			defer dcClient.Close()
			dcStart := time.Now()
			dcResult, _ = dcClient.SendDCRequest(cfg.SessionID)
			dcActualMs = float64(time.Since(dcStart).Microseconds()) / 1000.0
		}
	} else if cfg.SimulateDCTimeout {
		// Synthesize the exact completion-block outcome a real iOS device produces when
		// its 2s NSURLSession timeout fires (IDA 0x7a4c error branch):
		//   - jsonResponse stays nil → dynamic_evaluator_results omitted (handled below)
		//   - SEONErrorLogger records the "The request timed out." entry
		// Emitting stored_errors: {} alongside perf_network ≈ 2000 ms would be a
		// contradiction a fingerprinting service could catch.
		dcResult = &dc.Response{Success: false, ErrorMessage: dc.ErrorRequestTimedOut}
	} else if cfg.DCSyntheticSuccess {
		// Fake a successful DC roundtrip without going to the network. The
		// resulting payload looks like the dominant pattern in legit captures:
		// populated dynamic_evaluator_results, empty stored_errors, network
		// time sampled from the realistic 380–560 ms band.
		synth, err := dc.BuildSyntheticSuccess(cfg.SessionID, cfg.DCDeviceIP)
		if err == nil {
			dcResult = synth
			dcActualMs = simulateNetworkCallMs()
		}
	}

	var dcDynamic interface{}
	if dcResult != nil && dcResult.JSONResponse != "" {
		dcDynamic = dcResult.JSONResponse
	}

	if dcResult != nil && dcResult.RequestID != "" {
		cfg.RequestID = dcResult.RequestID
	}
	if cfg.RequestID == "" {
		cfg.RequestID = session.NewSessionID()
	}

	// stored_errors auto-populates with the DC failure entry when the DC call fails,
	// matching what the real SDK logs via SEONErrorLogger (verified at IDA 0x7a4c
	// completion block + 0x9e98 log key format). Caller-provided entries are merged.
	storedErrors := mergeStoredErrors(cfg.StoredErrors, dcResult)

	perfActual, perfExec, perfNetwork := resolvePerformanceTiming(&cfg, dcActualMs)

	in := &payload.BuildInput{
		SessionID:                   cfg.SessionID,
		DeviceHash:                  deviceHash,
		ImplicitDeviceHash:          implicitHash,
		HashProvider:                hashProvider,
		DCDynamicEvaluatorResults:   dcDynamic,
		Codename:                    cfg.Codename,
		CoreSeed:                    cfg.CoreSeed,
		RequestID:                   cfg.RequestID,
		Locale:                      localeMap,
		Device:                      deviceMap,
		Hardware:                    hardwareMap,
		Media:                       mediaMap,
		Network:                     networkMap,
		OS:                          osMap,
		Telephony:                   telephonyMap,
		Accessibility:               accessibilityMap,
		Auth:                        authMap,
		GeolocationConfig:           geoConfigMap,
		DeviceLocation:              devLoc,
		BehaviourResults:            behaviourResults,
		Experimental:                exp,
		StoredErrors:                storedErrors,
		PortScanResult:              portScanResult,
		Source:                      version.SourceFor(resolveBundleVersion(cfg.SDKVersion)),
		VersionCode:                 version.VersionCodeFor(resolveBundleVersion(cfg.SDKVersion)),
		PerformanceActualProcessing:   perfActual,
		PerformanceExecTime:           perfExec,
		PerformanceNetworkCall:        perfNetwork,
		TimestampOverride:             cfg.TimestampOverride,
	}

	payloadMap, err := payload.BuildPayload(in)
	if err != nil {
		return "", fmt.Errorf("build payload: %w", err)
	}

	jsonBytes, err := payload.MarshalSortedJSON(payloadMap)
	if err != nil {
		return "", fmt.Errorf("marshal JSON: %w", err)
	}

	return crypto.EncodePayload(cfg.SessionID, jsonBytes)
}

func FingerprintJSON(cfg FingerprintConfig) ([]byte, error) {
	if cfg.SessionID == "" {
		cfg.SessionID = session.NewSessionID()
	}
	if cfg.CoreSeed == "" {
		cfg.CoreSeed = randomCoreSeed()
	}

	geoConfig := cfg.GeolocationConfig
	if geoConfig == nil {
		geoConfig = DefaultGeolocationConfig()
	}

	geoConfigMap := map[string]interface{}{
		"geolocationEnabled":          geoConfig.GeolocationEnabled,
		"geolocationServiceTimeoutMs": geoConfig.GeolocationServiceTimeoutMs,
		"maxGeoLocationCacheAgeSec":   geoConfig.MaxGeoLocationCacheAgeSec,
		"prefetchEnabled":             geoConfig.PrefetchEnabled,
	}

	localeMap := payload.BuildProbeMap(payload.KeysLocale, []interface{}{
		cfg.Locale.RegionTimezone, cfg.Locale.RegionLanguage, cfg.Locale.RegionCountry,
		cfg.Locale.TimezoneIdentifier, cfg.Locale.CalendarIdentifier,
	})
	// KeysDevice has 11 entries and BuildProbeMap zips positionally, so this
	// list must be exactly 11 long and in KeysDevice order. It previously
	// carried 13 — DeviceName and IosDeviceName were included even though
	// neither is a KeysDevice entry — which shifted every field after
	// accessories_count by one: device_adid received the device NAME,
	// screen_brightness received the iOS version string, icloud_ubiquity_token
	// received a number, and so on. Fingerprint() has always passed the
	// correct 11; keep the two lists identical.
	deviceMap := payload.BuildProbeMap(payload.KeysDevice, []interface{}{
		cfg.Device.AccessoriesCount, cfg.Device.DeviceAdid,
		cfg.Device.DeviceOrientation, cfg.Device.DeviceUdid,
		cfg.Device.IosVersion, cfg.Device.PasteboardHash, cfg.Device.ScreenBrightness,
		cfg.Device.ScreenHeight, cfg.Device.ScreenWidth, cfg.Device.IcloudUbiquityToken,
		cfg.Device.IsScreenCaptured,
	})
	hardwareMap := payload.BuildProbeMap(payload.KeysHardware, []interface{}{
		cfg.Hardware.BatteryLevel, cfg.Hardware.BatteryCharging, cfg.Hardware.CpuType,
		cfg.Hardware.CpuCount, cfg.Hardware.IsEmulator, cfg.Hardware.PhysicalMemory,
		cfg.Hardware.SystemUptime,
	})
	mediaMap := payload.BuildProbeMap(payload.KeysMedia, []interface{}{
		cfg.Media.AudioMuteStatus, cfg.Media.AudioVolumeCurrent,
		cfg.Media.CanSendMail, cfg.Media.CanSendText,
	})
	networkMap := payload.BuildProbeMap(payload.KeysNetwork, []interface{}{
		cfg.Network.VpnState, cfg.Network.ProxyAddress, cfg.Network.ProxyState,
		cfg.Network.WifiSsid, cfg.Network.WifiMacAddress,
	})
	osMap := payload.BuildProbeMap(payload.KeysOS, []interface{}{
		cfg.OS.KernelVersion, cfg.OS.KernelName, cfg.OS.KernelArch,
		cfg.OS.IsJailbroken, cfg.OS.IsIosAppOnMac, cfg.OS.IsDebuggerAttached,
	})
	telephonyMap := payload.BuildProbeMap(payload.KeysTelephony, []interface{}{
		cfg.Telephony.NetworkConfig, cfg.Telephony.IsOnCall,
	})
	accessibilityMap := payload.BuildProbeMap(payload.KeysAccessibility, []interface{}{
		cfg.Accessibility.IsBoldTextEnabled, cfg.Accessibility.IsReduceMotionEnabled,
		cfg.Accessibility.IsVideoAutoplayEnabled, cfg.Accessibility.IsAssisstiveTouchRunning,
		cfg.Accessibility.IsReduceTransparencyEnabled, cfg.Accessibility.IsDarkerSystemColorsEnabled,
		cfg.Accessibility.PreferredContentSize, cfg.Accessibility.MfiHearingAidsStatus,
	})
	authMap := payload.BuildProbeMap(payload.KeysAuth, []interface{}{
		cfg.Auth.IsPasscodeEnabled, cfg.Auth.IsBiometricsEnabled,
	})

	screenSize := payload.ComputeScreenSize(
		interfaceToFloat(cfg.Device.ScreenWidth),
		interfaceToFloat(cfg.Device.ScreenHeight),
	)
	physicalMemoryFormatted := ""
	if pm, ok := cfg.Hardware.PhysicalMemory.(uint64); ok {
		physicalMemoryFormatted = payload.FormatPhysicalMemory(pm)
	}

	stableDict := payload.BuildStableDict(
		cfg.Device.DeviceName, cfg.Device.IosDeviceName, cfg.Hardware.CpuType,
		cfg.Hardware.CpuCount, physicalMemoryFormatted, cfg.Locale.RegionLanguage,
		cfg.Locale.RegionCountry, cfg.Locale.CalendarIdentifier,
		cfg.Media.CanSendMail, cfg.Media.CanSendText, cfg.Auth.IsBiometricsEnabled,
		cfg.Experimental.UserInterfaceStyle, cfg.Experimental.DateFormat,
		cfg.Accessibility.IsDarkerSystemColorsEnabled, cfg.Accessibility.IsBoldTextEnabled,
		cfg.Accessibility.IsReduceMotionEnabled, cfg.Accessibility.IsVideoAutoplayEnabled,
		cfg.Accessibility.IsReduceTransparencyEnabled, cfg.Accessibility.PreferredContentSize,
		screenSize,
	)

	deviceHash, implicitHash, hashProvider := payload.ComputeDeviceHash(
		cfg.Device.IcloudUbiquityToken, cfg.Device.DeviceUdid, stableDict, cfg.ReturningSession,
	)

	exp := buildExperimentalFields(&cfg)

	var devLoc interface{}
	if cfg.DeviceLocation != nil {
		devLoc = buildDeviceLocation(cfg.DeviceLocation)
	}
	var behaviourResults interface{}
	if cfg.BehaviourResults != nil {
		behaviourResults = buildBehaviourResults(cfg.BehaviourResults)
	}

	var portScanResult interface{}
	if cfg.PortScanResult != nil {
		arr := make([]interface{}, len(cfg.PortScanResult))
		for i, v := range cfg.PortScanResult {
			arr[i] = v
		}
		portScanResult = arr
	} else {
		portScanResult = []interface{}{}
	}

	// Mirror the DC simulation paths Fingerprint() takes, so the JSON-only
	// builder produces the same stored_errors / dynamic_evaluator_results /
	// performance_* values for the matching SkipDC sub-mode.
	var dcResult *dc.Response
	var dcActualMsJSON float64
	switch {
	case cfg.SkipDC && cfg.SimulateDCTimeout:
		dcResult = &dc.Response{Success: false, ErrorMessage: dc.ErrorRequestTimedOut}
	case cfg.SkipDC && cfg.DCSyntheticSuccess:
		if synth, err := dc.BuildSyntheticSuccess(cfg.SessionID, cfg.DCDeviceIP); err == nil {
			dcResult = synth
			dcActualMsJSON = simulateNetworkCallMs()
		}
	}
	storedErrors := mergeStoredErrors(cfg.StoredErrors, dcResult)

	var dcDynamicJSON interface{}
	if dcResult != nil && dcResult.JSONResponse != "" {
		dcDynamicJSON = dcResult.JSONResponse
	}

	if dcResult != nil && dcResult.RequestID != "" {
		cfg.RequestID = dcResult.RequestID
	}
	if cfg.RequestID == "" {
		cfg.RequestID = session.NewSessionID()
	}

	perfActual, perfExec, perfNetwork := resolvePerformanceTiming(&cfg, dcActualMsJSON)

	in := &payload.BuildInput{
		SessionID:                   cfg.SessionID,
		DeviceHash:                  deviceHash,
		ImplicitDeviceHash:          implicitHash,
		HashProvider:                hashProvider,
		DCDynamicEvaluatorResults:   dcDynamicJSON,
		Codename:                    cfg.Codename,
		CoreSeed:                    cfg.CoreSeed,
		RequestID:                   cfg.RequestID,
		Locale:                      localeMap,
		Device:                      deviceMap,
		Hardware:                    hardwareMap,
		Media:                       mediaMap,
		Network:                     networkMap,
		OS:                          osMap,
		Telephony:                   telephonyMap,
		Accessibility:               accessibilityMap,
		Auth:                        authMap,
		GeolocationConfig:           geoConfigMap,
		DeviceLocation:              devLoc,
		BehaviourResults:            behaviourResults,
		Experimental:                exp,
		StoredErrors:                storedErrors,
		PortScanResult:              portScanResult,
		Source:                      version.SourceFor(resolveBundleVersion(cfg.SDKVersion)),
		VersionCode:                 version.VersionCodeFor(resolveBundleVersion(cfg.SDKVersion)),
		PerformanceActualProcessing:   perfActual,
		PerformanceExecTime:           perfExec,
		PerformanceNetworkCall:        perfNetwork,
		TimestampOverride:             cfg.TimestampOverride,
	}

	payloadMap, err := payload.BuildPayload(in)
	if err != nil {
		return nil, err
	}

	return payload.MarshalSortedJSON(payloadMap)
}

func Decrypt(wireString string) (sessionID string, payloadData map[string]interface{}, err error) {
	sid, jsonBytes, err := crypto.DecodePayload(wireString)
	if err != nil {
		return "", nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &m); err != nil {
		return sid, nil, fmt.Errorf("unmarshal JSON: %w", err)
	}
	return sid, m, nil
}

func buildExperimentalFields(cfg *FingerprintConfig) map[string]interface{} {
	exp := make(map[string]interface{})

	setIfNotNil(exp, "force_touch_support", cfg.Experimental.ForceTouchSupport)
	setIfNotNil(exp, "display_gamut", cfg.Experimental.DisplayGamut)
	setIfNotNil(exp, "user_interface_style", cfg.Experimental.UserInterfaceStyle)
	setIfNotNil(exp, "thermal_state", cfg.Experimental.ThermalState)
	setIfNotNil(exp, "native_bounds", cfg.Experimental.NativeBounds)
	setIfNotNil(exp, "is_display_zoom_enabled", cfg.Experimental.IsDisplayZoomEnabled)
	setIfNotNil(exp, "connected_accessories", cfg.Experimental.ConnectedAccessories)
	setIfNotNil(exp, "accessory_hash", cfg.Experimental.AccessoryHash)

	setIfNotNil(exp, "is_plugged_in", cfg.Experimental.IsPluggedIn)
	setIfNotNil(exp, "is_arm64", cfg.Experimental.IsArm64)

	if cfg.Experimental.DateFormat != "" {
		exp["date_format"] = cfg.Experimental.DateFormat
	}

	setIfNotNil(exp, "is_low_power_enabled", cfg.Experimental.IsLowPowerEnabled)
	setIfNotNil(exp, "is_react_native", cfg.Experimental.IsReactNative)
	setIfNotNil(exp, "is_root_fs_compromised", cfg.Experimental.IsRootFsCompromised)
	setIfNotNil(exp, "has_invalid_code_sig", cfg.Experimental.HasInvalidCodeSig)
	setIfNotNil(exp, "pid", cfg.Experimental.Pid)
	setIfNotNil(exp, "is_flutter", cfg.Experimental.IsFlutter)

	// radio_technology + is_cellular_data_restricted are emitted
	// UNCONDITIONALLY by SEONTelephonyProbe.getExperimentalFields (IDA
	// 0xc8b8). Real iPhones always include both as JSON booleans. A missing
	// radio_technology key is an immediate tell.
	if cfg.Experimental.IsCellularDataRestricted != nil {
		exp["is_cellular_data_restricted"] = cfg.Experimental.IsCellularDataRestricted
	} else {
		exp["is_cellular_data_restricted"] = false
	}
	if cfg.Experimental.RadioTechnology != nil {
		exp["radio_technology"] = cfg.Experimental.RadioTechnology
	} else {
		exp["radio_technology"] = false
	}

	// Network probe's getExperimentalFields (IDA 0x6b58) always emits
	// vpn_protocol, proxy_type, ap_is_hotspot, nw_interfaces, def_gw.
	// getSafe returns NSNull when the underlying method yields nil, which
	// serialises as JSON null — so the key is present on every real
	// fingerprint even when the value is null.
	if cfg.Experimental.VpnProtocol != nil {
		exp["vpn_protocol"] = cfg.Experimental.VpnProtocol
	} else {
		// Default to "UNKNOWN" — the only value ever observed with no VPN
		// up. See VpnProtocolUnknown for the evidence and the one open
		// question. Callers with an active tunnel set the interface name
		// here explicitly.
		exp["vpn_protocol"] = VpnProtocolUnknown
	}
	if cfg.Experimental.ProxyType != nil {
		exp["proxy_type"] = cfg.Experimental.ProxyType
	} else {
		// kCFProxyTypeNone is what `-[ProxySettings proxyType]` returns when
		// SCDynamicStore has no active proxy — matches Apple's CF constant
		// name literally.
		exp["proxy_type"] = "kCFProxyTypeNone"
	}
	// proxy_autoconfig_settings is ONLY emitted when proxyType ==
	// kCFProxyTypeAutoConfigurationURL (IDA 0x6ca8). Keep conditional.
	setIfNotNil(exp, "proxy_autoconfig_settings", cfg.Experimental.ProxyAutoconfigSettings)
	if cfg.Experimental.ApIsHotspot != nil {
		exp["ap_is_hotspot"] = cfg.Experimental.ApIsHotspot
	} else {
		exp["ap_is_hotspot"] = false
	}
	setIfNotNil(exp, "nw_interfaces", cfg.Experimental.NwInterfaces)
	setIfNotNil(exp, "def_gw", cfg.Experimental.DefGw)

	// av_output_info — always emitted by SEONMediaProbe.getExperimentalFields
	// (IDA 0x1ea28). Default shape is a single built-in speaker output with
	// the name taken from `cfg.Locale.SpeakerName` (set by LocalePreset or
	// by the caller for custom locales). Callers can override the whole dict
	// via cfg.Experimental.AvOutputInfo.
	if cfg.Experimental.AvOutputInfo != nil {
		exp["av_output_info"] = cfg.Experimental.AvOutputInfo
	} else {
		exp["av_output_info"] = defaultAvOutputInfoForLocale(cfg.Locale.SpeakerName)
	}
	setIfNotNil(exp, "supported_biometry_type", cfg.Experimental.SupportedBiometryType)

	setIfNotNil(exp, "screen_bounds", cfg.Experimental.ScreenBounds)
	setIfNotNil(exp, "screen_scale", cfg.Experimental.ScreenScale)
	setIfNotNil(exp, "native_scale", cfg.Experimental.NativeScale)
	setIfNotNil(exp, "metal_info", cfg.Experimental.MetalInfo)

	if cfg.Experimental.IsCarplayActive != nil {
		exp["is_carplay_active"] = cfg.Experimental.IsCarplayActive
	} else {
		exp["is_carplay_active"] = false
	}
	if cfg.Experimental.IsUnityHost != nil {
		exp["is_unity_host"] = cfg.Experimental.IsUnityHost
	} else {
		exp["is_unity_host"] = false
	}
	exp["unity_host_reason"] = cfg.Experimental.UnityHostReason

	setIfNotNil(exp, "experimental_device_location", cfg.Experimental.ExperimentalDeviceLocation)

	return exp
}

func buildDeviceLocation(loc *DeviceLocationData) map[string]interface{} {
	return map[string]interface{}{
		"latitude":         loc.Latitude,
		"longitude":        loc.Longitude,
		"accuracy":         loc.Accuracy,
		"accuracy_auth_lvl": loc.AccuracyAuthLvl,
		"status":           loc.Status,
		"auth_status":      loc.AuthStatus,
		"is_simulated":     loc.IsSimulated,
		"is_cached":        loc.IsCached,
		"timestamp":        loc.Timestamp,
	}
}

func buildBehaviourResults(b *BehaviourData) map[string]interface{} {
	// Fast path: simulator output already has the complete key set the SDK
	// emits (verified against IDA 0x1300c). Pass it through untouched.
	if b.RawResults != nil {
		out := make(map[string]interface{}, len(b.RawResults))
		for k, v := range b.RawResults {
			out[k] = v
		}
		return out
	}

	m := make(map[string]interface{})
	setIfNotNil(m, "min_user_acc", b.MinUserAcc)
	setIfNotNil(m, "max_user_acc", b.MaxUserAcc)
	setIfNotNil(m, "mean_user_acc", b.MeanUserAcc)
	setIfNotNil(m, "median_user_acc", b.MedianUserAcc)
	setIfNotNil(m, "var_user_acc", b.VarUserAcc)
	setIfNotNil(m, "p95_user_acc", b.P95UserAcc)
	setIfNotNil(m, "min_rot_rate", b.MinRotRate)
	setIfNotNil(m, "max_rot_rate", b.MaxRotRate)
	setIfNotNil(m, "mean_rot_rate", b.MeanRotRate)
	setIfNotNil(m, "median_rot_rate", b.MedianRotRate)
	setIfNotNil(m, "var_rot_rate", b.VarRotRate)
	setIfNotNil(m, "p95_rot_rate", b.P95RotRate)
	setIfNotNil(m, "min_gravity", b.MinGravity)
	setIfNotNil(m, "max_gravity", b.MaxGravity)
	setIfNotNil(m, "mean_gravity", b.MeanGravity)
	setIfNotNil(m, "median_gravity", b.MedianGravity)
	setIfNotNil(m, "var_gravity", b.VarGravity)
	setIfNotNil(m, "p95_gravity", b.P95Gravity)
	setIfNotNil(m, "min_mag_field", b.MinMagField)
	setIfNotNil(m, "max_mag_field", b.MaxMagField)
	setIfNotNil(m, "mean_mag_field", b.MeanMagField)
	setIfNotNil(m, "median_mag_field", b.MedianMagField)
	setIfNotNil(m, "var_mag_field", b.VarMagField)
	setIfNotNil(m, "p95_mag_field", b.P95MagField)
	setIfNotNil(m, "mag_declination", b.MagDeclination)
	setIfNotNil(m, "heading_changes", b.HeadingChanges)
	setIfNotNil(m, "orientation_changes", b.OrientationChanges)
	setIfNotNil(m, "monitoring_duration", b.MonitoringDuration)
	setIfNotNil(m, "most_freq_attitude", b.MostFreqAttitude)
	return m
}

// defaultAvOutputInfoForLocale returns the `av_output_info` dict shape a
// real iPhone emits when audio is routed to the built-in speaker, with the
// speaker name taken from the caller's locale. iOS's AVAudioSession emits
// this string localised (UIKit string table lookup) — Polish "Głośnik",
// German "Lautsprecher", Japanese "スピーカー", etc. English fallback is
// used when `speakerName` is empty.
//
// `channelLabel = 4294967295` = kAudioChannelLabel_Unknown (UInt32 -1) and
// `portType`/`portUID` are Core Audio constants that stay "Speaker"
// regardless of language. Shape verified from
// `-[SEONMediaProbe getCurrentAvOuputInfo]` + the iPhone 14 Pro
// Polish-locale capture.
func defaultAvOutputInfoForLocale(speakerName string) map[string]interface{} {
	if speakerName == "" {
		speakerName = "Speaker"
	}
	return map[string]interface{}{
		"channels": []interface{}{
			map[string]interface{}{
				"channelLabel":  uint64(4294967295),
				"channelName":   speakerName + " 1",
				"channelNumber": 1,
				"owningPortUID": "Speaker",
			},
			map[string]interface{}{
				"channelLabel":  uint64(4294967295),
				"channelName":   speakerName + " 2",
				"channelNumber": 2,
				"owningPortUID": "Speaker",
			},
		},
		"dataSources": []interface{}{},
		"portName":    speakerName,
		"portType":    "Speaker",
		"portUID":     "Speaker",
	}
}

func setIfNotNil(m map[string]interface{}, key string, val interface{}) {
	if val != nil {
		m[key] = val
	}
}

func interfaceToFloat(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint64:
		return float64(val)
	default:
		return 0
	}
}

// resolveBundleVersion returns the caller-supplied SDKVersion if non-empty,
// otherwise falls back to version.DefaultBundleVersion (the compile-time
// baseline). Centralised so source + version_code + DC payload source all
// stay in sync from a single input.
func resolveBundleVersion(userOverride string) string {
	if userOverride != "" {
		return userOverride
	}
	return version.DefaultBundleVersion
}

// VpnProtocolUnknown is the `experimental_fields.vpn_protocol` value observed
// whenever no VPN tunnel is up. It pairs with `vpn_state: "NOT_CONNECTED"`.
//
// Every vpn_protocol observation available to this library:
//
//	VPN connected     "utun5"     iPhone 14 Pro capture — the TUNNEL INTERFACE
//	                              name, which also appears in nw_interfaces.
//	                              See cmd/regen-capture.
//	VPN not connected "UNKNOWN"   all three testdata/freshwhatnot payloads.
//	JSON null         never observed.
//
// So the field carries the active tunnel's interface name, falling back to
// the literal "UNKNOWN" when there isn't one. The library previously defaulted
// to null, which no capture supports — that made the field the wrong TYPE on
// every fingerprint, a stronger tell than a wrong value.
//
// Open question: "UNKNOWN" may specifically mean "VPN configuration profiles
// exist but none is connected", in which case a handset with no VPN profiles
// at all might report something else (possibly null). The freshwhatnot device
// did have profiles installed and disabled. This cannot be settled without a
// capture from a profile-free handset, and it does not change the default:
// "UNKNOWN" is the only disconnected value ever seen, and a persona that
// looks like it has a VPN app installed is unremarkable.
const VpnProtocolUnknown = "UNKNOWN"

// dcErrorLogKeyPrefix is the stored_errors key format used by -[SEONErrorLogger logError...]
// at 0x9e98: fmt.Sprintf("%@_%@:%@", class, method, message). For DC failures the class
// is "SEONNetworkProbe" (extracted from the block name at 0x7ad4) and the method is the
// selector captured in the block — `sendDynamicComponentRequest:dcRequestCompletionBlock:`.
// The final message is appended after the trailing colon.
const dcErrorLogKeyPrefix = "SEONNetworkProbe_sendDynamicComponentRequest:dcRequestCompletionBlock:"

// BuildDCErrorLog returns a stored_errors dict with the DC-failure entry that the real
// SDK emits (verified at 0x7a4c and 0x9e98). Typically callers don't need this — the
// entry is auto-populated when a real DC call fails — but it's useful for constructing
// deterministic fingerprints in tests or to force a specific error variant.
//
// message should be one of the dc.Error* constants:
//
//	dc.ErrorRequestTimedOut                      // NSURLErrorTimedOut localized
//	fmt.Sprintf(dc.ErrorRequestFailedFormat, N)  // HTTP non-200
//	dc.ErrorParseDCResponse                      // malformed JSON response
func BuildDCErrorLog(message string) map[string]interface{} {
	return map[string]interface{}{
		dcErrorLogKeyPrefix + ":" + message: 1,
	}
}

// mergeStoredErrors combines any caller-provided errors with the DC call result error.
// Returns a map[string]interface{} (an empty {} when there are no errors) matching the
// real SDK's NSMutableDictionary copy returned from -[SEONErrorLogger getErrorLog] at
// 0xa0ac. Real SDK key format: "<class>_<method>:<message>" → int count.
func mergeStoredErrors(userProvided interface{}, dcResult *dc.Response) map[string]interface{} {
	out := make(map[string]interface{})

	if existing, ok := userProvided.(map[string]interface{}); ok {
		for k, v := range existing {
			out[k] = v
		}
	}

	if dcResult != nil && dcResult.ErrorMessage != "" {
		key := dcErrorLogKeyPrefix + ":" + dcResult.ErrorMessage
		if current, ok := out[key]; ok {
			switch n := current.(type) {
			case int:
				out[key] = n + 1
			case float64:
				out[key] = int(n) + 1
			default:
				out[key] = 2
			}
		} else {
			out[key] = 1
		}
	}

	return out
}
