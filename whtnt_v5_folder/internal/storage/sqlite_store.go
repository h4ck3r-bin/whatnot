package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/engine"

	_ "modernc.org/sqlite"
)

const (
	sqliteBusyTimeout   = 60 * time.Second
	sqliteTxTimeout     = 10 * time.Minute
	sqliteRetryDelay    = 5 * time.Second
	sqliteRetryAttempts = 24
)

type SQLiteStore struct {
	db    *sql.DB
	paths appconfig.StorageConfig
}

type SQLiteTaskStore interface {
	ClaimNextAccountPipelineTask(startFresh bool, now time.Time) (engine.Task, bool, error)
	UpdateTask(taskID string, update func(*engine.Task), now time.Time) error
}

func NewSQLiteStore(paths appconfig.StorageConfig) (*SQLiteStore, error) {
	paths.NormalizeForStore()
	if err := os.MkdirAll(filepath.Dir(paths.DatabasePath), 0755); err != nil {
		return nil, err
	}
	dsn := sqliteDSN(paths.DatabasePath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db, paths: paths}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func sqliteDSN(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path, int(sqliteBusyTimeout/time.Millisecond))
}

func (s *SQLiteStore) init() error {
	ctx, cancel := context.WithTimeout(context.Background(), sqliteBusyTimeout)
	defer cancel()
	stmts := []string{
		`PRAGMA busy_timeout = 60000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA synchronous = NORMAL`,
		`CREATE TABLE IF NOT EXISTS records (
			collection TEXT NOT NULL,
			key TEXT NOT NULL,
			payload TEXT NOT NULL,
			ordinal INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (collection, key)
		)`,
		`CREATE INDEX IF NOT EXISTS records_collection_ordinal_idx ON records(collection, ordinal)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return s.importExistingFilesOnce(ctx)
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) LoadAccounts() ([]engine.Account, error) {
	return loadSQLiteRecords[engine.Account](s, "accounts")
}

func (s *SQLiteStore) SaveAccounts(records []engine.Account) error {
	return upsertSQLiteRecords(s, "accounts", records, func(record engine.Account, index int) string {
		return stableRecordKey(record.ID, record.Email, record.Username, index)
	})
}

func (s *SQLiteStore) LoadOrders() ([]engine.Order, error) {
	return loadSQLiteRecords[engine.Order](s, "orders")
}

func (s *SQLiteStore) SaveOrders(records []engine.Order) error {
	return upsertSQLiteRecords(s, "orders", records, func(record engine.Order, index int) string {
		return stableRecordKey(record.ID, record.ExternalOrderID, record.AccountID, index)
	})
}

func (s *SQLiteStore) LoadNormalSessions() ([]engine.SessionRecord, error) {
	return loadSQLiteRecords[engine.SessionRecord](s, "normal_sessions")
}

func (s *SQLiteStore) SaveNormalSessions(records []engine.SessionRecord) error {
	return upsertSQLiteRecords(s, "normal_sessions", records, func(record engine.SessionRecord, index int) string {
		return stableRecordKey(record.ID, record.AccountID, sqliteMetadataString(record.Metadata, "email"), index)
	})
}

func (s *SQLiteStore) LoadMasterSessions() ([]engine.SessionRecord, error) {
	return loadSQLiteRecords[engine.SessionRecord](s, "master_sessions")
}

func (s *SQLiteStore) SaveMasterSessions(records []engine.SessionRecord) error {
	return upsertSQLiteRecords(s, "master_sessions", records, func(record engine.SessionRecord, index int) string {
		return stableRecordKey(record.ID, record.AccountID, sqliteMetadataString(record.Metadata, "email"), index)
	})
}

func (s *SQLiteStore) LoadMasters() ([]engine.MasterAccount, error) {
	return loadSQLiteRecords[engine.MasterAccount](s, "masters")
}

func (s *SQLiteStore) SaveMasters(records []engine.MasterAccount) error {
	return upsertSQLiteRecords(s, "masters", records, func(record engine.MasterAccount, index int) string {
		return stableRecordKey(record.ID, record.AccountID, record.Email, record.Username, index)
	})
}

func (s *SQLiteStore) LoadRuntimeState() (engine.RuntimeState, error) {
	var out engine.RuntimeState
	records, err := loadSQLiteRecords[engine.RuntimeState](s, "runtime")
	if err != nil {
		return out, err
	}
	if len(records) > 0 {
		out = records[0]
	}
	return out, nil
}

func (s *SQLiteStore) SaveRuntimeState(record engine.RuntimeState) error {
	return replaceSQLiteRecords(s, "runtime", []engine.RuntimeState{record}, func(engine.RuntimeState, int) string {
		return "runtime"
	})
}

func (s *SQLiteStore) LoadTasks() ([]engine.Task, error) {
	return loadSQLiteRecords[engine.Task](s, "tasks")
}

func (s *SQLiteStore) SaveTasks(records []engine.Task) error {
	return upsertSQLiteRecords(s, "tasks", records, func(record engine.Task, index int) string {
		return stableRecordKey(record.ID, string(record.Type), index)
	})
}

func (s *SQLiteStore) LoadPendingReviews() ([]engine.PendingReviewItem, error) {
	return loadSQLiteRecords[engine.PendingReviewItem](s, "pending_reviews")
}

func (s *SQLiteStore) SavePendingReviews(records []engine.PendingReviewItem) error {
	return upsertSQLiteRecords(s, "pending_reviews", records, func(record engine.PendingReviewItem, index int) string {
		return stableRecordKey(record.ID, record.ReviewReferenceID, record.OrderID, record.AccountID, index)
	})
}

func (s *SQLiteStore) LoadBackendJSON(key string, dest any) error {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil
	}
	var payload string
	err := withSQLiteRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), sqliteBusyTimeout)
		defer cancel()
		err := s.db.QueryRowContext(ctx, `SELECT payload FROM records WHERE collection = 'backend_json' AND key = ?`, key).Scan(&payload)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(payload) == "" {
		return nil
	}
	return json.Unmarshal([]byte(payload), dest)
}

func (s *SQLiteStore) SaveBackendJSON(key string, value any) error {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO records(collection, key, payload, ordinal, updated_at) VALUES('backend_json', ?, ?, 0, ?)
			ON CONFLICT(collection, key) DO UPDATE SET payload = excluded.payload, updated_at = excluded.updated_at`, key, string(raw), now)
		return err
	})
}

