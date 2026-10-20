package fp

type Fingerprint struct {
	AndroidID                 string             `json:"android_id"`
	BuildDevice               string             `json:"build_device"`
	BuildModel                string             `json:"build_model"`
	BuildHardware             string             `json:"build_hardware"`
	DeviceName                string             `json:"device_name"`
	DeviceOrientation         string             `json:"device_orientation"`
	FreeStorage               int64              `json:"free_storage"`
	HasProximitySensor        bool               `json:"has_proximity_sensor"`
	PasteboardHash            string             `json:"pasteboard_hash"`
	ScreenHeight              int                `json:"screen_height"`
	ScreenWidth               int                `json:"screen_width"`
	ScreenScale               float64            `json:"screen_scale"`
	ScreenBrightness          int                `json:"screen_brightness"`
	SensorHash                string             `json:"sensor_hash"`
	TotalStorage              int64              `json:"total_storage"`
	GsfID                     string             `json:"gsf_id"`
	IsTablet                  bool               `json:"is_tablet"`
	AudioMuteStatus           bool               `json:"audio_mute_status"`
	AudioVolumeCurrent        int                `json:"audio_volume_current"`
	IsRemoteControlConnected  bool               `json:"is_remote_control_connected"`
	RemoteControlProvider     any                `json:"remote_control_provider"`
	IsScreenBeingMirrored     bool               `json:"is_screen_being_mirrored"`
	IsClickAutomatorInstalled bool               `json:"is_click_automator_installed"`
	EnabledAccessibilityApps  []any              `json:"enabled_accessibility_apps"`
	InterferingApps           []any              `json:"interfering_apps"`
	MediaDrmID                string             `json:"media_drm_id"`
	WifiMacAddress            any                `json:"wifi_mac_address"`
	WifiSsid                  any                `json:"wifi_ssid"`
	VpnState                  string             `json:"vpn_state"`
	ProxyState                string             `json:"proxy_state"`
	ProxyAddress              any                `json:"proxy_address"`
	WifiPrivateIP             string             `json:"wifi_private_ip"`
	BatteryCharging           bool               `json:"battery_charging"`
	BatteryHealth             string             `json:"battery_health"`
	BatteryLevel              int                `json:"battery_level"`
	BatteryTemperature        int                `json:"battery_temperature"`
	BatteryVoltage            int                `json:"battery_voltage"`
	BatteryTotalCapacity      int                `json:"battery_total_capacity"`
	CPUType                   string             `json:"cpu_type"`
	CPUCount                  int                `json:"cpu_count"`
	CPUSpeed                  int                `json:"cpu_speed"`
	CPUHash                   string             `json:"cpu_hash"`
	LastBootTime              int                `json:"last_boot_time"`
	PhysicalMemory            int                `json:"physical_memory"`
	SystemUptime              int                `json:"system_uptime"`
	IsNfcAvailable            bool               `json:"is_nfc_available"`
	IsNfcEnabled              bool               `json:"is_nfc_enabled"`
	InputDeviceData           string             `json:"input_device_data"`
	PowerSource               string             `json:"power_source"`
	RegionCountry             string             `json:"region_country"`
	RegionLanguage            string             `json:"region_language"`
	RegionTimezone            string             `json:"region_timezone"`
	TimezoneIdentifier        string             `json:"timezone_identifier"`
	AndroidVersion            string             `json:"android_version"`
	IsRooted                  bool               `json:"is_rooted"`
	KernelArch                string             `json:"kernel_arch"`
	KernelName                string             `json:"kernel_name"`
	KernelVersion             string             `json:"kernel_version"`
	UsbCableState             string             `json:"usb_cable_state"`
	UsbDebuggingState         string             `json:"usb_debugging_state"`
	BootloaderState           string             `json:"bootloader_state"`
	DeveloperOptionsState     string             `json:"developer_options_state"`
	Timestamp                 int64              `json:"timestamp"`
	CarrierName               string             `json:"carrier_name"`
	CarrierCountry            string             `json:"carrier_country"`
	DeviceID                  string             `json:"device_id"`
	NetworkConfig             string             `json:"network_config"`
	IsOnCall                  bool               `json:"is_on_call"`
	SimSlotCount              int                `json:"sim_slot_count"`
	DeviceLocation            any                `json:"device_location"`
	GeolocationConfig         GeolocationConfig  `json:"geolocation_config"`
	LocationTimestamp         int                `json:"location_timestamp"`
	BiometricStatus           string             `json:"biometric_status"`
	IsKeyguardSecure          bool               `json:"is_keyguard_secure"`
	FirstAPILevel             int                `json:"first_api_level"`
	IsEspressoConnected       bool               `json:"is_espresso_connected"`
	IsHiddenapiModified       *bool              `json:"is_hiddenapi_modified"`
	ProfileAdministrators     []any              `json:"profile_administrators"`
	IsAppCloned               bool               `json:"is_app_cloned"`
	PackageName               string             `json:"package_name"`
	PackageSignatures         []string           `json:"package_signatures"`
	AppGUID                   string             `json:"app_guid"`
	BuildID                   string             `json:"build_id"`
	BuildManufacturer         string             `json:"build_manufacturer"`
	BuildNumber               string             `json:"build_number"`
	BuildTime                 int                `json:"build_time"`
	SessionID                 string             `json:"session_id"`
	Source                    string             `json:"source"`
	ExperimentalFields        ExperimentalFields `json:"experimental_fields"`
	PerformanceProbes         map[string]any     `json:"performance_probes"`
	PerformanceEmulator       int                `json:"performance_emulator"`
	RequestID                 string             `json:"request_id"`
	DynamicEvaluatorResults   string             `json:"dynamic_evaluator_results"`
	PerformanceNetworkCall    int                `json:"performance_network_call"`
	IsEmulator                bool               `json:"is_emulator"`
	EmulatorTriggerCauses     []string           `json:"emulator_trigger_causes"`
	PredictedEmulatorEngine   string             `json:"predicted_emulator_engine"`
	VersionCode               string             `json:"version_code"`
	PerformanceExecTime       int                `json:"performance_exec_time"`
	DeviceHash                string             `json:"device_hash"`
	PortScanResult            []string           `json:"port_scan_result"`
	BehaviourResults          map[string]any     `json:"behaviour_results"`
	StoredErrors              []string           `json:"stored_errors"`
	CommErrors                []any              `json:"comm_errors"`
}
type GeolocationConfig struct {
	Enabled     bool `json:"enabled"`
	TimeoutMs   int  `json:"timeout_ms"`
	Prefetch    bool `json:"prefetch"`
	MaxCacheAge int  `json:"max_cache_age"`
}
type FrdScan struct {
	F  []int `json:"f"`
	Ws []int `json:"ws"`
}
type LvlInf struct {
	H int   `json:"h"`
	M []any `json:"m"`
}
type ExperimentalFields struct {
	SensorsBitmask                 int      `json:"sensors_bitmask"`
	IsSecureNfcAvailable           bool     `json:"is_secure_nfc_available"`
	CalculatedTotalBatteryCapacity int      `json:"calculated_total_battery_capacity"`
	SupportedAbis                  string   `json:"supported_abis"`
	IsSecureNfcEnabled             bool     `json:"is_secure_nfc_enabled"`
	GlesVersion                    string   `json:"gles_version"`
	IsRootedRootbeer               bool     `json:"is_rooted_rootbeer"`
	NativeCrashBit                 string   `json:"native_crash_bit"`
	IsDebuggerConnected            bool     `json:"is_debugger_connected"`
	LdPreloadString                string   `json:"ld_preload_string"`
	IsRootedNative                 bool     `json:"is_rooted_native"`
	IsSafetynetfixLoaded           bool     `json:"is_safetynetfix_loaded"`
	IsOemUnlockAllowed             bool     `json:"is_oem_unlock_allowed"`
	VbmetaDigest                   string   `json:"vbmeta_digest"`
	IsMagiskByMount                bool     `json:"is_magisk_by_mount"`
	MountFilesModifyInfo           string   `json:"mount_files_modify_info"`
	Permissions                    []string `json:"permissions"`
	Wrappers                       []any    `json:"wrappers"`
	IsLocationMockingEnabled       bool     `json:"is_location_mocking_enabled"`
	RunningFingerprints            int      `json:"running_fingerprints"`
	PageSize                       int      `json:"page_size"`
	CurrentActiveAudioDevice       int      `json:"current_active_audio_device"`
	DisplayFlags                   string   `json:"display_flags"`
	MediaDrmIDList                 []string `json:"media_drm_id_list"`
	IsSpeakerphoneActive           bool     `json:"is_speakerphone_active"`
	CodecHash                      string   `json:"codec_hash"`
	WifiGw                         string   `json:"wifi_gw"`
	NwIfList                       []string `json:"nw_if_list"`
	Line1Number                    any      `json:"line1_number"`
	IsSmsCapable                   bool     `json:"is_sms_capable"`
	SimOperatorName                []string `json:"sim_operator_name"`
	DefaultSmsPackage              string   `json:"default_sms_package"`
	TypeAllocationCode             string   `json:"type_allocation_code"`
	GsmRoamingState                string   `json:"gsm_roaming_state"`
	DataRoamingStateProp           string   `json:"data_roaming_state_prop"`
	SimState                       string   `json:"sim_state"`
	MmsUserAgent                   any      `json:"mms_user_agent"`
	SimOperatorCode                []string `json:"sim_operator_code"`
	IsNetworkRoaming               []bool   `json:"is_network_roaming"`
	MmsUaProfURL                   any      `json:"mms_ua_prof_url"`
	IsOnCallPerm                   bool     `json:"is_on_call_perm"`
	IsInManagedCall                any      `json:"is_in_managed_call"`
	VoicemailNumber                any      `json:"voicemail_number"`
	PhoneNumber                    any      `json:"phone_number"`
	IsInCall                       any      `json:"is_in_call"`
	SimSpecificCarrierIDName       []string `json:"sim_specific_carrier_id_name"`
	DataRoamingState               []string `json:"data_roaming_state"`
	SimSpecificCarrierID           []string `json:"sim_specific_carrier_id"`
	IsCallActiveByAudio            bool     `json:"is_call_active_by_audio"`
	SimOperatorCountry             []string `json:"sim_operator_country"`
	CarrierOperatorCode            []string `json:"carrier_operator_code"`
	AttestKmVersion                int      `json:"attest_km_version"`
	AttestOsVersion                int      `json:"attest_os_version"`
	AttestKmLevel                  string   `json:"attest_km_level"`
	AttestChallenge                string   `json:"attest_challenge"`
	AttestCertSignature            string   `json:"attest_cert_signature"`
	AttestSwTags                   []int    `json:"attest_sw_tags"`
	IsSystemUser                   bool     `json:"is_system_user"`
	InstrClass                     string   `json:"instr_class"`
	AttestTime                     int64    `json:"attest_time"`
	IsApplistoDetected             bool     `json:"is_applisto_detected"`
	IsPackageinfoInconsistent      bool     `json:"is_packageinfo_inconsistent"`
	AttestCertIds                  []string `json:"attest_cert_ids"`
	FrdScan                        FrdScan  `json:"frd_scan"`
	IsWorkProfile                  bool     `json:"is_work_profile"`
	LvlInf                         LvlInf   `json:"lvl_inf"`
	AttestSignatures               []string `json:"attest_signatures"`
	AttestVerBootHash              string   `json:"attest_ver_boot_hash"`
	AttestPackages                 []string `json:"attest_packages"`
	AttestationCapability          string   `json:"attestation_capability"`
	IsRootedByProp                 bool     `json:"is_rooted_by_prop"`
	AttestIsDeviceUnlocked         string   `json:"attest_is_device_unlocked"`
	AttestSecLevel                 string   `json:"attest_sec_level"`
	AttestHwTags                   any      `json:"attest_hw_tags"`
	AttestVerBootState             string   `json:"attest_ver_boot_state"`
	AttestVersion                  int      `json:"attest_version"`
	AttestRtErrorFlag              bool     `json:"attest_rt_error_flag"`
	AttestVerBootKey               string   `json:"attest_ver_boot_key"`
	AttestOsPatchLevel             int      `json:"attest_os_patch_level"`
	AttestLogs                     []any    `json:"attest_logs"`
	CameraApps                     []string `json:"camera_apps"`
	ExperimentalDeviceLocation     any      `json:"experimental_device_location"`
}
type MediaProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type TelephonyProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type IntegrityProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type DeviceProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type HardwareProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type OSProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type LocaleProbe struct {
	Scan      int `json:"scan"`
	Bootstrap int `json:"bootstrap"`
}
type NetworkProbe struct {
	Scan         int `json:"scan"`
	Experimental int `json:"experimental"`
	Bootstrap    int `json:"bootstrap"`
}
type GeolocationProbe struct {
	Scan      int `json:"scan"`
	Bootstrap int `json:"bootstrap"`
}
type PerformanceProbes struct {
	MediaProbe       MediaProbe       `json:"MediaProbe"`
	TelephonyProbe   TelephonyProbe   `json:"TelephonyProbe"`
	IntegrityProbe   IntegrityProbe   `json:"IntegrityProbe"`
	DeviceProbe      DeviceProbe      `json:"DeviceProbe"`
	HardwareProbe    HardwareProbe    `json:"HardwareProbe"`
	OSProbe          OSProbe          `json:"OSProbe"`
	LocaleProbe      LocaleProbe      `json:"LocaleProbe"`
	NetworkProbe     NetworkProbe     `json:"NetworkProbe"`
	GeolocationProbe GeolocationProbe `json:"GeolocationProbe"`
}
type Acc struct {
	Sc       int   `json:"sc"`
	Bc       int   `json:"bc"`
	TopDiffs []int `json:"topDiffs"`
	Sample   int   `json:"sample"`
}
type Mag struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Med    float64 `json:"med"`
	P95    float64 `json:"p95"`
	Sample int     `json:"sample"`
}
type Light struct {
	Min    int `json:"min"`
	Max    int `json:"max"`
	Med    int `json:"med"`
	P95    int `json:"p95"`
	Sample int `json:"sample"`
}
type Prox struct {
	Elems  []any `json:"elems"`
	Sample int   `json:"sample"`
}
type Gyro struct {
	Sc       int   `json:"sc"`
	Bc       int   `json:"bc"`
	TopDiffs []int `json:"topDiffs"`
	Sample   int   `json:"sample"`
}
type BehaviourResults struct {
	Acc      Acc   `json:"acc"`
	Duration int   `json:"duration"`
	Mag      Mag   `json:"mag"`
	Light    Light `json:"light"`
	Prox     Prox  `json:"prox"`
	Gyro     Gyro  `json:"gyro"`
}
