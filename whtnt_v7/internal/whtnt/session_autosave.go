package whtnt

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/engine"
	"rnldo.dev/wnshell/internal/storage"
)

type sessionAutosaveJob struct {
	owner       *Module
	label       string
	record      engine.SessionRecord
	fingerprint string
	done        chan struct{}
}

var sessionAutosaves = struct {
	once  sync.Once
	mu    sync.Mutex
	cond  *sync.Cond
	queue []sessionAutosaveJob
}{}

func (m *Module) autosaveSessionIfChanged(label string) {
	if m == nil {
		return
	}
	snapshot, err := m.SessionSnapshot()
	if err != nil {
		return
	}
	record := sessionRecordFromSnapshot(snapshot, m.autosaveNow())
	if !isReusableSessionRecord(record) {
		return
	}

	fingerprint := sessionRecordFingerprint(record)
	if fingerprint == "" {
		return
	}
	m.autosaveMu.Lock()
	if fingerprint == m.lastSessionFingerprint {
		m.autosaveMu.Unlock()
		return
	}
	m.lastSessionFingerprint = fingerprint
	done := enqueueSessionAutosave(sessionAutosaveJob{
		owner:       m,
		label:       label,
		record:      record,
		fingerprint: fingerprint,
	})
	m.autosavePending = append(m.autosavePending, done)
	m.autosaveMu.Unlock()
}

func enqueueSessionAutosave(job sessionAutosaveJob) <-chan struct{} {
	job.done = make(chan struct{})
	sessionAutosaves.once.Do(func() {
		sessionAutosaves.cond = sync.NewCond(&sessionAutosaves.mu)
		go runSessionAutosaves()
	})
	sessionAutosaves.mu.Lock()
	sessionAutosaves.queue = append(sessionAutosaves.queue, job)
	sessionAutosaves.cond.Signal()
	sessionAutosaves.mu.Unlock()
	return job.done
}

func runSessionAutosaves() {
	for {
		sessionAutosaves.mu.Lock()
		for len(sessionAutosaves.queue) == 0 {
			sessionAutosaves.cond.Wait()
		}
		job := sessionAutosaves.queue[0]
		copy(sessionAutosaves.queue, sessionAutosaves.queue[1:])
		sessionAutosaves.queue = sessionAutosaves.queue[:len(sessionAutosaves.queue)-1]
		sessionAutosaves.mu.Unlock()

		persistSessionAutosave(job)
		close(job.done)
	}
}

func persistSessionAutosave(job sessionAutosaveJob) {
	m := job.owner
	if m == nil {
		return
	}

	cfg, err := appconfig.Load()
	if err != nil {
		m.logAutosaveError(job.label, err)
		return
	}
	paths := cfg.Engine.Storage
	paths.NormalizeForStore()

	if _, err := storage.UpsertSession(paths.NormalSessionsPath, job.record); err != nil {
		m.logAutosaveError(job.label, err)
		return
	}
	if exists, err := storage.SessionExists(paths.MasterSessionsPath, job.record); err == nil && exists {
		if _, err := storage.UpsertSession(paths.MasterSessionsPath, job.record); err != nil {
			m.logAutosaveError(job.label, err)
			return
		}
	} else if err != nil {
		m.logAutosaveError(job.label, err)
		return
	}
}

func (m *Module) detachSessionAutosaves() {
	if m == nil {
		return
	}
	m.autosaveMu.Lock()
	m.autosavePending = nil
	m.autosaveMu.Unlock()
}

func (m *Module) autosaveNow() time.Time {
	timezone := "Europe/London"
	if m != nil && m.config != nil && strings.TrimSpace(m.config.Engine.Timezone) != "" {
		timezone = m.config.Engine.Timezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	return time.Now().In(location)
}

func (m *Module) logAutosaveError(label string, err error) {
	if m == nil || m.log == nil || err == nil {
		return
	}
	m.log.ManagerError(fmt.Sprintf("Session autosave after %s failed: %v", label, err))
}

func sessionRecordFromSnapshot(snapshot SessionSnapshot, now time.Time) engine.SessionRecord {
	cookies := make([]engine.CookieRecord, 0, len(snapshot.Cookies))
	for _, cookie := range snapshot.Cookies {
		cookies = append(cookies, engine.CookieRecord{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HttpOnly,
			SameSite: cookie.SameSite,
		})
	}
	metadata := map[string]any{}
	for key, value := range snapshot.Metadata {
		metadata[key] = value
	}
	metadata["whtnt_session_id"] = snapshot.SessionID
	metadata["whtnt_app_session_id"] = snapshot.AppSessionID
	metadata["whtnt_request_id"] = snapshot.RequestID
	metadata["whtnt_session_token"] = snapshot.SessionToken

	id := strings.TrimSpace(snapshot.ID)
	if id == "" {
		id = stableSessionID(snapshot.AccountID, metadataString(metadata, "email"))
	}

	return engine.SessionRecord{
		ID:        id,
		AccountID: strings.TrimSpace(snapshot.AccountID),
		Cookies:   cookies,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func isReusableSessionRecord(record engine.SessionRecord) bool {
	if strings.TrimSpace(record.ID) == "" {
		return false
	}
	if strings.TrimSpace(record.AccountID) == "" && metadataString(record.Metadata, "email") == "" {
		return false
	}
	return sessionCookieValue(record.Cookies, accessTokenCookieName) != "" &&
		sessionCookieValue(record.Cookies, refreshTokenCookieName) != ""
}

func sessionCookieValue(cookies []engine.CookieRecord, name string) string {
	for _, cookie := range cookies {
		if strings.EqualFold(strings.TrimSpace(cookie.Name), name) {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func sessionRecordFingerprint(record engine.SessionRecord) string {
	metadata := stableSessionFingerprintMetadata(record.Metadata)
	stable := struct {
		ID        string
		AccountID string
		Cookies   []engine.CookieRecord
		Metadata  map[string]any
	}{
		ID:        record.ID,
		AccountID: record.AccountID,
		Cookies:   record.Cookies,
		Metadata:  metadata,
	}
	raw, err := json.Marshal(stable)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}

func stableSessionFingerprintMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	stable := make(map[string]any, len(metadata))
	for key, value := range metadata {
		if key == "reward_credit_checked_at" {
			continue
		}
		stable[key] = value
	}
	return stable
}
