package engine

import (
	"context"
	"math/rand"
	"time"
)

func (e *Engine) staggerWorkerStart(ctx context.Context, workerIndex int, concurrency int) error {
	if concurrency <= 1 || workerIndex <= 0 {
		return nil
	}
	delay := e.threadStartCooldown()
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(workerIndex) * delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) threadStartCooldown() time.Duration {
	if e == nil || e.cfg == nil {
		return 5 * time.Second
	}
	minSecs, maxSecs, ok := e.cfg.Delays.Range("thread_start_cooldown")
	if !ok {
		return 5 * time.Second
	}
	if maxSecs < minSecs {
		minSecs, maxSecs = maxSecs, minSecs
	}
	chosen := minSecs
	if spread := maxSecs - minSecs; spread > 0 {
		chosen = minSecs + rand.Float64()*spread
	}
	if chosen <= 0 {
		return 0
	}
	return time.Duration(chosen * float64(time.Second))
}
