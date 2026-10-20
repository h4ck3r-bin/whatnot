package storage

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"rnldo.dev/wnshell/internal/engine"
)

var sessionCSVHeader = []string{
	"id",
	"account_id",
	"email",
	"username",
	"proxy",
	"ip",
	"access_token",
	"refresh_token",
	"created_at",
	"updated_at",
	"cookies_json",
	"metadata_json",
}

var sessionFileLocks sync.Map
var sessionGzipWriters = sync.Pool{
	New: func() any {
		writer, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return writer
	},
}

func sessionFileMutex(path string) *sync.Mutex {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	key := strings.ToLower(filepath.Clean(absolute))
	value, _ := sessionFileLocks.LoadOrStore(key, &sync.Mutex{})
	return value.(*sync.Mutex)
}

func ensureSessionFile(path string) error {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return saveSessionsCSV(path, nil)
	}
	return ensureJSONFile(path)
}

func loadSessions(path string) ([]engine.SessionRecord, error) {
	mu := sessionFileMutex(path)
	mu.Lock()
	defer mu.Unlock()
	return loadSessionsUnlocked(path)
}

func loadSessionsUnlocked(path string) ([]engine.SessionRecord, error) {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		return loadSessionsCSV(path)
	}
	return loadSlice[engine.SessionRecord](path)
}

func loadSessionsForWriteUnlocked(path string) ([]engine.SessionRecord, error) {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		records, err := readSessionsCSVFile(path)
		if err != nil {
			return nil, err
		}
		recovered := recoverSessionCSVTempRecords(path)
		if len(recovered) > 0 {
			records = mergeSessionRecords(records, recovered)
		}
		return records, nil
	}
	return loadSlice[engine.SessionRecord](path)
}

func saveSessions(path string, records []engine.SessionRecord) error {
	mu := sessionFileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	existing, err := loadSessionsForWriteUnlocked(path)
	if err != nil {
		return err
	}
	merged := mergeSessionRecords(existing, records)
	return saveSessionsUnlocked(path, merged)
}

func saveSessionsUnlocked(path string, records []engine.SessionRecord) error {
	if strings.EqualFold(filepath.Ext(path), ".csv") {
		return saveSessionsCSV(path, records)
	}
	return saveSlice(path, records)
}

func UpsertSession(path string, record engine.SessionRecord) (bool, error) {
	mu := sessionFileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	records, err := loadSessionsForWriteUnlocked(path)
	if err != nil {
		return false, err
	}

	changed := true
	found := false
	for i := range records {
		if !sameSessionIdentity(records[i], record) {
			continue
		}
		found = true
		if record.ID == "" {
			record.ID = records[i].ID
		}
		if record.AccountID == "" {
			record.AccountID = records[i].AccountID
		}
		if !records[i].CreatedAt.IsZero() {
			record.CreatedAt = records[i].CreatedAt
		} else if record.CreatedAt.IsZero() {
			record.CreatedAt = records[i].CreatedAt
		}
		if sessionsEquivalent(records[i], record) {
			changed = false
			break
		}
		if !record.UpdatedAt.IsZero() && !records[i].UpdatedAt.IsZero() && records[i].UpdatedAt.After(record.UpdatedAt) {
			changed = false
			break
		}
		records[i] = record
		break
	}
	if !found {
		records = append(records, record)
	}
	if !changed {
		return false, nil
	}
	return true, saveSessionsUnlocked(path, records)
}

func SessionExists(path string, record engine.SessionRecord) (bool, error) {
	mu := sessionFileMutex(path)
	mu.Lock()
	defer mu.Unlock()

	records, err := loadSessionsUnlocked(path)
	if err != nil {
		return false, err
	}
	for _, existing := range records {
		if sameSessionIdentity(existing, record) {
			return true, nil
		}
	}
	return false, nil
}

func sameSessionIdentity(existing engine.SessionRecord, next engine.SessionRecord) bool {
	if existing.ID != "" && next.ID != "" && existing.ID == next.ID {
		return true
	}
	if existing.AccountID != "" && next.AccountID != "" && existing.AccountID == next.AccountID {
		return true
	}
	existingEmail := metadataString(existing.Metadata, "email")
	nextEmail := metadataString(next.Metadata, "email")
	return existingEmail != "" && nextEmail != "" && strings.EqualFold(existingEmail, nextEmail)
}

