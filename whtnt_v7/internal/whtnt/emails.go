package whtnt

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const emailsFileName = "emails.txt"

var textPopLocks sync.Map

func popEmailFromEmailsTXT() (string, error) {
	email, err := popFirstTextValue(setupFilePath(emailsFileName))
	if err != nil {
		return "", err
	}
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return "", fmt.Errorf("invalid email popped from setup/%s", emailsFileName)
	}
	return strings.ToLower(email), nil
}

func popFirstTextValue(path string) (string, error) {
	path = filepath.Clean(path)
	lockValue, _ := textPopLocks.LoadOrStore(lockKey(path), &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	input, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer input.Close()

	var selected string
	var remaining []string
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		raw := scanner.Text()
		value := strings.TrimSpace(raw)
		value = strings.TrimPrefix(value, "\ufeff")
		if selected == "" && value != "" && !strings.HasPrefix(value, "#") {
			selected = value
			continue
		}
		remaining = append(remaining, raw)
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if selected == "" {
		return "", fmt.Errorf("%s has no unused emails left", path)
	}
	if err := input.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil && dir != "." {
		return "", err
	}
	tempFile, err := os.CreateTemp(dir, "tmp-"+filepath.Base(path)+"-*.txt")
	if err != nil {
		return "", fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tempName := tempFile.Name()
	cleanup := func() {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
	}

	writer := bufio.NewWriter(tempFile)
	for _, line := range remaining {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			cleanup()
			return "", fmt.Errorf("write temp %s: %w", path, err)
		}
	}
	if err := writer.Flush(); err != nil {
		cleanup()
		return "", fmt.Errorf("flush temp %s: %w", path, err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempName)
		return "", fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := replaceFileWithRetry(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return "", fmt.Errorf("replace %s: %w", path, err)
	}

	return selected, nil
}

func setupFilePath(name string) string {
	candidates := []string{filepath.Join("setup", name)}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "setup", name))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

func lockKey(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	return strings.ToLower(filepath.Clean(absolute))
}