func (s *SQLiteStore) ClaimNextAccountPipelineTask(startFresh bool, now time.Time) (engine.Task, bool, error) {
	var claimed engine.Task
	err := s.withTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT key, payload FROM records WHERE collection = 'tasks' ORDER BY ordinal ASC, updated_at ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			var payload string
			if err := rows.Scan(&key, &payload); err != nil {
				return err
			}
			var task engine.Task
			if err := json.Unmarshal([]byte(payload), &task); err != nil {
				return err
			}
			if task.Type != engine.TaskTypeAccountPipeline || task.State != engine.TaskStatePending {
				continue
			}
			if startFresh && strings.TrimSpace(task.AccountID) != "" {
				continue
			}
			task.State = engine.TaskStateRunning
			task.Attempt++
			task.StartedAt = &now
			task.UpdatedAt = now
			raw, err := json.Marshal(task)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE records SET payload = ?, updated_at = ? WHERE collection = 'tasks' AND key = ?`, string(raw), now.Format(time.RFC3339Nano), key); err != nil {
				return err
			}
			claimed = task
			return nil
		}
		return rows.Err()
	})
	if err != nil {
		return engine.Task{}, false, err
	}
	return claimed, claimed.ID != "", nil
}

func (s *SQLiteStore) UpdateTask(taskID string, update func(*engine.Task), now time.Time) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return fmt.Errorf("task id is required")
	}
	return s.withTx(func(tx *sql.Tx) error {
		key := ""
		task := engine.Task{}
		rows, err := tx.Query(`SELECT key, payload FROM records WHERE collection = 'tasks'`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var candidateKey string
			var payload string
			if err := rows.Scan(&candidateKey, &payload); err != nil {
				_ = rows.Close()
				return err
			}
			var candidate engine.Task
			if err := json.Unmarshal([]byte(payload), &candidate); err != nil {
				_ = rows.Close()
				return err
			}
			if candidate.ID == taskID {
				key = candidateKey
				task = candidate
				break
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if key == "" {
			return fmt.Errorf("task %s not found", taskID)
		}
		update(&task)
		if task.UpdatedAt.IsZero() {
			task.UpdatedAt = now
		}
		raw, err := json.Marshal(task)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE records SET payload = ?, updated_at = ? WHERE collection = 'tasks' AND key = ?`, string(raw), now.Format(time.RFC3339Nano), key)
		return err
	})
}

