package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/engine"
)

type JSONStore struct {
	paths appconfig.StorageConfig
	mu    sync.Mutex
}

func NewJSONStore(paths appconfig.StorageConfig) (*JSONStore, error) {
	paths.NormalizeForStore()
	store := &JSONStore{paths: paths}
	for _, path := range []string{
		paths.AccountsPath,
		paths.OrdersPath,
		paths.MastersPath,
		paths.TaskQueuePath,
	} {
		if err := ensureJSONFile(path); err != nil {
			return nil, err
		}
	}
	for _, path := range []string{
		paths.NormalSessionsPath,
		paths.MasterSessionsPath,
	} {
		if err := ensureSessionFile(path); err != nil {
			return nil, err
		}
	}
	if err := ensureJSONObjectFile(paths.RuntimePath); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *JSONStore) LoadAccounts() ([]engine.Account, error) {
	return loadSlice[engine.Account](s.paths.AccountsPath)
}

func (s *JSONStore) SaveAccounts(records []engine.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSlice(s.paths.AccountsPath, records)
}

func (s *JSONStore) LoadOrders() ([]engine.Order, error) {
	return loadSlice[engine.Order](s.paths.OrdersPath)
}

func (s *JSONStore) SaveOrders(records []engine.Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSlice(s.paths.OrdersPath, records)
}

func (s *JSONStore) LoadNormalSessions() ([]engine.SessionRecord, error) {
	return loadSessions(s.paths.NormalSessionsPath)
}

func (s *JSONStore) SaveNormalSessions(records []engine.SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSessions(s.paths.NormalSessionsPath, records)
}

func (s *JSONStore) LoadMasterSessions() ([]engine.SessionRecord, error) {
	return loadSessions(s.paths.MasterSessionsPath)
}

func (s *JSONStore) SaveMasterSessions(records []engine.SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSessions(s.paths.MasterSessionsPath, records)
}

func (s *JSONStore) LoadMasters() ([]engine.MasterAccount, error) {
	return loadSlice[engine.MasterAccount](s.paths.MastersPath)
}

func (s *JSONStore) SaveMasters(records []engine.MasterAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSlice(s.paths.MastersPath, records)
}

func (s *JSONStore) LoadRuntimeState() (engine.RuntimeState, error) {
	return loadObject[engine.RuntimeState](s.paths.RuntimePath)
}

func (s *JSONStore) SaveRuntimeState(record engine.RuntimeState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveObject(s.paths.RuntimePath, record)
}

func (s *JSONStore) LoadTasks() ([]engine.Task, error) {
	return loadSlice[engine.Task](s.paths.TaskQueuePath)
}

func (s *JSONStore) SaveTasks(records []engine.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSlice(s.paths.TaskQueuePath, records)
}

func (s *JSONStore) LoadPendingReviews() ([]engine.PendingReviewItem, error) {
	return loadSlice[engine.PendingReviewItem](s.paths.PendingReviewsPath)
}

func (s *JSONStore) SavePendingReviews(records []engine.PendingReviewItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveSlice(s.paths.PendingReviewsPath, records)
}

func (s *JSONStore) LoadBackendJSON(key string, dest any) error {
	path := s.backendJSONPath(key)
	if path == "" {
		return nil
	}
	record, err := loadObject[json.RawMessage](path)
	if err != nil {
		return err
	}
	if len(record) == 0 {
		return nil
	}
	return json.Unmarshal(record, dest)
}

func (s *JSONStore) SaveBackendJSON(key string, value any) error {
	path := s.backendJSONPath(key)
	if path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return saveObject(path, value)
}

func (s *JSONStore) backendJSONPath(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "account_discovery_snapshots":
		return s.paths.DiscoveryCachePath
	case "error_webhook_dedupe":
		return s.paths.ErrorDedupePath
	default:
		return ""
	}
}

func loadSlice[T any](path string) ([]T, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []T{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return []T{}, nil
	}
	raw = stripUTF8BOM(raw)
	var records []T
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, err
	}
	if records == nil {
		records = []T{}
	}
	return records, nil
}

func saveSlice[T any](path string, records []T) error {
	if records == nil {
		records = []T{}
	}
	return saveJSON(path, records)
}

func loadObject[T any](path string) (T, error) {
	var record T
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return record, nil
		}
		return record, err
	}
	raw = stripUTF8BOM(raw)
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "[]" {
		return record, nil
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record, err
	}
	return record, nil
}

func saveObject[T any](path string, record T) error {
	return saveJSON(path, record)
}

func stripUTF8BOM(raw []byte) []byte {
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return raw[3:]
	}
	return raw
}

func saveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := replaceFileWithRetry(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func replaceFileWithRetry(tmpName string, path string) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(100+attempt*50) * time.Millisecond)
		}
		backupName := fmt.Sprintf("%s.replace.%d.bak", path, os.Getpid())
		_ = os.Remove(backupName)
		renamedExisting := false
		if err := os.Rename(path, backupName); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				lastErr = err
				continue
			}
		} else {
			renamedExisting = true
		}
		if err := os.Rename(tmpName, path); err != nil {
			lastErr = err
			if renamedExisting {
				_ = os.Rename(backupName, path)
			}
			continue
		}
		if renamedExisting {
			_ = os.Remove(backupName)
		}
		return nil
	}
	return lastErr
}

func ensureJSONFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte("[]\n"), 0644)
}

func ensureJSONObjectFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.WriteFile(path, []byte("{}\n"), 0644)
}
