package engine

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (e *Engine) recoverAllSessionScopeFromStorageCSVs(accounts []Account, sessions []SessionRecord) ([]Account, []SessionRecord, int, error) {
	now := e.now()
	recovered := 0
	seenFiles := map[string]bool{}
	err := filepath.WalkDir("storage", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".csv") {
			return nil
		}
		clean := strings.ToLower(filepath.Clean(path))
		if seenFiles[clean] {
			return nil
		}
		seenFiles[clean] = true
		rows, readErr := readRecoveryCSVRows(path)
		if readErr != nil || len(rows) == 0 {
			return nil
		}
		beforeSessions := len(sessions)
		for _, row := range rows {
			session, ok := recoverySessionFromCSVRow(row, now)
			if !ok {
				continue
			}
			sessions = upsertSession(sessions, session)
		}
		if len(sessions) > beforeSessions {
			recovered += len(sessions) - beforeSessions
		}
		beforeAccounts := len(accounts)
		accounts, _, _ = accountUpdateTargetsForPreparedRowsSilent(accounts, sessions, rows, now)
		if len(accounts) > beforeAccounts {
			recovered += len(accounts) - beforeAccounts
		}
		return nil
	})
	if err != nil {
		return accounts, sessions, recovered, err
	}
	if recovered > 0 {
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return accounts, sessions, recovered, err
		}
		if err := e.store.SaveAccounts(accounts); err != nil {
			return accounts, sessions, recovered, err
		}
	}
	return accounts, sessions, recovered, nil
}

func readRecoveryCSVRows(path string) ([]map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}
	for index := range headers {
		headers[index] = normalizeRecoveryHeader(headers[index], index == 0)
	}
	var rows []map[string]string
	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return rows, err
		}
		row := map[string]string{}
		for index, header := range headers {
			if header == "" || index >= len(record) {
				continue
			}
			row[header] = strings.TrimSpace(record[index])
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func normalizeRecoveryHeader(header string, first bool) string {
	header = strings.ToLower(strings.TrimSpace(header))
	if first {
		header = strings.TrimPrefix(header, "\ufeff")
	}
	return header
}

func recoverySessionFromCSVRow(row map[string]string, now time.Time) (SessionRecord, bool) {
	sessionID := firstNonEmptyString(row["session_id"], row["id"])
	if strings.TrimSpace(sessionID) == "" {
		return SessionRecord{}, false
	}
	session := SessionRecord{
		ID:        strings.TrimSpace(sessionID),
		AccountID: strings.TrimSpace(row["account_id"]),
		CreatedAt: parseRecoveryTime(row["created_at"]),
		UpdatedAt: parseRecoveryTime(firstNonEmptyString(row["updated_at"], row["last_checked_at"])),
		Metadata:  map[string]any{},
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}
	_ = json.Unmarshal([]byte(row["cookies_json"]), &session.Cookies)
	_ = json.Unmarshal([]byte(row["metadata_json"]), &session.Metadata)
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	applyRecoveryIdentityMetadata(session.Metadata, row)
	if session.AccountID == "" {
		session.AccountID = firstNonEmptyString(row["account_id"], metadataValue(session.Metadata, "account_id"))
	}
	if len(session.Cookies) == 0 && metadataValue(session.Metadata, "whtnt_session_token") == "" {
		return SessionRecord{}, false
	}
	return session, true
}

func applyRecoveryIdentityMetadata(metadata map[string]any, row map[string]string) {
	for source, key := range map[string]string{
		"email":             "email",
		"username":          "username",
		"password":          "password",
		"first_name":        "first_name",
		"last_name":         "last_name",
		"referrer_username": "referrer_username",
		"reward_amount":     "reward_balance_amount_safe",
		"reward_label":      "reward_label",
	} {
		value := strings.TrimSpace(row[source])
		if value != "" && metadataValue(metadata, key) == "" {
			metadata[key] = value
		}
	}
}

func parseRecoveryTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}
