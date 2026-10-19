package devices

import (
	"math/rand"
	"sort"
	"strings"
)

// PickRealisticIOSVersion returns a random iOS version compatible with the
// given machine, weighted toward the LATEST patch of the LATEST major release
// the device can run — matching real-world Apple iPhone population.
//
// Why this exists: PickCompatibleIOSVersion uses uniform-random selection
// across the full IOSBuildNumbers catalog, which over-represents intermediate
// patches that real users have auto-updated past. Empirical evidence from a
// 2026-04-30 SEON run shows that all banned fingerprints had non-latest patch
// versions (18.6, 18.6.2, 18.7.1, 18.7.3) while the 17 unbanned ones almost
// all reported 18.7.6 / 18.7.7 / 26.x. SEON tightened its iOS-version risk
// signal that day; uniform-random selection became a reliable tell.
//
// Selection weights (population-realistic):
//
//   - 60% — LATEST patch of the LATEST major branch the device supports
//     (e.g. "26.4.1" for an iPhone 16 Pro, "18.7.7" for an iPhone XR
//     that can't run iOS 26)
//   - 25% — any patch of the LATEST major branch (within last 3 releases)
//   - 12% — any patch of the previous major branch (last 3 releases)
//   - 3%  — any other compatible version (legacy / opt-out users)
//
// minIOSVersion is the caller-supplied lower bound (pass "" to skip). The
// machine's MinIOSVersion is always enforced — picks below the device's
// shipping iOS would be physically impossible and an immediate giveaway.
//
// Example:
//
//	machine, _ := devices.RandomMachine(), ""
//	v := devices.PickRealisticIOSVersion(machine, "")
func PickRealisticIOSVersion(machine, minIOSVersion string) string {
	versions := CompatibleIOSVersions(machine, minIOSVersion)
	if len(versions) == 0 {
		return ""
	}
	tiers := classifyByTier(versions)
	// Pick a tier by weight, fall back to next-richer tier when empty.
	r := rand.Float64()
	switch {
	case r < 0.60 && len(tiers.LatestPatch) > 0:
		return tiers.LatestPatch[rand.Intn(len(tiers.LatestPatch))]
	case r < 0.85 && len(tiers.LatestMajor) > 0:
		return tiers.LatestMajor[rand.Intn(len(tiers.LatestMajor))]
	case r < 0.97 && len(tiers.PrevMajor) > 0:
		return tiers.PrevMajor[rand.Intn(len(tiers.PrevMajor))]
	default:
		return tiers.Other[rand.Intn(len(tiers.Other))]
	}
}

// versionTiers groups iOS versions by how recently Apple shipped them, used
// by PickRealisticIOSVersion. The slices may overlap-by-promotion: every
// version always lands in the lowest-priority slice that fits, not duplicated.
type versionTiers struct {
	LatestPatch []string // latest patch of latest major (highest weight)
	LatestMajor []string // any patch of latest major's last 3 releases
	PrevMajor   []string // any patch of previous major's last 3 releases
	Other       []string // everything else compatible
}

// classifyByTier groups the given compatible iOS versions into the four
// recency buckets described above. Inputs are taken verbatim from
// CompatibleIOSVersions (already filtered to the machine's MinIOSVersion).
func classifyByTier(compatible []string) versionTiers {
	if len(compatible) == 0 {
		return versionTiers{}
	}
	// Find the highest major present in the compatible set, then the
	// highest patch within that major.
	highestMajor := majorOf(compatible[0])
	for _, v := range compatible {
		if m := majorOf(v); m > highestMajor {
			highestMajor = m
		}
	}
	prevMajor := highestMajor - 1
	// Find the latest patch within highestMajor.
	latestInLatest := ""
	for _, v := range compatible {
		if majorOf(v) != highestMajor {
			continue
		}
		if latestInLatest == "" || CompareIOSVersions(v, latestInLatest) > 0 {
			latestInLatest = v
		}
	}
	// Sort for deterministic recency picks.
	latestMajor := versionsForMajor(compatible, highestMajor)
	sort.Slice(latestMajor, func(i, j int) bool {
		return CompareIOSVersions(latestMajor[i], latestMajor[j]) > 0
	})
	prevMajorList := versionsForMajor(compatible, prevMajor)
	sort.Slice(prevMajorList, func(i, j int) bool {
		return CompareIOSVersions(prevMajorList[i], prevMajorList[j]) > 0
	})
	// Trim each "recent" tier to the last 3 releases — anything older falls
	// into the long tail. Three covers the typical 2-3 active security
	// branches Apple maintains at any one time.
	const recentN = 3
	if len(latestMajor) > recentN {
		latestMajor = latestMajor[:recentN]
	}
	if len(prevMajorList) > recentN {
		prevMajorList = prevMajorList[:recentN]
	}

	tiers := versionTiers{}
	if latestInLatest != "" {
		tiers.LatestPatch = []string{latestInLatest}
	}
	for _, v := range latestMajor {
		if v == latestInLatest {
			continue
		}
		tiers.LatestMajor = append(tiers.LatestMajor, v)
	}
	for _, v := range prevMajorList {
		tiers.PrevMajor = append(tiers.PrevMajor, v)
	}
	// Other: everything not already in a recency tier.
	in := map[string]bool{latestInLatest: true}
	for _, v := range tiers.LatestMajor {
		in[v] = true
	}
	for _, v := range tiers.PrevMajor {
		in[v] = true
	}
	for _, v := range compatible {
		if !in[v] {
			tiers.Other = append(tiers.Other, v)
		}
	}
	return tiers
}

func majorOf(version string) int {
	idx := strings.IndexByte(version, '.')
	if idx < 0 {
		idx = len(version)
	}
	n := 0
	for _, c := range version[:idx] {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func versionsForMajor(versions []string, major int) []string {
	out := make([]string, 0, 8)
	for _, v := range versions {
		if majorOf(v) == major {
			out = append(out, v)
		}
	}
	return out
}

// RandomMachineAndRealisticIOS is the population-weighted counterpart to
// RandomMachineAndIOS — picks a random machine and a population-realistic iOS
// version it can run. Pass "" for minIOSVersion to apply only the machine's
// MinIOSVersion bound.
//
// Use this in fingerprint generation pipelines that emit accounts at scale —
// uniform iOS-version distribution is a reliable population-statistics tell
// that risk models score against.
func RandomMachineAndRealisticIOS(minIOSVersion string) (machine, iosVersion string) {
	for attempt := 0; attempt < 256; attempt++ {
		machine = RandomMachine()
		iosVersion = PickRealisticIOSVersion(machine, minIOSVersion)
		if iosVersion != "" {
			return
		}
	}
	return "", ""
}
