package engine

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

func (e *Engine) sleepDelay(ctx context.Context, name string) error {
	if e == nil || e.cfg == nil {
		return nil
	}
	minSecs, maxSecs, ok := e.cfg.Delays.Range(name)
	if !ok || (minSecs <= 0 && maxSecs <= 0) {
		return nil
	}
	if maxSecs < minSecs {
		minSecs, maxSecs = maxSecs, minSecs
	}

	chosen := minSecs
	if spread := maxSecs - minSecs; spread > 0 {
		chosen = minSecs + rand.Float64()*spread
	}
	if chosen <= 0 {
		return nil
	}

	delay := time.Duration(chosen * float64(time.Second))
	logger.Manager(fmt.Sprintf("Sleeping %.2fs (delay: %s)", chosen, name))
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
