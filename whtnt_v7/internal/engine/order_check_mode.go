package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

func (e *Engine) CheckAllOrders(ctx context.Context) (rows []OrderListRow, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("order check recovered panic: %v", recovered)
		}
	}()

	accounts, loadErr := e.store.LoadAccounts()
	if loadErr != nil {
		return nil, loadErr
	}
	orders, loadErr := e.store.LoadOrders()
	if loadErr != nil {
		return nil, loadErr
	}
	sessions, loadErr := e.store.LoadNormalSessions()
	if loadErr != nil {
		return nil, loadErr
	}
	accounts, sessions, _, loadErr = e.recoverAllSessionScopeFromStorageCSVs(accounts, sessions)
	if loadErr != nil {
		return nil, loadErr
	}
	setConsoleTitle(fmt.Sprintf("Order Check: %d accounts | %d stored orders", len(accounts), len(orders)))

	accounts, addedAccounts := e.ensureAccountsForOrderSessions(accounts, sessions)
	if addedAccounts {
		if saveErr := e.store.SaveAccounts(accounts); saveErr != nil {
			return nil, saveErr
		}
	}

	bannedSkipped := countKnownBannedAccounts(accounts)
	setConsoleTitle(fmt.Sprintf("Order Check: discovering live orders for %d accounts (%d banned)", len(accounts)-bannedSkipped, bannedSkipped))
	logger.Manager(fmt.Sprintf("Order Check discovering live orders for %d account(s) (%d banned).", len(accounts)-bannedSkipped, bannedSkipped))
	discovered, discoverErr := e.discoverAccountOrders(ctx, accounts, orders, sessions)
	orders = discovered.orders
	sessions = discovered.sessions
	if discoverErr != nil {
		e.sendErrorWebhook(ctx, "Order check mode error", discoverErr, nil)
	}

	saveErr := e.store.SaveOrders(orders)
	if saveErr == nil {
		saveErr = e.store.SaveNormalSessions(sessions)
	}
	if saveErr == nil && addedAccounts {
		saveErr = e.store.SaveAccounts(accounts)
	}

	rows = e.orderListRows(accounts, discovered.currentOrders)
	setConsoleTitle(fmt.Sprintf("Order Check complete: %d visible orders", len(rows)))
	if saveErr != nil {
		return rows, saveErr
	}
	return rows, discoverErr
}

func (e *Engine) ensureAccountsForOrderSessions(accounts []Account, sessions []SessionRecord) ([]Account, bool) {
	accountByID := make(map[string]bool, len(accounts))
	sessionByID := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		if strings.TrimSpace(account.ID) != "" {
			accountByID[account.ID] = true
		}
		if strings.TrimSpace(account.SessionID) != "" {
			sessionByID[account.SessionID] = true
		}
	}

	added := false
	now := e.now()
	for _, session := range sessions {
		if strings.TrimSpace(session.ID) == "" || sessionByID[session.ID] {
			continue
		}
		accountID := strings.TrimSpace(session.AccountID)
		if accountID == "" {
			accountID = "acct_" + slugForID(firstNonEmptyString(metadataValue(session.Metadata, "email"), session.ID))
		}
		if accountByID[accountID] {
			continue
		}
		createdAt := session.CreatedAt
		if createdAt.IsZero() {
			createdAt = now
		}
		updatedAt := session.UpdatedAt
		if updatedAt.IsZero() {
			updatedAt = now
		}
		accounts = append(accounts, Account{
			ID:        accountID,
			Username:  metadataValue(session.Metadata, "username"),
			Email:     metadataValue(session.Metadata, "email"),
			FirstName: metadataValue(session.Metadata, "first_name"),
			LastName:  metadataValue(session.Metadata, "last_name"),
			IP:        metadataValue(session.Metadata, "ip"),
			Proxy:     metadataValue(session.Metadata, "proxy"),
			State:     AccountStateCheckedOut,
			SessionID: session.ID,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			LastError: "Created from session CSV for order checking.",
		})
		accountByID[accountID] = true
		sessionByID[session.ID] = true
		added = true
	}
	return accounts, added
}

func slugForID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastUnderscore := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(builder.String(), "_")
	if out == "" {
		return "session"
	}
	return out
}

func (e *Engine) orderListRows(accounts []Account, orders []Order) []OrderListRow {
	accountByID := make(map[string]Account, len(accounts))
	activeAccountIDs := make(map[string]bool, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
		if !storedAccountIsKnownBanned(account) && strings.TrimSpace(account.SessionID) != "" {
			activeAccountIDs[account.ID] = true
		}
	}

	rows := make([]OrderListRow, 0, len(orders))
	for _, order := range orders {
		if !activeAccountIDs[order.AccountID] {
			continue
		}
		account := accountByID[order.AccountID]
		rows = append(rows, OrderListRow{
			AccountEmail:    account.Email,
			AccountUsername: account.Username,
			AccountCredits:  metadataCreditLabel(order.Metadata),
			Item:            e.orderItemLabel(order),
			Price:           e.orderPriceLabel(order),
			OrderDate:       e.orderDateLabel(order),
			Status:          e.orderStatusLabel(order),
			Tracking:        e.orderTrackingLabel(order),
			OrderID:         firstNonEmptyString(metadataValue(order.Metadata, "display_id"), order.ExternalOrderID, order.ID),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].AccountEmail != rows[j].AccountEmail {
			return rows[i].AccountEmail < rows[j].AccountEmail
		}
		if rows[i].OrderDate != rows[j].OrderDate {
			return rows[i].OrderDate > rows[j].OrderDate
		}
		return rows[i].Item < rows[j].Item
	})
	return rows
}

func (e *Engine) orderItemLabel(order Order) string {
	return firstNonEmptyString(
		metadataValue(order.Metadata, "items"),
		metadataValue(order.Metadata, "listing_title"),
		metadataValue(order.Metadata, "item"),
		"-",
	)
}

func (e *Engine) orderPriceLabel(order Order) string {
	value := firstNonEmptyString(
		metadataValue(order.Metadata, "subtotal"),
		metadataValue(order.Metadata, "item_price"),
		metadataValue(order.Metadata, "listing_price"),
		metadataValue(order.Metadata, "total"),
	)
	if value == "" {
		return "-"
	}
	return formatMoneyLine(value)
}

func (e *Engine) orderDateLabel(order Order) string {
	if value := metadataValue(order.Metadata, "order_date"); value != "" {
		if parsed, ok := parseOrderDate(value); ok {
			return parsed.In(e.location).Format("2006-01-02 15:04")
		}
		return value
	}
	if !order.CreatedAt.IsZero() {
		return order.CreatedAt.In(e.location).Format("2006-01-02 15:04")
	}
	return "-"
}

func parseOrderDate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func (e *Engine) orderStatusLabel(order Order) string {
	return firstNonEmptyString(
		metadataValue(order.Metadata, "pretty_status"),
		metadataValue(order.Metadata, "overview_status"),
		string(order.State),
		"-",
	)
}

func (e *Engine) orderTrackingLabel(order Order) string {
	return firstNonEmptyString(
		order.TrackingNumber,
		metadataValue(order.Metadata, "tracking_number"),
		metadataValue(order.Metadata, "tracking_url"),
		"-",
	)
}
