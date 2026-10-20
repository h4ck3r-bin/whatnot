package devices

import (
	"math/rand"
	"strings"
)

type MetalGPUInfo struct {
	GPUName                  string
	GPUArchitecture          string
	GPUFamily                int
	GPUHasUnifiedMemory      bool
	GPUMaxThreadgroupMemory  int
	GPUMaxThreadsWidth       int
	GPUMaxThreadsHeight      int
	GPUMaxThreadsDepth       int
	GPURWTextureTier         int
	GPUSupportsRaytracing    bool
}

var gpuByChip = map[string]MetalGPUInfo{
	// A12 Bionic
	"T8020": {GPUName: "Apple A12 GPU", GPUArchitecture: "applegpu_g11p", GPUFamily: 5, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 512, GPUMaxThreadsHeight: 512, GPUMaxThreadsDepth: 512, GPURWTextureTier: 2, GPUSupportsRaytracing: false},
	// A13 Bionic
	"T8030": {GPUName: "Apple A13 GPU", GPUArchitecture: "applegpu_g12p", GPUFamily: 6, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 512, GPUMaxThreadsHeight: 512, GPUMaxThreadsDepth: 512, GPURWTextureTier: 2, GPUSupportsRaytracing: false},
	// A14 Bionic
	"T8101": {GPUName: "Apple A14 GPU", GPUArchitecture: "applegpu_g13p", GPUFamily: 7, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 512, GPUMaxThreadsHeight: 512, GPUMaxThreadsDepth: 512, GPURWTextureTier: 2, GPUSupportsRaytracing: false},
	// A15 Bionic
	"T8110": {GPUName: "Apple A15 GPU", GPUArchitecture: "applegpu_g14p", GPUFamily: 8, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 1024, GPUMaxThreadsHeight: 1024, GPUMaxThreadsDepth: 1024, GPURWTextureTier: 2, GPUSupportsRaytracing: true},
	// A16 Bionic
	"T8120": {GPUName: "Apple A16 GPU", GPUArchitecture: "applegpu_g15p", GPUFamily: 8, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 1024, GPUMaxThreadsHeight: 1024, GPUMaxThreadsDepth: 1024, GPURWTextureTier: 2, GPUSupportsRaytracing: true},
	// A17 Pro
	"T8122": {GPUName: "Apple A17 Pro GPU", GPUArchitecture: "applegpu_g16p", GPUFamily: 9, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 1024, GPUMaxThreadsHeight: 1024, GPUMaxThreadsDepth: 1024, GPURWTextureTier: 2, GPUSupportsRaytracing: true},
	// A18
	"T8130": {GPUName: "Apple A18 GPU", GPUArchitecture: "applegpu_g17p", GPUFamily: 9, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 1024, GPUMaxThreadsHeight: 1024, GPUMaxThreadsDepth: 1024, GPURWTextureTier: 2, GPUSupportsRaytracing: true},
	// A18 Pro
	"T8140": {GPUName: "Apple A18 Pro GPU", GPUArchitecture: "applegpu_g17p", GPUFamily: 9, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 1024, GPUMaxThreadsHeight: 1024, GPUMaxThreadsDepth: 1024, GPURWTextureTier: 2, GPUSupportsRaytracing: true},
	// A19 Pro
	"T8150": {GPUName: "Apple A19 Pro GPU", GPUArchitecture: "applegpu_g18p", GPUFamily: 10, GPUHasUnifiedMemory: true, GPUMaxThreadgroupMemory: 32768, GPUMaxThreadsWidth: 1024, GPUMaxThreadsHeight: 1024, GPUMaxThreadsDepth: 1024, GPURWTextureTier: 2, GPUSupportsRaytracing: true},
}

func gpuNameForMachine(machine string) string {
	if strings.HasPrefix(machine, "iPhone18,") {
		if machine == "iPhone18,1" || machine == "iPhone18,2" {
			return "Apple A19 Pro GPU"
		}
		return "Apple A19 GPU"
	}
	return ""
}

const gib = 1024 * 1024 * 1024

// maxBufferLengthForRAM returns the MTLDevice.maxBufferLength for a given
// physical memory size. Real values are exact GiB multiples derived from
// nominal RAM: 6GB nominal → 3 GiB, 8GB → 4 GiB, 12GB → 6 GiB.
// Verified: iPhone 15 (6GB) = 3221225472, iPhone 17 Pro (12GB) = 6442450944.
func maxBufferLengthForRAM(physicalMemory uint64) uint64 {
	switch {
	case physicalMemory > 10*gib:
		return 6 * gib // 12 GB nominal
	case physicalMemory > 7*gib:
		return 4 * gib // 8 GB nominal
	case physicalMemory > 5*gib:
		return 3 * gib // 6 GB nominal
	case physicalMemory > 3*gib:
		return 2 * gib // 4 GB nominal
	default:
		return 1 * gib // 3 GB nominal
	}
}