func sessionsEquivalent(existing engine.SessionRecord, next engine.SessionRecord) bool {
	return existing.ID == next.ID &&
		existing.AccountID == next.AccountID &&
		reflect.DeepEqual(existing.Cookies, next.Cookies) &&
		reflect.DeepEqual(existing.Metadata, next.Metadata)
}

func mergeSessionRecords(existing []engine.SessionRecord, incoming []engine.SessionRecord) []engine.SessionRecord {
	merged := append([]engine.SessionRecord(nil), existing...)
	for _, record := range incoming {
		found := false
		for i := range merged {
			if !sameSessionIdentity(merged[i], record) {
				continue
			}
			found = true
			if record.ID == "" {
				record.ID = merged[i].ID
			}
			if record.AccountID == "" {
				record.AccountID = merged[i].AccountID
			}
			if !merged[i].CreatedAt.IsZero() {
				record.CreatedAt = merged[i].CreatedAt
			}
			merged[i] = record
			break
		}
		if !found {
			merged = append(merged, record)
		}
	}
	return merged
}

func loadSessionsCSV(path string) ([]engine.SessionRecord, error) {
	records, err := readSessionsCSVFile(path)
	if err != nil {
		return nil, err
	}
	recovered := recoverSessionCSVTempRecords(path)
	if len(recovered) == 0 {
		return mergeRelatedSessionCSVRecords(path, records), nil
	}
	return mergeRelatedSessionCSVRecords(path, mergeSessionRecords(records, recovered)), nil
}

func readSessionsCSVFile(path string) ([]engine.SessionRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []engine.SessionRecord{}, nil
		}
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return []engine.SessionRecord{}, nil
	}

	index := map[string]int{}
	for i, column := range rows[0] {
		index[strings.TrimSpace(column)] = i
	}

	records := make([]engine.SessionRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		record := engine.SessionRecord{
			ID:        csvValue(row, index, "id"),
			AccountID: csvValue(row, index, "account_id"),
			CreatedAt: parseCSVTime(csvValue(row, index, "created_at")),
			UpdatedAt: parseCSVTime(csvValue(row, index, "updated_at")),
		}
		if record.ID == "" {
			continue
		}
		_ = unmarshalSessionJSON(csvValue(row, index, "cookies_json"), &record.Cookies)
		_ = unmarshalSessionJSON(csvValue(row, index, "metadata_json"), &record.Metadata)
		if record.Cookies == nil {
			record.Cookies = []engine.CookieRecord{}
		}
		if record.Metadata == nil {
			record.Metadata = map[string]any{}
		}
		records = append(records, record)
	}
	return records, nil
}

func recoverSessionCSVTempRecords(path string) []engine.SessionRecord {
	pattern := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool {
		left, leftErr := os.Stat(matches[i])
		right, rightErr := os.Stat(matches[j])
		if leftErr != nil || rightErr != nil {
			return matches[i] < matches[j]
		}
		return left.ModTime().Before(right.ModTime())
	})
	var recovered []engine.SessionRecord
	for _, match := range matches {
		records, err := readSessionsCSVFile(match)
		if err != nil || len(records) == 0 {
			continue
		}
		recovered = mergeSessionRecords(recovered, records)
	}
	return recovered
}

func mergeRelatedSessionCSVRecords(path string, records []engine.SessionRecord) []engine.SessionRecord {
	if !strings.EqualFold(filepath.Base(path), "normal_sessions.csv") {
		return records
	}
	for _, name := range []string{"valid_sessions.csv", "whatnot_checkout_accounts.csv"} {
		relatedPath := filepath.Join(filepath.Dir(path), name)
		related, err := readRelatedSessionCSVFile(relatedPath)
		if err != nil || len(related) == 0 {
			continue
		}
		records = mergeSessionRecordsPreservingComplete(records, related)
	}
	return records
}

func mergeSessionRecordsPreservingComplete(existing []engine.SessionRecord, incoming []engine.SessionRecord) []engine.SessionRecord {
	merged := append([]engine.SessionRecord(nil), existing...)
	for _, record := range incoming {
		found := false
		for i := range merged {
			if !sameSessionIdentity(merged[i], record) {
				continue
			}
			found = true
			merged[i] = mergeSessionRecordPreferComplete(merged[i], record)
			break
		}
		if !found {
			merged = append(merged, record)
		}
	}
	return merged
}

