package engine

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/referrors"
)

type friendCodeRecord struct {
	Code   string
	Status string
	Uses   int
}

var friendCodeLocks sync.Map

func claimFriendCode(path string) (string, error) {
	return claimFriendCodeExcluding(path, nil)
}

func claimFriendCodeExcluding(path string, excluded map[string]bool) (string, error) {
	path = filepath.Clean(path)
	lockValue, _ := friendCodeLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	records, err := loadFriendCodeRecords(path)
	if err != nil {
		return "", err
	}
	candidates := make([]int, 0, len(records))
	for i, record := range records {
		code := strings.ToLower(strings.TrimSpace(record.Code))
		if excluded != nil && excluded[code] {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(record.Status)) {
		case "ACTIVE":
			candidates = append(candidates, i)
		case "TEMP":
			if record.Uses > 0 {
				candidates = append(candidates, i)
			}
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no usable friend codes in %s", path)
	}

	index := candidates[rand.Intn(len(candidates))]
	selected := strings.TrimSpace(records[index].Code)
	if strings.EqualFold(records[index].Status, "TEMP") {
		records[index].Uses--
		if records[index].Uses <= 0 {
			records[index].Uses = 0
			records[index].Status = "USED"
		}
		if err := saveFriendCodeRecords(path, records); err != nil {
			return "", err
		}
	}
	return selected, nil
}

func syncMasterFriendCode(path string, master MasterAccount) error {
	status, uses := friendCodeStatusForMaster(master)
	if strings.TrimSpace(master.Username) == "" || status == "" {
		return nil
	}
	return upsertFriendCode(path, master.Username, status, uses)
}

func friendCodeStatusForMaster(master MasterAccount) (string, int) {
	switch master.State {
	case MasterStateCandidate:
		return "", 0
	case MasterStateHealthyPending:
		return "HEALTHY_PENDING", maxInt(master.SignupCap-master.SignupCount, 0)
	case MasterStateActive:
		return "TEMP", maxInt(master.SignupCap-master.SignupCount, 0)
	case MasterStateAging:
		return "AGING", 0
	case MasterStateReady:
		return "READY", 0
	case MasterStateDormant:
		return "DORMANT", 0
	case MasterStateFailed:
		return "FAILED", 0
	default:
		return "", 0
	}
}

func upsertFriendCode(path string, code string, status string, uses int) error {
	path = filepath.Clean(path)
	lockValue, _ := friendCodeLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	records, err := loadFriendCodeRecords(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		records = []friendCodeRecord{}
	}

	trimmedCode := strings.TrimSpace(code)
	for i := range records {
		if strings.EqualFold(strings.TrimSpace(records[i].Code), trimmedCode) {
			records[i].Code = trimmedCode
			records[i].Status = strings.ToUpper(strings.TrimSpace(status))
			records[i].Uses = uses
			return saveFriendCodeRecords(path, records)
		}
	}

	records = append(records, friendCodeRecord{
		Code:   trimmedCode,
		Status: strings.ToUpper(strings.TrimSpace(status)),
		Uses:   uses,
	})
	return saveFriendCodeRecords(path, records)
}

func invalidateFriendCode(path string, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	return upsertFriendCode(path, code, "INVALID", 0)
}

func replaceFriendCodeWithInvalid(path string, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	path = filepath.Clean(path)
	lockValue, _ := friendCodeLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	records, err := loadFriendCodeRecords(path)
	if err != nil {
		return err
	}
	filtered := records[:0]
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Code), code) {
			continue
		}
		filtered = append(filtered, record)
	}
	filtered = append(filtered, friendCodeRecord{
		Code:   code,
		Status: "INVALID",
		Uses:   0,
	})
	return saveFriendCodeRecords(path, filtered)
}

func removeFriendCode(path string, code string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	path = filepath.Clean(path)
	lockValue, _ := friendCodeLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	records, err := loadFriendCodeRecords(path)
	if err != nil {
		return err
	}
	filtered := records[:0]
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Code), code) {
			continue
		}
		filtered = append(filtered, record)
	}
	return saveFriendCodeRecords(path, filtered)
}

func friendCodeExplicitlyUnusable(path string, code string) bool {
	status, uses, found := friendCodeCurrentStatus(path, code)
	if !found {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "INVALID", "DORMANT", "FAILED", "USED", "AGING", "HEALTHY_PENDING", "READY":
		return true
	case "TEMP":
		return uses <= 0
	default:
		return false
	}
}

func friendCodeCurrentStatus(path string, code string) (string, int, bool) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", 0, false
	}
	path = filepath.Clean(path)
	lockValue, _ := friendCodeLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	records, err := loadFriendCodeRecords(path)
	if err != nil {
		return "", 0, false
	}
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Code), code) {
			return strings.ToUpper(strings.TrimSpace(record.Status)), record.Uses, true
		}
	}
	return "", 0, false
}

func isInvalidReferrerError(err error) bool {
	if err == nil {
		return false
	}
	var applyErr referrors.ApplyError
	return errors.As(err, &applyErr) && applyErr.IsInvalidReferrer()
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func loadFriendCodeRecords(path string) ([]friendCodeRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("friend-code file %s could not be read: %w", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read friend-code csv %s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	header := map[string]int{}
	for i, column := range rows[0] {
		key := strings.ToLower(strings.TrimSpace(column))
		if i == 0 {
			key = strings.TrimPrefix(key, "\ufeff")
		}
		header[key] = i
	}
	codeIndex, hasCode := header["code"]
	statusIndex, hasStatus := header["status"]
	usesIndex, hasUses := header["uses"]
	if !hasCode || !hasStatus || !hasUses {
		return nil, fmt.Errorf("%s must have code,status,uses headers", path)
	}

	records := make([]friendCodeRecord, 0, len(rows)-1)
	for _, row := range rows[1:] {
		code := csvColumn(row, codeIndex)
		if code == "" {
			continue
		}
		uses, _ := strconv.Atoi(csvColumn(row, usesIndex))
		records = append(records, friendCodeRecord{
			Code:   code,
			Status: strings.ToUpper(csvColumn(row, statusIndex)),
			Uses:   uses,
		})
	}
	return records, nil
}

func saveFriendCodeRecords(path string, records []friendCodeRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	writer := csv.NewWriter(tmp)
	if err := writer.Write([]string{"code", "status", "uses"}); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	for _, record := range records {
		if err := writer.Write([]string{
			strings.TrimSpace(record.Code),
			strings.ToUpper(strings.TrimSpace(record.Status)),
			strconv.Itoa(record.Uses),
		}); err != nil {
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
	if err := replaceLocalFileWithRetry(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func csvColumn(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func replaceLocalFileWithRetry(tmpName string, path string) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(100+attempt*50) * time.Millisecond)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			lastErr = err
			continue
		}
		if err := os.Rename(tmpName, path); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
