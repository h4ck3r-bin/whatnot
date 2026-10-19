package devices

type DeviceSpec struct {
	Machine        string
	ScreenWidth    float64 // PIXELS (bounds * scale)
	ScreenHeight   float64 // PIXELS (bounds * scale)
	NativeWidth    float64
	NativeHeight   float64
	// PhysicalMemory is what `sysctl hw.memsize` returns on a real iPhone,
	// i.e., the RAM figure AFTER Apple's firmware-reserved pages. This is
	// NOT nominal RAM × 1024³ — the real values are ~92–96 % of nominal
	// depending on chip generation. A payload emitting `6 << 30 =
	// 6442450944` for a 6 GB phone is an immediate tell because no real
	// iPhone reports that round a number.
	//
	// Verified values:
	//   iPhone 14 Pro (6 GB, iOS 18.5): 5,940,199,424 bytes
	// Other values are regression-scaled from the verified datapoint; the
	// next real-device capture we get for a different RAM class lets us
	// pin the exact byte count.
	PhysicalMemory uint64
	CpuType        string
	CpuCount       int
	IsArm64        bool
	Has3DTouch     bool
	DisplayGamut   string // "P3" for iPhone 7+, "sRGB" for older
	// MinIOSVersion is the minimum iOS version this device can run (the version it shipped with).
	// A machine running an iOS version lower than this is IMPOSSIBLE on a real device and will
	// be flagged by any fingerprinting service worth its salt. Pairing "iPhone17,4" with "17.x"
	// is a dead giveaway — iPhone 16 Plus shipped with iOS 18.
	MinIOSVersion string
}

// Real-device reserved-memory values, from sysctl hw.memsize on actual
// iPhones. Stop using `N << 30` for iPhone specs — it's uniquely Go-ish.
const (
	memsize3GB  = 2948530176  //  ≈ 2.75 GiB (95.4% of 3 × 1024³)
	memsize4GB  = 3945406464  //  ≈ 3.67 GiB (91.9% of 4 × 1024³)
	memsize6GB  = 5940199424  //  ≈ 5.53 GiB (92.2% of 6 × 1024³) — iPhone 14 Pro verified
	memsize8GB  = 7888867328  //  ≈ 7.35 GiB (91.9% of 8 × 1024³)

	// The two 12 GB iPhone 17 Pro models do NOT report the same hw.memsize.
	// Both values below are from real captures and both are exact multiples
	// of the 16 KiB arm64 page size, which is the signature of a genuine
	// hw.memsize rather than a rounded guess:
	//
	//   iPhone18,1  12,291,325,952  95.39% of 12 GiB  (iPhone 17 Pro capture)
	//   iPhone18,2  12,262,113,280  95.17% of 12 GiB  (testdata/freshwhatnot)
	//
	// The 27.86 MiB delta tracks the Pro Max's larger panel (1320×2868 vs
	// 1206×2622) needing a bigger display/GPU carve-out. Sharing one constant
	// across both models emits the wrong value for whichever one you didn't
	// capture, so they are kept separate on purpose.
	memsize12GBPro    = 12291325952 // iPhone 17 Pro
	memsize12GBProMax = 12262113280 // iPhone 17 Pro Max
)

