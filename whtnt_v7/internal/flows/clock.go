package flows

import (
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
)

func flowNow() time.Time {
	cfg, err := appconfig.Load()
	if err != nil {
		return time.Now()
	}
	location, err := time.LoadLocation(cfg.Engine.Timezone)
	if err != nil {
		return time.Now()
	}
	return time.Now().In(location)
}
