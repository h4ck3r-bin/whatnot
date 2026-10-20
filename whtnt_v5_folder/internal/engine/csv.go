package engine

import (
	"encoding/csv"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var orderExportMu sync.Mutex

func appendCheckoutLabel(path string, account Account, order Order, firstName string, lastName string, timestamp time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	exists, err := checkoutLabelExists(path, account.ID, order.ID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, statErr := os.Stat(path)
	needsHeader := errors.Is(statErr, os.ErrNotExist)

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if needsHeader {
		if err := writer.Write([]string{
			"first_name",
			"last_name",
			"email",
			"account_id",
			"order_id",
			"master_username_used",
			"timestamp",
		}); err != nil {
			return err
		}
	}

	return writer.Write([]string{
		firstName,
		lastName,
		account.Email,
		account.ID,
		order.ID,
		account.MasterUsernameUsed,
		timestamp.Format(time.RFC3339),
	})
}

func checkoutLabelExists(path string, accountID string, orderID string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
	accountIndex := -1
	orderIndex := -1
	for i, column := range header {
		switch column {
		case "account_id":
			accountIndex = i
		case "order_id":
			orderIndex = i
		}
	}
	if accountIndex < 0 || orderIndex < 0 {
		return false, nil
	}
	for {
		row, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				return false, nil
			}
			return false, err
		}
		if accountIndex < len(row) && orderIndex < len(row) && row[accountIndex] == accountID && row[orderIndex] == orderID {
			return true, nil
		}
	}
}

func appendOrderExport(account Account, order Order, firstName string, lastName string, timestamp time.Time) error {
	orderExportMu.Lock()
	defer orderExportMu.Unlock()

	path := executableCSVPath("orders.csv")
	header := orderExportHeader()
	rows, err := readCSVRows(path)
	if err != nil {
		return err
	}

	next := []string{
		timestamp.Format(time.RFC3339),
		account.Email,
		account.Username,
		account.ID,
		string(account.State),
		firstName,
		lastName,
		account.MasterUsernameUsed,
		order.ID,
		order.ExternalOrderID,
		metadataValue(order.Metadata, "display_id"),
		string(order.State),
		effectiveOrderItem(order),
		effectiveOrderPrice(order),
		formatMoneyLine(metadataValue(order.Metadata, "shipping")),
		formatMoneyLine(metadataValue(order.Metadata, "total")),
		metadataValue(order.Metadata, "seller_username"),
		metadataValue(order.Metadata, "listing_id"),
		metadataValue(order.Metadata, "product_id"),
		order.TrackingNumber,
		metadataValue(order.Metadata, "tracking_url"),
		metadataValue(order.Metadata, "shipping_service"),
		metadataValue(order.Metadata, "order_date"),
		account.IP,
		account.Proxy,
	}

	replaced := false
	accountIndex := columnIndex(header, "account_id")
	orderIndex := columnIndex(header, "order_id")
	for i := range rows {
		if accountIndex < len(rows[i]) && orderIndex < len(rows[i]) && rows[i][accountIndex] == account.ID && rows[i][orderIndex] == order.ID {
			rows[i] = next
			replaced = true
			break
		}
	}
	if !replaced {
		rows = append(rows, next)
	}
	return writeCSVRows(path, header, rows)
}

func orderExportHeader() []string {
	return []string{
		"timestamp",
		"email",
		"username",
		"account_id",
		"account_state",
		"first_name",
		"last_name",
		"master_username_used",
		"order_id",
		"external_order_id",
		"display_id",
		"order_state",
		"item",
		"item_price",
		"shipping",
		"total_paid",
		"seller_username",
		"listing_id",
		"product_id",
		"tracking_number",
		"tracking_url",
		"shipping_service",
		"order_date",
		"ip",
		"proxy",
	}
}

func effectiveOrderItem(order Order) string {
	return firstNonEmptyString(
		metadataValue(order.Metadata, "items"),
		metadataValue(order.Metadata, "listing_title"),
		metadataValue(order.Metadata, "item"),
	)
}

func effectiveOrderPrice(order Order) string {
	return formatMoneyLine(firstNonEmptyString(
		metadataValue(order.Metadata, "subtotal"),
		metadataValue(order.Metadata, "item_price"),
		metadataValue(order.Metadata, "listing_price"),
		metadataValue(order.Metadata, "total"),
	))
}

func executableCSVPath(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return name
	}
	return filepath.Join(filepath.Dir(exe), name)
}

func readCSVRows(path string) ([][]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) <= 1 {
		return nil, nil
	}
	return rows[1:], nil
}

func writeCSVRows(path string, header []string, rows [][]string) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	writer := csv.NewWriter(tmp)
	if err := writer.Write(header); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	for _, row := range rows {
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
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func columnIndex(header []string, name string) int {
	for i, column := range header {
		if strings.TrimSpace(column) == name {
			return i
		}
	}
	return -1
}

func csvRowExists(path string, keyColumn string, keyValue string, secondColumn string, secondValue string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
	firstIndex := -1
	secondIndex := -1
	for i, column := range header {
		switch strings.TrimSpace(column) {
		case keyColumn:
			firstIndex = i
		case secondColumn:
			secondIndex = i
		}
	}
	if firstIndex < 0 || secondIndex < 0 {
		return false, nil
	}
	for {
		row, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				return false, nil
			}
			return false, err
		}
		if firstIndex < len(row) && secondIndex < len(row) && row[firstIndex] == keyValue && row[secondIndex] == secondValue {
			return true, nil
		}
	}
}