func loadSQLiteRecords[T any](s *SQLiteStore, collection string) ([]T, error) {
	var out []T
	err := withSQLiteRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), sqliteBusyTimeout)
		defer cancel()
		rows, err := s.db.QueryContext(ctx, `SELECT payload FROM records WHERE collection = ? ORDER BY ordinal ASC, updated_at ASC`, collection)
		if err != nil {
			return err
		}
		defer rows.Close()
		records := []T{}
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				return err
			}
			var record T
			if err := json.Unmarshal([]byte(payload), &record); err != nil {
				return err
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		out = records
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []T{}
	}
	return out, nil
}

func withSQLiteRetry(fn func() error) error {
	var lastErr error
	for attempt := 1; attempt <= sqliteRetryAttempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err
		if !isSQLiteBusyError(err) || attempt == sqliteRetryAttempts {
			return err
		}
		time.Sleep(sqliteRetryDelay)
	}
	return lastErr
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrTxDone) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "database is locked") ||
		strings.Contains(text, "database table is locked") ||
		strings.Contains(text, "database is busy") ||
		strings.Contains(text, "sqlite_busy") ||
		strings.Contains(text, "sqlite_locked") ||
		strings.Contains(text, "transaction has already been committed or rolled back") ||
		strings.Contains(text, "already been committed or rolled back") ||
		strings.Contains(text, "statement is closed") ||
		strings.Contains(text, "driver: bad connection") ||
		strings.Contains(text, "bad connection") ||
		strings.Contains(text, "busy") && strings.Contains(text, "sqlite") ||
		strings.Contains(text, "locked") && strings.Contains(text, "sqlite")
}

func upsertSQLiteRecords[T any](s *SQLiteStore, collection string, records []T, key func(T, int) string) error {
	return writeSQLiteRecords(s, collection, records, key, false)
}

func replaceSQLiteRecords[T any](s *SQLiteStore, collection string, records []T, key func(T, int) string) error {
	return writeSQLiteRecords(s, collection, records, key, true)
}

