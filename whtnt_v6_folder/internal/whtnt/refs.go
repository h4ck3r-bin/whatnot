package whtnt

import (
	"encoding/csv"
	"math/rand"
	"os"
	"strings"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/referrors"
)

// resolveReferralCode returns the code sent as referal_code during register, falling back
// to a random entry from the friend-code list when the engine did not assign one.
func (m *Module) resolveReferralCode() string {
	if code := strings.TrimSpace(m.referralCode); code != "" {
		return code
	}
	codes := referralCodes()
	code := codes[rand.Intn(len(codes))]
	m.referralCode = code
	return code
}

// registerReferralError maps a rejected register response onto the referral error the
// engine already uses to rotate friend codes.
func registerReferralError(code string, body string) (referrors.ApplyError, bool) {
	if !strings.Contains(strings.ToLower(body), "refer") {
		return referrors.ApplyError{}, false
	}
	return referrors.InvalidReferrer(code, body), true
}

func referralCodes() []string {
	cfg, err := appconfig.Load()
	if err == nil {
		if codes, loadErr := loadReferralCodeCSV(cfg.Engine.FriendCodesPath); loadErr == nil && len(codes) > 0 {
			return codes
		}
	}
	return []string{"kennethsmi92711"}
}

func loadReferralCodeCSV(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
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
		return nil, nil
	}

	codes := make([]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		code := referralColumn(row, codeIndex)
		status := strings.ToUpper(referralColumn(row, statusIndex))
		uses := referralColumn(row, usesIndex)
		if code == "" {
			continue
		}
		if status == "ACTIVE" || (status == "TEMP" && uses != "" && uses != "0") {
			codes = append(codes, code)
		}
	}
	return codes, nil
}

func referralColumn(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}
