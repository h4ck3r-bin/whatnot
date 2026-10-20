package whtnt

import (
	"fmt"
	"math/rand"
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
)

// sleepDelay sleeps a random duration in seconds from setup/delays.json.
// range [min, max]. If the name is missing or invalid, no sleep happens.
func (m *Module) sleepDelay(name string) {
	if m.skipDelays {
		return
	}
	cfg := m.config
	if cfg == nil {
		loaded, err := appconfig.Load()
		if err != nil {
			if m.log != nil {
				m.log.ManagerError("Failed to load delay config: " + err.Error())
			}
			return
		}
		cfg = loaded
	}

	minSecs, maxSecs, ok := cfg.Delays.Range(name)
	if !ok {
		return
	}
	if minSecs <= 0 && maxSecs <= 0 {
		return
	}

	spread := maxSecs - minSecs
	chosen := minSecs
	if spread > 0 {
		chosen = minSecs + rand.Float64()*spread
	}
	if chosen <= 0 {
		return
	}

	dur := time.Duration(chosen * float64(time.Second))
	if m.log != nil {
		m.log.Manager(fmt.Sprintf("Sleeping %.2fs (delay: %s)", chosen, name))
	}
	time.Sleep(dur)
}
