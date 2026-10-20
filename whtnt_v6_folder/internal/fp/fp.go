package fp

import (
	"fmt"
	mathrand "math/rand"
	"strings"
	"time"
)

type SessionID string

func GenerateLegitFingerprint(ip string) (SessionID, Fingerprint) {
	if len(devices) == 0 {
		return "", Fingerprint{}
	}
	d := devices[mathrand.Intn(len(devices))]

	nowMs := time.Now().UnixMilli()
	uptimeSec := int64(randInt(1800, 28800))
	lastBootSec := nowMs/1000 - uptimeSec
	buildTime := lastBootSec - int64(randInt(86400*30, 86400*365))

	mediaDrmID := randHex32()
	deviceID := mediaDrmID
	var sessionID SessionID = SessionID(randUUID())
	appGUID := randUUID()
	requestID := randUUID()
	attestChallenge := randUUID()

	kernelArch := kernelArchForBoard(d.Board)
	cpuType := cpuTypeForBoard(d.Board)
	cpuCount := cpuCountForBoard(d.Board)

	tz := randChoice(tzList)
	carriers, ok := carriersByCountry[tz.Country]
	if !ok {
		carriers = carriersByCountry["US"]
		tz = tzList[0]
	}
	carrier := randChoice(carriers)

	wifiIP, wifiGW := randomPrivateIP()

	totalStorageGB := randChoice([]int{32, 64, 128, 128, 256, 256, 256, 512})
	totalStorage := int64(totalStorageGB) * 1024 * 1024 * 1024
	freeStorage := int64(float64(totalStorage) * (1 - mathrand.Float64()*0.55 - 0.2))
	ramGB := randChoice([]int{3, 4, 4, 6, 6, 8, 8, 12})
	physicalMemory := int64(ramGB)*1024*1024*1024 - int64(randInt(50, 200))*1024*1024

	battCapacity := (randInt(2800, 5200) / 50) * 50
	battLevel := randInt(15, 99)
	battVoltage := randInt(3600, 4100)
	battTemp := randInt(24, 36)

	sensorHash := randHex32()
	cpuHash := randHex32()
	pastboardHash := randHex32()
	codecHash := randHex32()
	vbmetaDigest := randHex32()
	deviceHash := randHex32()
	certID1 := randHex(16)
	certID2 := randHex(16)
	pkgSig := randBase64(32)
	bootKey := randBase64(32)
	bootHash := randBase64(32)

	attestTime := nowMs + int64(randInt(1, 20))
	mountT1 := nowMs - int64(randInt(100, 3000))
	mountT2 := nowMs - int64(randInt(3001, 15000))
	mountT3 := nowMs - int64(randInt(0, 800))
	mountInfo := fmt.Sprintf("%d;%d;%d", mountT1, mountT2, mountT3)

	var supportedABIs string
	if kernelArch == "aarch64" {
		supportedABIs = "[arm64-v8a, armeabi-v7a, armeabi]"
	} else {
		supportedABIs = "[armeabi-v7a, armeabi]"
	}

	accelReadings := jitterSensorReadings(d.Accel, 0.035)
	accTopDiffs := consecutiveDiffs(accelReadings, 10)
	accSamples := len(d.Accel)
	if accSamples == 0 {
		accSamples = randInt(180, 350)
	} else {
		delta := int(float64(accSamples)*0.08) + 1
		accSamples += randInt(-delta, delta)
	}

	gyroReadings := jitterSensorReadings(d.Gyro, 0.012)
	gyroTopDiffs := consecutiveDiffs(gyroReadings, 10)
	gyroSamples := len(d.Gyro)
	if gyroSamples == 0 {
		gyroSamples = randInt(80, 200)
	} else {
		delta := int(float64(gyroSamples)*0.08) + 1
		gyroSamples += randInt(-delta, delta)
	}

	var magMin, magMax, magMed, magP95 float64
	magSamples := len(d.Mag)
	if magSamples > 0 {
		magMin, magMax, magMed, magP95 = statsFromMagnitudes(sensorMagnitudes(d.Mag))
	} else {
		base := 42.0 + mathrand.Float64()*14.0
		v := 1.0 + mathrand.Float64()*7.0
		magMin = base - v
		magMed = base + (mathrand.Float64()-0.3)*v*0.3
		magP95 = magMed + mathrand.Float64()*v*0.4
		magMax = base + v
		magSamples = randInt(60, 160)
	}

	frdVal := randInt(20000, 35000)

	attestKmVer, attestVer, attestKmLevel, attestSecLevel, attestBootState, attestIsUnlocked := attestParamsForSDK(d.VersionSDK)

	sensorBitmask := d.SensorBitmask
	if sensorBitmask == 0 {
		sensorBitmask = 0x1FFFF
	}

	screenHeight := d.ScreenHeight
	screenWidth := d.ScreenWidth
	if screenHeight == 0 {
		screenHeight = 1920
		screenWidth = 1080
	}

	probeScanMedia := randInt(1, 3)
	probeScanTelephony := randInt(1, 2)
	probeScanIntegrity := randInt(20, 35)
	probeScanDevice := randInt(2, 5)
	probeScanHardware := randInt(7, 12)
	probeScanOS := randInt(1, 4)
	probeScanLocale := randInt(1, 3)
	probeScanNetwork := randInt(1, 3)
	probes := map[string]any{
		"MediaProbe":       map[string]int{"scan": probeScanMedia, "experimental": randInt(8, 15), "bootstrap": randInt(1, 3)},
		"TelephonyProbe":   map[string]int{"scan": probeScanTelephony, "experimental": randInt(2, 5), "bootstrap": 0},
		"IntegrityProbe":   map[string]int{"scan": probeScanIntegrity, "experimental": randInt(3, 8), "bootstrap": 0},
		"DeviceProbe":      map[string]int{"scan": probeScanDevice, "experimental": 0, "bootstrap": randInt(7, 12)},
		"HardwareProbe":    map[string]int{"scan": probeScanHardware, "experimental": randInt(1, 3), "bootstrap": randInt(9, 14)},
		"OSProbe":          map[string]int{"scan": probeScanOS, "experimental": randInt(4, 8), "bootstrap": randInt(18, 28)},
		"LocaleProbe":      map[string]int{"scan": probeScanLocale, "bootstrap": randInt(0, 2)},
		"NetworkProbe":     map[string]int{"scan": probeScanNetwork, "experimental": randInt(1, 2), "bootstrap": randInt(1, 2)},
		"GeolocationProbe": map[string]int{"scan": 0, "bootstrap": randInt(1, 3)},
	}
	probeSum := probeScanMedia + probeScanTelephony + probeScanIntegrity + probeScanDevice + probeScanHardware + probeScanOS + probeScanLocale + probeScanNetwork
	perfExecTime := probeSum*randInt(8, 14) + randInt(20, 80)

	dynEval, _ := EncryptAndGenerateFingerprint(
		sessionID,
		map[string]any{
			"request_id":                    requestID,
			"device_ip":                     ip,
			"is_battery_capacity_validated": false,
		},
		false,
	)

	return sessionID, Fingerprint{
		AndroidID:                 randHex(8),
		BuildDevice:               d.Device,
		BuildModel:                d.Model,
		BuildHardware:             d.Hardware,
		DeviceName:                fmt.Sprintf("%s %s", strings.ToUpper(d.Manufacturer[:1])+strings.ToLower(d.Manufacturer[1:]), d.Model),
		DeviceOrientation:         "Portrait",
		FreeStorage:               freeStorage,
		HasProximitySensor:        true,
		ScreenHeight:              screenHeight,
		ScreenWidth:               screenWidth,
		ScreenScale:               1,
		ScreenBrightness:          randInt(40, 200),
		PasteboardHash:            pastboardHash,
		SensorHash:                sensorHash,
		TotalStorage:              totalStorage,
		GsfID:                     randHex(8),
		IsTablet:                  false,
		AudioMuteStatus:           false,
		AudioVolumeCurrent:        randInt(5, 80),
		IsRemoteControlConnected:  false,
		RemoteControlProvider:     nil,
		IsScreenBeingMirrored:     false,
		IsClickAutomatorInstalled: false,
		EnabledAccessibilityApps:  []any{},
		InterferingApps:           []any{},
		MediaDrmID:                mediaDrmID,
		WifiMacAddress:            nil,
		WifiSsid:                  nil,
		VpnState:                  "NOT_CONNECTED",
		ProxyState:                "NOT_CONNECTED",
		ProxyAddress:              nil,
		WifiPrivateIP:             wifiIP,
		BatteryCharging:           false,
		BatteryHealth:             "GOOD",
		BatteryLevel:              battLevel,
		BatteryTemperature:        battTemp,
		BatteryVoltage:            battVoltage,
		BatteryTotalCapacity:      battCapacity,
		CPUType:                   cpuType,
		CPUCount:                  cpuCount,
		CPUSpeed:                  randInt(1200000, 2800000),
		CPUHash:                   cpuHash,
		LastBootTime:              int(lastBootSec),
		PhysicalMemory:            int(physicalMemory),
		SystemUptime:              int(uptimeSec),
		IsNfcAvailable:            true,
		IsNfcEnabled:              false,
		InputDeviceData:           inputDeviceStr(d.Manufacturer),
		PowerSource:               "NO DATA",
		RegionCountry:             strings.ToUpper(carrier.CountryCode),
		RegionLanguage:            langForCountry(tz.Country),
		RegionTimezone:            tz.Offset,
		TimezoneIdentifier:        tz.Identifier,
		AndroidVersion:            fmt.Sprintf("%d (%s)", d.VersionSDK, d.VersionRelease),
		IsRooted:                  false,
		KernelArch:                kernelArch,
		KernelName:                "Linux",
		KernelVersion:             kernelVersionForSDK(d.VersionSDK),
		UsbCableState:             "USB_DISCONNECTED",
		UsbDebuggingState:         "USB_DEBUGGING_DISABLED",
		BootloaderState:           "BOOTLOADER_STATE_LOCKED",
		DeveloperOptionsState:     "DEV_OPTIONS_DISABLED",
		Timestamp:                 nowMs,
		CarrierName:               carrier.Name,
		CarrierCountry:            strings.ToUpper(carrier.CountryCode),
		DeviceID:                  deviceID,
		NetworkConfig:             "WIFI",
		IsOnCall:                  false,
		SimSlotCount:              randChoice([]int{1, 1, 1, 1, 2}),
		DeviceLocation:            nil,
		GeolocationConfig: GeolocationConfig{
			Enabled:     false,
			TimeoutMs:   3000,
			Prefetch:    true,
			MaxCacheAge: 600,
		},
		LocationTimestamp:     -1,
		BiometricStatus:       randChoice([]string{"ENROLLED", "NOT_ENROLLED", "NOT_ENROLLED", "DISABLED"}),
		IsKeyguardSecure:      true,
		FirstAPILevel:         d.VersionSDK,
		IsEspressoConnected:   false,
		IsHiddenapiModified:   nil,
		ProfileAdministrators: []any{},
		IsAppCloned:           false,
		PackageName:           "com.whatnot_mobile",
		PackageSignatures:     []string{pkgSig}, // KEEP IN MIND
		AppGUID:               appGUID,
		BuildID:               d.BuildID,
		BuildManufacturer:     strings.ToUpper(d.Manufacturer[:1]) + strings.ToLower(d.Manufacturer[1:]),
		BuildNumber:           d.BuildID,
		BuildTime:             int(buildTime),
		SessionID:             string(sessionID),
		Source:                "ios-5.6.3",
		ExperimentalFields: ExperimentalFields{
			SensorsBitmask:                 sensorBitmask,
			IsSecureNfcAvailable:           true,
			CalculatedTotalBatteryCapacity: battCapacity / 250,
			SupportedAbis:                  supportedABIs,
			IsSecureNfcEnabled:             false,
			GlesVersion:                    randChoice([]string{"3.0", "3.1", "3.1", "3.2"}),
			IsRootedRootbeer:               false,
			NativeCrashBit:                 "0",
			IsDebuggerConnected:            false,
			LdPreloadString:                "",
			IsRootedNative:                 false,
			IsSafetynetfixLoaded:           false,
			IsOemUnlockAllowed:             false,
			VbmetaDigest:                   vbmetaDigest,
			IsMagiskByMount:                false,
			MountFilesModifyInfo:           mountInfo,
			Permissions: shufflePermissions([]string{
				"INTERNET: 1", "ACCESS_NETWORK_STATE: 1", "BLUETOOTH_SCAN: 0",
				"BLUETOOTH_CONNECT: 0", "AD_ID: 1", "READ_PHONE_STATE: 0",
				"RECORD_AUDIO: 0", "CAMERA: 0", "MODIFY_AUDIO_SETTINGS: 1",
				"BLUETOOTH: 1", "ACCESS_WIFI_STATE: 1", "READ_CALENDAR: 0",
				"WRITE_CALENDAR: 0", "VIBRATE: 1", "READ_CONTACTS: 0",
				"ACCESS_COARSE_LOCATION: 0", "ACCESS_FINE_LOCATION: 0",
				"WAKE_LOCK: 1", "RECEIVE_BOOT_COMPLETED: 1", "FOREGROUND_SERVICE: 1",
				"BIND_GET_INSTALL_REFERRER_SERVICE: 1", "ACCESS_ADSERVICES_ATTRIBUTION: 1",
				"ACCESS_ADSERVICES_AD_ID: 1", "POST_NOTIFICATIONS: 0", "RECEIVE: 1",
				"READ_GSERVICES: 1", "DYNAMIC_RECEIVER_NOT_EXPORTED_PERMISSION: 1",
				"READ_PHONE_NUMBERS: 0", "USE_BIOMETRIC: 1", "USE_FINGERPRINT: 1",
				"CHANGE_NETWORK_STATE: 1", "CHECK_LICENSE: 1",
			}),
			Wrappers:                  []any{},
			IsLocationMockingEnabled:  false,
			RunningFingerprints:       1,
			PageSize:                  4096,
			CurrentActiveAudioDevice:  2,
			DisplayFlags:              fmt.Sprintf("Built-in Screen(0):%d;", randInt(100, 200)),
			MediaDrmIDList:            []string{fmt.Sprintf("0,%s", mediaDrmID)},
			IsSpeakerphoneActive:      false,
			CodecHash:                 codecHash,
			WifiGw:                    wifiGW,
			NwIfList:                  buildNwIfList(d.Manufacturer),
			Line1Number:               nil,
			IsSmsCapable:              true,
			SimOperatorName:           []string{carrier.Name},
			DefaultSmsPackage:         smsPackageForManufacturer(d.Manufacturer),
			TypeAllocationCode:        tacForManufacturer(d.Manufacturer),
			GsmRoamingState:           "false",
			DataRoamingStateProp:      "false",
			SimState:                  "Ready",
			MmsUserAgent:              nil,
			SimOperatorCode:           []string{carrier.OperatorCode},
			IsNetworkRoaming:          []bool{false},
			MmsUaProfURL:              nil,
			IsOnCallPerm:              false,
			IsInManagedCall:           nil,
			VoicemailNumber:           nil,
			PhoneNumber:               nil,
			IsInCall:                  nil,
			SimSpecificCarrierIDName:  []string{carrier.CarrierIDName},
			DataRoamingState:          []string{roamingState(tz.Country)},
			SimSpecificCarrierID:      []string{carrier.CarrierID},
			IsCallActiveByAudio:       false,
			SimOperatorCountry:        []string{carrier.CountryCode},
			CarrierOperatorCode:       []string{carrier.OperatorCode},
			AttestKmVersion:           attestKmVer,
			AttestOsVersion:           sdkToOSVersion(d.VersionSDK),
			AttestKmLevel:             attestKmLevel,
			AttestChallenge:           attestChallenge,
			AttestCertSignature:       randBase64(64),
			AttestSwTags:              []int{1, 2, 3, 5, 10, 400, 401, 402, 503, 701, 702, 704, 705, 706, 709, 718, 719},
			IsSystemUser:              true,
			InstrClass:                "Instrumentation",
			AttestTime:                attestTime,
			IsApplistoDetected:        false,
			IsPackageinfoInconsistent: false,
			AttestCertIds:             []string{certID1, certID2, "1"},
			FrdScan: FrdScan{
				F:  []int{frdVal},
				Ws: []int{frdVal},
			},
			IsWorkProfile: false,
			LvlInf: LvlInf{
				H: d.VersionSDK,
				M: buildLvlInfM(d.VersionSDK),
			},
			AttestSignatures:           []string{pkgSig},
			AttestVerBootHash:          bootHash,
			AttestPackages:             []string{"com.whatnot_mobile"},
			AttestationCapability:      "SUPPORTED",
			IsRootedByProp:             false,
			AttestIsDeviceUnlocked:     attestIsUnlocked,
			AttestSecLevel:             attestSecLevel,
			AttestHwTags:               nil,
			AttestVerBootState:         attestBootState,
			AttestVersion:              attestVer,
			AttestRtErrorFlag:          false,
			AttestVerBootKey:           bootKey,
			AttestOsPatchLevel:         sdkToPatchLevel(d.VersionSDK),
			AttestLogs:                 []any{},
			CameraApps:                 cameraAppsForManufacturer(d.Manufacturer),
			ExperimentalDeviceLocation: nil,
		},
		PerformanceProbes:       probes,
		PerformanceEmulator:     0,
		RequestID:               requestID,
		DynamicEvaluatorResults: dynEval,
		PerformanceNetworkCall:  randInt(180, 400),
		IsEmulator:              false,
		EmulatorTriggerCauses:   []string{},
		PredictedEmulatorEngine: "",
		VersionCode:             "02060800",
		PerformanceExecTime:     perfExecTime,
		DeviceHash:              deviceHash,
		PortScanResult:          []string{},
		BehaviourResults: map[string]any{
			"acc": map[string]any{
				"sc": randInt(1, 5), "bc": randInt(0, 3),
				"topDiffs": accTopDiffs,
				"sample":   accSamples,
			},
			"duration": randInt(8000, 45000),
			"mag": map[string]any{
				"min": magMin, "max": magMax, "med": magMed, "p95": magP95,
				"sample": magSamples,
			},
			"light": map[string]any{
				"min": -1, "max": -1, "med": -1, "p95": -1,
				"sample": 0,
			},
			"prox": map[string]any{
				"elems": []any{}, "sample": randInt(5, 20),
			},
			"gyro": map[string]any{
				"sc": randInt(0, 3), "bc": randInt(0, 2),
				"topDiffs": gyroTopDiffs,
				"sample":   gyroSamples,
			},
		},
		StoredErrors: []string{},
		CommErrors:   []any{},
	}
}
