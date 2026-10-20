package whtnt

import (
	"fmt"
	"strings"
	"time"
)

const sessionRefreshMaxAge = 2 * time.Minute

func (m *Module) ensureSessionFreshForAction(action string) error {
	if m == nil {
		return nil
	}
	m.sessionRefreshMu.Lock()
	defer m.sessionRefreshMu.Unlock()
	if m.refreshingSession {
		return nil
	}
	if strings.TrimSpace(m.session.sessionID) == "" ||
		strings.TrimSpace(m.session.appSessionID) == "" ||
		strings.TrimSpace(m.session.requestID) == "" {
		return nil
	}
	if !m.lastSessionRefreshAt.IsZero() && time.Since(m.lastSessionRefreshAt) < sessionRefreshMaxAge {
		return nil
	}

	if m.log != nil {
		m.log.Manager("Refreshing session before " + firstNonEmptyString(strings.TrimSpace(action), "next action") + ".")
	}
	m.refreshingSession = true
	defer func() { m.refreshingSession = false }()
	valid, err := m.checkIfAccountValid(m.session)
	if err != nil {
		return fmt.Errorf("refresh before %s failed: %w", firstNonEmptyString(strings.TrimSpace(action), "action"), err)
	}
	if !valid {
		return fmt.Errorf("refresh before %s returned invalid session", firstNonEmptyString(strings.TrimSpace(action), "action"))
	}
	m.autosaveSessionIfChanged("pre-action refresh " + firstNonEmptyString(strings.TrimSpace(action), "action"))
	return nil
}
