package whtnt

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"rnldo.dev/wnshell/internal/appconfig"
	"strings"
	"sync"
	"time"
)

type AddressFields struct {
	AddressLine1 string
	AddressLine2 string
	Town         string
	Postcode     string
	FirstName    string
	LastName     string
}

type CardDetails struct {
	Number   string
	CVC      string
	ExpYear  string
	ExpMonth string
	Postcode string
}

var csvPopLocks sync.Map

func popAddressFromProfilesCSV(lineNumber int) (AddressFields, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return AddressFields{}, err
	}
	values, err := popCSVRecord(cfg.Engine.ProfilesCSVPath, lineNumber, []string{
		"address line 1",
		"address line 2",
		"town",
		"postcode",
		"last name",
	})
	if err != nil {
		return AddressFields{}, err
	}

	return AddressFields{
		AddressLine1: values["address line 1"],
		AddressLine2: values["address line 2"],
		Town:         values["town"],
		Postcode:     values["postcode"],
		FirstName:    values["first name"],
		LastName:     values["last name"],
	}, nil
}

func popCardFromCardsCSV(lineNumber int) (CardDetails, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return CardDetails{}, err
	}
	values, err := popCSVRecord(cfg.Engine.CardsCSVPath, lineNumber, []string{
		"cardnumber",
		"cardcvc",
		"cardexpyear",
		"cardexpmonth",
	})
	if err != nil {
		return CardDetails{}, err
	}

	return CardDetails{
		Number:   values["cardnumber"],
		CVC:      values["cardcvc"],
		ExpYear:  values["cardexpyear"],
		ExpMonth: values["cardexpmonth"],
	}, nil
}

func popCSVRecord(path string, lineNumber int, requiredColumns []string) (map[string]string, error) {
	path = filepath.Clean(path)
	lockValue, _ := csvPopLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if lineNumber < 1 {
		return nil, fmt.Errorf("lineNumber must be >= 1, got %d", lineNumber)
	}

	input, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	reader := csv.NewReader(input)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		_ = input.Close()
		if err == io.EOF {
			return nil, fmt.Errorf("%s is empty", path)
		}
		return nil, fmt.Errorf("read %s header: %w", path, err)
	}

	columnIndex := make(map[string]int, len(header))
	for index, column := range header {
		key := strings.ToLower(strings.TrimSpace(column))
		if index == 0 {
			key = strings.TrimPrefix(key, "\ufeff")
		}
		if key == "" {
			continue
		}
		if _, exists := columnIndex[key]; !exists {
			columnIndex[key] = index
		}
	}

	requiredIndex := make(map[string]int, len(requiredColumns))
	for _, name := range requiredColumns {
		index, exists := columnIndex[name]
		if !exists {
			_ = input.Close()
			return nil, fmt.Errorf("missing required column %q in %s", name, path)
		}
		requiredIndex[name] = index
	}

	dir := filepath.Dir(path)
	tempFile, err := os.CreateTemp(dir, "tmp-"+filepath.Base(path)+"-*.csv")
	if err != nil {
		_ = input.Close()
		return nil, fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tempName := tempFile.Name()

	writer := csv.NewWriter(tempFile)
	cleanupTemp := func() {
		writer.Flush()
		_ = tempFile.Close()
		_ = os.Remove(tempName)
	}

	if err := writer.Write(header); err != nil {
		_ = input.Close()
		cleanupTemp()
		return nil, fmt.Errorf("write %s temp header: %w", path, err)
	}

	var (
		currentRow int
		selected   map[string]string
	)

	for {
		row, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			_ = input.Close()
			cleanupTemp()
			return nil, fmt.Errorf("read %s data row: %w", path, err)
		}

		currentRow++
		if currentRow == lineNumber {
			selected = make(map[string]string, len(requiredColumns))
			for _, name := range requiredColumns {
				index := requiredIndex[name]
				if index < len(row) {
					selected[name] = strings.TrimSpace(row[index])
				}
			}
			continue
		}

		if err := writer.Write(row); err != nil {
			_ = input.Close()
			cleanupTemp()
			return nil, fmt.Errorf("write %s temp row: %w", path, err)
		}
	}

	if err := input.Close(); err != nil {
		cleanupTemp()
		return nil, fmt.Errorf("close %s: %w", path, err)
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		cleanupTemp()
		return nil, fmt.Errorf("flush %s temp file: %w", path, err)
	}

	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempName)
		return nil, fmt.Errorf("close temp file for %s: %w", path, err)
	}

	if selected == nil {
		_ = os.Remove(tempName)
		return nil, fmt.Errorf("requested lineNumber %d but %s has only %d data rows", lineNumber, path, currentRow)
	}

	var lastErr error
	for attempt := 0; attempt < 10; attempt++ {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			lastErr = fmt.Errorf("remove original %s: %w", path, err)
		} else if err := os.Rename(tempName, path); err != nil {
			lastErr = fmt.Errorf("replace %s: %w", path, err)
		} else {
			return selected, nil
		}

		time.Sleep(150 * time.Millisecond)
	}

	_ = os.Remove(tempName)
	return nil, lastErr
}
