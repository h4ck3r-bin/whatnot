package whtnt

import (
	"fmt"
	"strings"
)

func (m *Module) checkpointDone(key string) bool {
	if m == nil {
		return false
	}
	key = normalizeCheckpointKey(key)
	if key == "" || m.completedRequests == nil {
		return false
	}
	return m.completedRequests[key]
}

func (m *Module) markCheckpointDone(key string) {
	if m == nil {
		return
	}
	key = normalizeCheckpointKey(key)
	if key == "" {
		return
	}
	if m.completedRequests == nil {
		m.completedRequests = map[string]bool{}
	}
	m.completedRequests[key] = true
	m.autosaveSessionIfChanged("checkpoint " + key)
}

func (m *Module) clearCheckpointsWithPrefix(prefix string) {
	if m == nil || m.completedRequests == nil {
		return
	}
	prefix = normalizeCheckpointKey(prefix)
	if prefix == "" {
		return
	}
	changed := false
	for key := range m.completedRequests {
		if strings.HasPrefix(key, prefix) {
			delete(m.completedRequests, key)
			changed = true
		}
	}
	if changed {
		m.autosaveSessionIfChanged("clear checkpoints " + prefix)
	}
}

// runCheckpointedRequest runs the step only if its checkpoint is not already
// complete. The returned bool reports whether run() actually executed this call
// (true) versus being skipped because the checkpoint was already done (false),
// so callers can avoid re-firing side effects such as analytics events on a
// resumed run.
func (m *Module) runCheckpointedRequest(key string, run func() error) (bool, error) {
	key = normalizeCheckpointKey(key)
	if key == "" {
		return true, run()
	}
	if m.checkpointDone(key) {
		if m.log != nil {
			m.log.Manager("Skipping completed checkpoint: " + key)
		}
		return false, nil
	}
	if err := run(); err != nil {
		return false, err
	}
	m.markCheckpointDone(key)
	return true, nil
}

func (m *Module) checkpointedBestEffort(key string, run func()) {
	key = normalizeCheckpointKey(key)
	if key == "" {
		run()
		return
	}
	if m.checkpointDone(key) {
		return
	}
	run()
	m.markCheckpointDone(key)
}

func checkoutProductCheckpoint(prefix string, product Product) string {
	return fmt.Sprintf("%s:%s:%d", prefix, strings.TrimSpace(product.ProductListingID), product.ProductPrice)
}

func normalizeCheckpointKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.ReplaceAll(key, " ", "_")
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	return key
}