func writeSQLiteRecords[T any](s *SQLiteStore, collection string, records []T, key func(T, int) string, replace bool) error {
	if records == nil {
		records = []T{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return s.withTx(func(tx *sql.Tx) error {
		if replace {
			if _, err := tx.Exec(`DELETE FROM records WHERE collection = ?`, collection); err != nil {
				return err
			}
		}
		stmt, err := tx.Prepare(`INSERT INTO records(collection, key, payload, ordinal, updated_at) VALUES(?, ?, ?, ?, ?)
			ON CONFLICT(collection, key) DO UPDATE SET payload = excluded.payload, ordinal = excluded.ordinal, updated_at = excluded.updated_at`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for index, record := range records {
			if !replace {
				write, err := shouldWriteSQLiteRecord(tx, collection, key(record, index), record)
				if err != nil {
					return err
				}
				if !write {
					continue
				}
			}
			raw, err := json.Marshal(record)
			if err != nil {
				return err
			}
			if _, err := stmt.Exec(collection, key(record, index), string(raw), index, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func shouldWriteSQLiteRecord[T any](tx *sql.Tx, collection string, key string, incoming T) (bool, error) {
	var payload string
	err := tx.QueryRow(`SELECT payload FROM records WHERE collection = ? AND key = ?`, collection, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var existing T
	if err := json.Unmarshal([]byte(payload), &existing); err != nil {
		return true, nil
	}
	incomingTime, incomingOK := recordUpdatedAt(incoming)
	existingTime, existingOK := recordUpdatedAt(existing)
	if !incomingOK || incomingTime.IsZero() || !existingOK || existingTime.IsZero() {
		return true, nil
	}
	return !incomingTime.Before(existingTime), nil
}

func recordUpdatedAt(value any) (time.Time, bool) {
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return time.Time{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return time.Time{}, false
	}
	field := v.FieldByName("UpdatedAt")
	if !field.IsValid() {
		return time.Time{}, false
	}
	t, ok := field.Interface().(time.Time)
	return t, ok
}

func (s *SQLiteStore) withTx(fn func(*sql.Tx) error) error {
	return withSQLiteRetry(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), sqliteTxTimeout)
		defer cancel()
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
		if err != nil {
			return err
		}
		finalized := false
		defer func() {
			if !finalized {
				_ = tx.Rollback()
			}
		}()
		if err := fn(tx); err != nil {
			finalized = true
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			finalized = true
			return err
		}
		finalized = true
		return nil
	})
}

func (s *SQLiteStore) importExistingFilesOnce(ctx context.Context) error {
	imported, err := s.metaValue(ctx, "json_import_completed_at")
	if err != nil {
		return err
	}
	if strings.TrimSpace(imported) != "" {
		return nil
	}
	empty, err := s.recordsEmpty(ctx)
	if err != nil {
		return err
	}
	if !empty {
		return s.setMetaValue(ctx, "json_import_completed_at", time.Now().UTC().Format(time.RFC3339Nano))
	}
	if err := s.importExistingFiles(); err != nil {
		return err
	}
	return s.setMetaValue(ctx, "json_import_completed_at", time.Now().UTC().Format(time.RFC3339Nano))
}

func (s *SQLiteStore) importExistingFiles() error {
	accounts, err := loadSlice[engine.Account](s.paths.AccountsPath)
	if err != nil {
		return fmt.Errorf("import accounts: %w", err)
	}
	orders, err := loadSlice[engine.Order](s.paths.OrdersPath)
	if err != nil {
		return fmt.Errorf("import orders: %w", err)
	}
	normalSessions, err := loadSessions(s.paths.NormalSessionsPath)
	if err != nil {
		return fmt.Errorf("import normal sessions: %w", err)
	}
	masterSessions, err := loadSessions(s.paths.MasterSessionsPath)
	if err != nil {
		return fmt.Errorf("import master sessions: %w", err)
	}
	masters, err := loadSlice[engine.MasterAccount](s.paths.MastersPath)
	if err != nil {
		return fmt.Errorf("import masters: %w", err)
	}
	runtimeState, err := loadObject[engine.RuntimeState](s.paths.RuntimePath)
	if err != nil {
		return fmt.Errorf("import runtime: %w", err)
	}
	tasks, err := loadSlice[engine.Task](s.paths.TaskQueuePath)
	if err != nil {
		tasks = []engine.Task{}
	}
	pendingReviews, err := loadSlice[engine.PendingReviewItem](s.paths.PendingReviewsPath)
	if err != nil {
		return fmt.Errorf("import pending reviews: %w", err)
	}
	discoverySnapshots, err := loadBackendFile(s.paths.DiscoveryCachePath)
	if err != nil {
		return fmt.Errorf("import discovery snapshots: %w", err)
	}
	errorWebhookDedupe, err := loadBackendFile(s.paths.ErrorDedupePath)
	if err != nil {
		return fmt.Errorf("import error webhook dedupe: %w", err)
	}
	if err := s.SaveAccounts(accounts); err != nil {
		return err
	}
	if err := s.SaveOrders(orders); err != nil {
		return err
	}
	if err := s.SaveNormalSessions(normalSessions); err != nil {
		return err
	}
	if err := s.SaveMasterSessions(masterSessions); err != nil {
		return err
	}
	if err := s.SaveMasters(masters); err != nil {
		return err
	}
	if err := s.SaveRuntimeState(runtimeState); err != nil {
		return err
	}
	if err := s.SaveTasks(tasks); err != nil {
		return err
	}
	if err := s.SavePendingReviews(pendingReviews); err != nil {
		return err
	}
	if discoverySnapshots != nil {
		if err := s.SaveBackendJSON("account_discovery_snapshots", discoverySnapshots); err != nil {
			return err
		}
	}
	if errorWebhookDedupe != nil {
		if err := s.SaveBackendJSON("error_webhook_dedupe", errorWebhookDedupe); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) recordsEmpty(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM records`).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *SQLiteStore) metaValue(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *SQLiteStore) setMetaValue(ctx context.Context, key string, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func stableRecordKey(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "0" {
			return strings.ToLower(text)
		}
	}
	raw, _ := json.Marshal(values)
	hash := fnv.New64a()
	_, _ = hash.Write(raw)
	return fmt.Sprintf("generated_%x", hash.Sum64())
}

func sqliteMetadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func loadBackendFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