func mergeSessionRecordPreferComplete(existing engine.SessionRecord, incoming engine.SessionRecord) engine.SessionRecord {
	if sessionRecordCompleteness(incoming) > sessionRecordCompleteness(existing) {
		return incoming
	}
	if existing.Metadata == nil {
		existing.Metadata = map[string]any{}
	}
	for key, value := range incoming.Metadata {
		if _, ok := existing.Metadata[key]; !ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			existing.Metadata[key] = value
		}
	}
	if !hasRelatedCookieValues(existing.Cookies) && hasRelatedCookieValues(incoming.Cookies) {
		existing.Cookies = incoming.Cookies
	}
	if existing.AccountID == "" {
		existing.AccountID = incoming.AccountID
	}
	if existing.CreatedAt.IsZero() {
		existing.CreatedAt = incoming.CreatedAt
	}
	if incoming.UpdatedAt.After(existing.UpdatedAt) {
		existing.UpdatedAt = incoming.UpdatedAt
	}
	return existing
}

func sessionRecordCompleteness(record engine.SessionRecord) int {
	score := 0
	if strings.TrimSpace(record.ID) != "" {
		score++
	}
	if strings.TrimSpace(record.AccountID) != "" {
		score++
	}
	if metadataString(record.Metadata, "email") != "" {
		score++
	}
	if metadataString(record.Metadata, "username") != "" {
		score++
	}
	if metadataString(record.Metadata, "whtnt_session_token") != "" {
		score++
	}
	if cookieValue(record.Cookies, "__Secure-access-token") != "" {
		score += 2
	}
	if cookieValue(record.Cookies, "__Secure-refresh-token") != "" {
		score += 2
	}
	return score
}

