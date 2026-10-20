package whtnt

import (
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

var kasadaGlobalBackoff = struct {
	mu                 sync.Mutex
	consecutiveFailure int
	blockUntil         time.Time
}{}

func waitKasadaGlobalBackoff() {
	for {
		kasadaGlobalBackoff.mu.Lock()
		wait := time.Until(kasadaGlobalBackoff.blockUntil)
		kasadaGlobalBackoff.mu.Unlock()
		if wait <= 0 {
			return
		}
		time.Sleep(wait)
	}
}

func recordKasadaSolveResult(success bool) {
	kasadaGlobalBackoff.mu.Lock()
	defer kasadaGlobalBackoff.mu.Unlock()
	if success {
		kasadaGlobalBackoff.consecutiveFailure = 0
		kasadaGlobalBackoff.blockUntil = time.Time{}
		return
	}
	kasadaGlobalBackoff.consecutiveFailure++
	if kasadaGlobalBackoff.consecutiveFailure >= 2 {
		kasadaGlobalBackoff.blockUntil = time.Now().Add(30 * time.Second)
		kasadaGlobalBackoff.consecutiveFailure = 0
		logger.ManagerError("Kasada failed twice in a row. Pausing all Kasada solves for 30 seconds.")
	}
}