// recommendedMaxWorkingSetForRAM returns MTLDevice.recommendedMaxWorkingSetSize.
// Real values are (nominal_RAM * 2/3) in exact GiB + 16384 bytes (4 pages).
// Verified: iPhone 15 (6GB) = 4294983680, iPhone 17 Pro (12GB) = 8589950976.
func recommendedMaxWorkingSetForRAM(physicalMemory uint64) uint64 {
	const pageReserve = 16384
	switch {
	case physicalMemory > 10*gib:
		return 8*gib + pageReserve // 12 GB nominal → 8 GiB
	case physicalMemory > 7*gib:
		return 5*gib + pageReserve // 8 GB nominal → ~5.33 GiB (rounding to 5 GiB)
	case physicalMemory > 5*gib:
		return 4*gib + pageReserve // 6 GB nominal → 4 GiB
	case physicalMemory > 3*gib:
		return 3*gib + pageReserve // 4 GB nominal → ~2.67 GiB (rounding to 3 GiB)
	default:
		return 2*gib + pageReserve // 3 GB nominal → 2 GiB
	}
}

// quantizeProbeDurationMs converts a probe duration in seconds into the
// milliseconds value a real device would report.
//
// The SDK times its probes with CACurrentMediaTime(), which returns
// seconds-since-boot as a float64. Both endpoints therefore sit at the
// magnitude of system_uptime, so their difference is an exact multiple of
// ulp(uptime) — a coarse grid, not an arbitrary double. On the
// testdata/freshwhatnot device (uptime 303,620 s → ulp 2⁻³⁴ s) the captured
// duration_ms of 0.065922737121582031 is exactly 1,132,544 ulps.
//
// Picking a bare rand.Float64() lands off that grid essentially always, which
// is visible once floats are serialised at %.17g. Emulating the subtraction
// reproduces the grid and self-adjusts as uptime changes: a device up for an
// hour has a finer grid than one up for a week.
//
// A non-positive uptime falls back to a representative one-day uptime rather
// than skipping quantisation, so callers can't accidentally emit an off-grid
// value by leaving uptime unset.
//
// Caveat: the corpus holds exactly one distinct metal duration, and it is also
// exactly representable as a float32 (69125 / 2²⁰). A competing explanation is
// that the SDK rounds the value through a float rather than subtracting clock
// doubles. The grid model is the better fit for that single sample — a
// grid-derived value is float32-exact about 1 time in 16, whereas a
// float32-derived value lands on the uptime grid about 1 time in 125 — but it
// is not settled. Do NOT also round to float32: whichever model is right, the
// other property must stay incidental, or the aggregate distribution becomes
// its own tell. Revisit if more captures arrive.
func quantizeProbeDurationMs(durationSec float64, uptimeSec int64) float64 {
	const fallbackUptimeSec = 86400 // 1 day
	if uptimeSec <= 0 {
		uptimeSec = fallbackUptimeSec
	}
	base := float64(uptimeSec)
	return ((base + durationSec) - base) * 1000
}

// BuildMetalInfo assembles experimental_fields.metal_info. uptimeSec is the
// device's system_uptime, used to place duration_ms on the same clock grid a
// real device's CACurrentMediaTime() subtraction produces.
func BuildMetalInfo(machine string, physicalMemory uint64, uptimeSec int64) map[string]interface{} {
	chip := ChipForDevice(machine)
	if chip == "" {
		return nil
	}
	gpu, ok := gpuByChip[chip]
	if !ok {
		return nil
	}

	name := gpu.GPUName
	if override := gpuNameForMachine(machine); override != "" {
		name = override
	}

	maxBufferLength := maxBufferLengthForRAM(physicalMemory)
	recommendedMaxWorkingSet := recommendedMaxWorkingSetForRAM(physicalMemory)

	// Real captures show Metal probe durations in the 0.03–0.5 ms range
	// (testdata/freshwhatnot iPhone 17 Pro Max: 0.0659 ms; iPhone XR: 3.9 ms).
	// Older GPUs (family ≤ 6) are slower; newer ones (family ≥ 7) finish
	// faster. The band is sampled in seconds and then snapped to the
	// CACurrentMediaTime grid — see quantizeProbeDurationMs.
	var durationSec float64
	if gpu.GPUFamily >= 7 {
		durationSec = (0.03 + rand.Float64()*0.15) / 1000 // 0.03–0.18 ms
	} else {
		durationSec = (0.5 + rand.Float64()*4.0) / 1000 // 0.5–4.5 ms
	}
	durationMs := quantizeProbeDurationMs(durationSec, uptimeSec)

	return map[string]interface{}{
		"duration_ms":                            durationMs,
		"gpu_name":                               name,
		"gpu_architecture":                       gpu.GPUArchitecture,
		"gpu_argument_buffers_tier":              1, // MTLArgumentBuffersTier.tier2.rawValue — all A12+ devices
		"gpu_family":                             gpu.GPUFamily,
		"gpu_has_unified_memory":                 gpu.GPUHasUnifiedMemory,
		"gpu_max_buffer_length":                  maxBufferLength,
		"gpu_max_threadgroup_memory":             gpu.GPUMaxThreadgroupMemory,
		"gpu_max_threads_per_threadgroup_width":  gpu.GPUMaxThreadsWidth,
		"gpu_max_threads_per_threadgroup_height": gpu.GPUMaxThreadsHeight,
		"gpu_max_threads_per_threadgroup_depth":  gpu.GPUMaxThreadsDepth,
		"gpu_recommended_max_working_set":        recommendedMaxWorkingSet,
		"gpu_rw_texture_tier":                    gpu.GPURWTextureTier,
		"gpu_supports_raytracing":                gpu.GPUSupportsRaytracing,
	}
}
