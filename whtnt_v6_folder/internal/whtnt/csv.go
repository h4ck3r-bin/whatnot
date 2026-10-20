package whtnt

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const accountsFile = "storage/sessions/whatnot_checkout_accounts.csv"
const validSessionsFile = "storage/sessions/valid_sessions.csv"

var whatnotCSVLocks sync.Map

type cookie struct {
	Name             string  `json:"name"`
	Value            string  `json:"value"`
	Domain           string  `json:"domain"`
	HostOnly         bool    `json:"hostOnly"`
	Path             string  `json:"path"`
	Secure           bool    `json:"secure"`
	HTTPOnly         bool    `json:"httpOnly"`
	SameSite         string  `json:"sameSite"`
	Session          bool    `json:"session"`
	FirstPartyDomain string  `json:"firstPartyDomain"`
	PartitionKey     any     `json:"partitionKey"`
	ExpirationDate   float64 `json:"expirationDate"`
	StoreID          any     `json:"storeId"`
}

func (m *Module) saveAccountToCsv(session string) error {
	if err := m.ensureHTTPClient("save account csv"); err != nil {
		return fmt.Errorf("save account csv: recover client session: %w", err)
	}
	parsedUrl, err := url.Parse(fmt.Sprintf("https://%s", WEB_HOST))

	if err != nil {
		return err
	}

	var accessToken string
	var refreshToken string

	for _, cookie := range m.client.GetCookieJar().Cookies(parsedUrl) {
		if len(accessToken) > 0 && len(refreshToken) > 0 {
			break
		}

		if cookie.Name == accessTokenCookieName {
			accessToken = cookie.Value
		}

		if cookie.Name == refreshTokenCookieName {
			refreshToken = cookie.Value
		}
	}

	cookies := []cookie{
		{
			Name:             "__Secure-refresh-token",
			Value:            refreshToken,
			Domain:           "www.whatnot.com",
			HostOnly:         true,
			Path:             "/",
			Secure:           true,
			HTTPOnly:         true,
			SameSite:         "lax",
			Session:          false,
			FirstPartyDomain: "",
			PartitionKey:     nil,
		},
		{
			Name:             "__Secure-access-token",
			Value:            accessToken,
			Domain:           "www.whatnot.com",
			HostOnly:         true,
			Path:             "/",
			Secure:           true,
			HTTPOnly:         true,
			SameSite:         "lax",
			Session:          false,
			FirstPartyDomain: "",
			PartitionKey:     nil,
		},
	}

	cookiesString, err := json.Marshal(cookies)

	if err != nil {
		return err
	}

	return appendCSVRecord(
		accountsFile,
		[]string{"email", "password", "username", "ip", "proxy", "access_token", "refresh_token", "session_token", "cookies_json", "updated_at"},
		[]string{
			m.account.email,
			m.account.password,
			m.account.username,
			m.ip,
			m.proxy,
			accessToken,
			refreshToken,
			session,
			string(cookiesString),
			londonTimestamp(),
		},
	)
}

func appendSessionValidation(path string, m *Module, sessionToken string, status string) error {
	if m == nil {
		return fmt.Errorf("session validation csv: module is nil")
	}
	return appendCSVRecord(
		path,
		[]string{"status", "session_token", "email", "username", "ip", "proxy", "recorded_at"},
		[]string{
			status,
			sessionToken,
			m.account.email,
			m.account.username,
			m.ip,
			m.proxy,
			londonTimestamp(),
		},
	)
}

func appendCSVRecord(path string, header []string, row []string) error {
	lockValue, _ := whatnotCSVLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	needsHeader := true
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		needsHeader = false
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	if needsHeader {
		if err := writer.Write(header); err != nil {
			return err
		}
	}
	if err := writer.Write(row); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}

func londonTimestamp() string {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		location = time.Local
	}
	return time.Now().In(location).Format(time.RFC3339Nano)
}