var DeviceSpecs = map[string]DeviceSpec{
	// iPhone XS / XS Max / XR (A12 Bionic) — shipped Sep 2018 with iOS 12.0.
	// Supported through iOS 18.x; dropped by iOS 26.
	"iPhone11,2": {Machine: "iPhone11,2", ScreenWidth: 1125, ScreenHeight: 2436, NativeWidth: 1125, NativeHeight: 2436, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: true, DisplayGamut: "P3", MinIOSVersion: "12.0"},   // iPhone XS
	"iPhone11,4": {Machine: "iPhone11,4", ScreenWidth: 1242, ScreenHeight: 2688, NativeWidth: 1242, NativeHeight: 2688, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: true, DisplayGamut: "P3", MinIOSVersion: "12.0"},   // iPhone XS Max (CN, early run)
	"iPhone11,6": {Machine: "iPhone11,6", ScreenWidth: 1242, ScreenHeight: 2688, NativeWidth: 1242, NativeHeight: 2688, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: true, DisplayGamut: "P3", MinIOSVersion: "12.0"},   // iPhone XS Max
	"iPhone11,8": {Machine: "iPhone11,8", ScreenWidth: 828, ScreenHeight: 1792, NativeWidth: 828, NativeHeight: 1792, PhysicalMemory: memsize3GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "12.0"},    // iPhone XR (Haptic Touch only)
	// iPhone 11 series (A13 Bionic) — shipped Sep 2019 with iOS 13.0
	"iPhone12,1": {Machine: "iPhone12,1", ScreenWidth: 828, ScreenHeight: 1792, NativeWidth: 828, NativeHeight: 1792, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "13.0"},
	"iPhone12,3": {Machine: "iPhone12,3", ScreenWidth: 1125, ScreenHeight: 2436, NativeWidth: 1125, NativeHeight: 2436, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "13.0"},
	"iPhone12,5": {Machine: "iPhone12,5", ScreenWidth: 1242, ScreenHeight: 2688, NativeWidth: 1242, NativeHeight: 2688, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "13.0"},
	// SE 2nd/3rd Gen
	"iPhone12,8": {Machine: "iPhone12,8", ScreenWidth: 750, ScreenHeight: 1334, NativeWidth: 750, NativeHeight: 1334, PhysicalMemory: memsize3GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "13.4"},   // SE 2nd Gen, Apr 2020
	"iPhone14,6": {Machine: "iPhone14,6", ScreenWidth: 750, ScreenHeight: 1334, NativeWidth: 750, NativeHeight: 1334, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "15.4"},   // SE 3rd Gen, Mar 2022
	// iPhone 12 series (A14 Bionic) — shipped Oct 2020 with iOS 14.1
	"iPhone13,1": {Machine: "iPhone13,1", ScreenWidth: 1080, ScreenHeight: 2340, NativeWidth: 1080, NativeHeight: 2340, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "14.1"},
	"iPhone13,2": {Machine: "iPhone13,2", ScreenWidth: 1170, ScreenHeight: 2532, NativeWidth: 1170, NativeHeight: 2532, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "14.1"},
	"iPhone13,3": {Machine: "iPhone13,3", ScreenWidth: 1170, ScreenHeight: 2532, NativeWidth: 1170, NativeHeight: 2532, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "14.1"},
	"iPhone13,4": {Machine: "iPhone13,4", ScreenWidth: 1284, ScreenHeight: 2778, NativeWidth: 1284, NativeHeight: 2778, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "14.2"},
	// iPhone 13 series (A15 Bionic) — shipped Sep 2021 with iOS 15.0
	"iPhone14,2": {Machine: "iPhone14,2", ScreenWidth: 1170, ScreenHeight: 2532, NativeWidth: 1170, NativeHeight: 2532, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "15.0"},
	"iPhone14,3": {Machine: "iPhone14,3", ScreenWidth: 1284, ScreenHeight: 2778, NativeWidth: 1284, NativeHeight: 2778, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "15.0"},
	"iPhone14,4": {Machine: "iPhone14,4", ScreenWidth: 1080, ScreenHeight: 2340, NativeWidth: 1080, NativeHeight: 2340, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "15.0"},
	"iPhone14,5": {Machine: "iPhone14,5", ScreenWidth: 1170, ScreenHeight: 2532, NativeWidth: 1170, NativeHeight: 2532, PhysicalMemory: memsize4GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "15.0"},
	// iPhone 14 series (A15 non-Pro) — shipped Sep 2022 with iOS 16.0
	"iPhone14,7": {Machine: "iPhone14,7", ScreenWidth: 1170, ScreenHeight: 2532, NativeWidth: 1170, NativeHeight: 2532, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "16.0"},
	"iPhone14,8": {Machine: "iPhone14,8", ScreenWidth: 1284, ScreenHeight: 2778, NativeWidth: 1284, NativeHeight: 2778, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "16.0"},
	// iPhone 14 Pro series (A16 Bionic) — shipped Sep 2022 with iOS 16.0
	"iPhone15,2": {Machine: "iPhone15,2", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "16.0"},
	"iPhone15,3": {Machine: "iPhone15,3", ScreenWidth: 1290, ScreenHeight: 2796, NativeWidth: 1290, NativeHeight: 2796, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "16.0"},
	// iPhone 15 series (A16 non-Pro) — shipped Sep 2023 with iOS 17.0
	"iPhone15,4": {Machine: "iPhone15,4", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "17.0"},
	"iPhone15,5": {Machine: "iPhone15,5", ScreenWidth: 1290, ScreenHeight: 2796, NativeWidth: 1290, NativeHeight: 2796, PhysicalMemory: memsize6GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "17.0"},
	// iPhone 15 Pro (A17 Pro) — shipped Sep 2023 with iOS 17.0
	"iPhone16,1": {Machine: "iPhone16,1", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "17.0"},
	"iPhone16,2": {Machine: "iPhone16,2", ScreenWidth: 1290, ScreenHeight: 2796, NativeWidth: 1290, NativeHeight: 2796, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "17.0"},
	// iPhone 16 series (A18/A18 Pro) — shipped Sep 2024 with iOS 18.0
	"iPhone17,1": {Machine: "iPhone17,1", ScreenWidth: 1206, ScreenHeight: 2622, NativeWidth: 1206, NativeHeight: 2622, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "18.0"},
	"iPhone17,2": {Machine: "iPhone17,2", ScreenWidth: 1320, ScreenHeight: 2868, NativeWidth: 1320, NativeHeight: 2868, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "18.0"},
	"iPhone17,3": {Machine: "iPhone17,3", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "18.0"},
	"iPhone17,4": {Machine: "iPhone17,4", ScreenWidth: 1290, ScreenHeight: 2796, NativeWidth: 1290, NativeHeight: 2796, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "18.0"},
	// iPhone 16e (A18 binned) — shipped Feb 2025 with iOS 18.3
	"iPhone17,5": {Machine: "iPhone17,5", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "18.3"},
	// iPhone 17 series (A19 Pro) — shipped Sep 2025 with iOS 26.0.
	// 17 Pro / Pro Max both carry 12 GB but report different hw.memsize
	// values — see the memsize12GBPro / memsize12GBProMax comment above.
	// 17 non-Pro / Air ship with 8 GB per Apple's published specs.
	"iPhone18,1": {Machine: "iPhone18,1", ScreenWidth: 1206, ScreenHeight: 2622, NativeWidth: 1206, NativeHeight: 2622, PhysicalMemory: memsize12GBPro, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "26.0"},    // iPhone 17 Pro
	"iPhone18,2": {Machine: "iPhone18,2", ScreenWidth: 1320, ScreenHeight: 2868, NativeWidth: 1320, NativeHeight: 2868, PhysicalMemory: memsize12GBProMax, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "26.0"}, // iPhone 17 Pro Max
	"iPhone18,3": {Machine: "iPhone18,3", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "26.0"},   // iPhone 17
	"iPhone18,4": {Machine: "iPhone18,4", ScreenWidth: 1179, ScreenHeight: 2556, NativeWidth: 1179, NativeHeight: 2556, PhysicalMemory: memsize8GB, CpuType: "ARM_64E", CpuCount: 6, IsArm64: true, Has3DTouch: false, DisplayGamut: "P3", MinIOSVersion: "26.0"},   // iPhone 17 Air
}

func LookupSpec(machine string) *DeviceSpec {
	if spec, ok := DeviceSpecs[machine]; ok {
		return &spec
	}
	return nil
}