func readRelatedSessionCSVFile(path string) ([]engine.SessionRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return nil, nil
	}

	index := map[string]int{}
	for i, column := range rows[0] {
		index[strings.ToLower(strings.TrimSpace(column))] = i
	}

	records := make([]engine.SessionRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		record := relatedSessionRecord(row, index)
		if record.ID == "" {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func relatedSessionRecord(row []string, index map[string]int) engine.SessionRecord {
	email := relatedCSVValue(row, index, "email")
	username := relatedCSVValue(row, index, "username")
	accountID := firstNonEmpty(
		relatedCSVValue(row, index, "account_id"),
		storageAccountID(email),
	)
	sessionID := firstNonEmpty(
		relatedCSVValue(row, index, "session_id"),
		storageSessionID(email),
	)
	sessionToken := relatedCSVValue(row, index, "session_token")
	accessToken := relatedCSVValue(row, index, "access_token")
	refreshToken := relatedCSVValue(row, index, "refresh_token")
	cookies := relatedSessionCookies(row, index, accessToken, refreshToken)
	if sessionToken == "" && !hasRelatedCookieValues(cookies) {
		return engine.SessionRecord{}
	}

	updatedAt := firstRelatedCSVTime(
		relatedCSVValue(row, index, "updated_at"),
		relatedCSVValue(row, index, "recorded_at"),
	)
	metadata := map[string]any{
		"email":               email,
		"username":            username,
		"ip":                  relatedCSVValue(row, index, "ip"),
		"proxy":               relatedCSVValue(row, index, "proxy"),
		"password":            relatedCSVValue(row, index, "password"),
		"first_name":          relatedCSVValue(row, index, "first_name"),
		"last_name":           relatedCSVValue(row, index, "last_name"),
		"referrer_username":   relatedCSVValue(row, index, "referrer_username"),
		"whtnt_session_token": sessionToken,
		"access_token":        accessToken,
		"reward_label":        relatedCSVValue(row, index, "reward_label"),
	}
	if amount := relatedCSVValue(row, index, "reward_amount"); amount != "" {
		metadata["referral_amount"] = amount
		metadata["reward_balance_amount_safe"] = amount
		metadata["reward_balance_currency"] = "GBP"
		metadata["reward_balance_last_seen_amount_safe"] = amount
		metadata["reward_balance_last_seen_currency"] = "GBP"
		metadata["reward_balance_last_logged_amount_safe"] = amount
		metadata["reward_balance_last_logged_currency"] = "GBP"
	}
	for key, value := range metadata {
		if strings.TrimSpace(fmt.Sprint(value)) == "" {
			delete(metadata, key)
		}
	}

	return engine.SessionRecord{
		ID:        sessionID,
		AccountID: accountID,
		Cookies:   cookies,
		Metadata:  metadata,
		CreatedAt: updatedAt,
		UpdatedAt: updatedAt,
	}
}

func relatedSessionCookies(row []string, index map[string]int, accessToken string, refreshToken string) []engine.CookieRecord {
	cookiesJSON := relatedCSVValue(row, index, "cookies_json")
	if cookiesJSON != "" {
		var cookies []relatedCookieRecord
		if err := json.Unmarshal([]byte(cookiesJSON), &cookies); err == nil {
			out := make([]engine.CookieRecord, 0, len(cookies))
			for _, cookie := range cookies {
				out = append(out, engine.CookieRecord{
					Name:     strings.TrimSpace(cookie.Name),
					Value:    strings.TrimSpace(cookie.Value),
					Domain:   firstNonEmpty(cookie.Domain, "www.whatnot.com"),
					Path:     firstNonEmpty(cookie.Path, "/"),
					Secure:   cookie.Secure,
					HTTPOnly: cookie.HTTPOnly,
					SameSite: cookie.SameSite,
				})
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	out := []engine.CookieRecord{}
	if refreshToken != "" {
		out = append(out, engine.CookieRecord{Name: "__Secure-refresh-token", Value: refreshToken, Domain: "www.whatnot.com", Path: "/", Secure: true, HTTPOnly: true, SameSite: "lax"})
	}
	if accessToken != "" {
		out = append(out, engine.CookieRecord{Name: "__Secure-access-token", Value: accessToken, Domain: "www.whatnot.com", Path: "/", Secure: true, HTTPOnly: true, SameSite: "lax"})
	}
	return out
}

type relatedCookieRecord struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite"`
}

func relatedCSVValue(row []string, index map[string]int, column string) string {
	i, ok := index[column]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func storageAccountID(email string) string {
	if strings.TrimSpace(email) == "" {
		return ""
	}
	return "acct_" + storageSlug(email)
}

func storageSessionID(email string) string {
	if strings.TrimSpace(email) == "" {
		return ""
	}
	return "sess_" + storageSlug(email)
}

func storageSlug(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func firstRelatedCSVTime(values ...string) time.Time {
	for _, value := range values {
		if parsed := parseCSVTime(value); !parsed.IsZero() {
			return parsed
		}
	}
	return time.Time{}
}

func hasRelatedCookieValues(cookies []engine.CookieRecord) bool {
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) != "" && strings.TrimSpace(cookie.Value) != "" {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func saveSessionsCSV(path string, records []engine.SessionRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	writer := csv.NewWriter(tmp)

	if err := writer.Write(sessionCSVHeader); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	for _, record := range records {
		cookiesJSON, err := marshalSessionJSON(record.Cookies)
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
		metadataJSON, err := marshalSessionJSON(record.Metadata)
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
		row := []string{
			record.ID,
			record.AccountID,
			metadataString(record.Metadata, "email"),
			metadataString(record.Metadata, "username"),
			metadataString(record.Metadata, "proxy"),
			metadataString(record.Metadata, "ip"),
			cookieValue(record.Cookies, "__Secure-access-token"),
			cookieValue(record.Cookies, "__Secure-refresh-token"),
			formatCSVTime(record.CreatedAt),
			formatCSVTime(record.UpdatedAt),
			cookiesJSON,
			metadataJSON,
		}
		if err := writer.Write(row); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
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

func marshalSessionJSON(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}

	var compressed bytes.Buffer
	writer := sessionGzipWriters.Get().(*gzip.Writer)
	writer.Reset(&compressed)
	defer func() {
		writer.Reset(io.Discard)
		sessionGzipWriters.Put(writer)
	}()
	if _, err := writer.Write(raw); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(compressed.Bytes())
	if len("gz:"+encoded) >= len(raw) {
		return string(raw), nil
	}
	return "gz:" + encoded, nil
}

func unmarshalSessionJSON(value string, target any) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "gz:") {
		return json.Unmarshal([]byte(value), target)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "gz:"))
	if err != nil {
		return fmt.Errorf("decode compressed session json: %w", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return fmt.Errorf("open compressed session json: %w", err)
	}
	defer reader.Close()

	raw, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("read compressed session json: %w", err)
	}
	return json.Unmarshal(raw, target)
}

func csvValue(row []string, index map[string]int, column string) string {
	i, ok := index[column]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func cookieValue(cookies []engine.CookieRecord, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func parseCSVTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func formatCSVTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339Nano)
}
