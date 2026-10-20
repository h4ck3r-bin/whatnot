package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"

type sessionAuditTarget struct {
	AccountIndex int
	RowIndex     int
	Account      Account
	Session      SessionRecord
}

type sessionAuditResult struct {
	Session SessionRecord
	Healthy bool
	Err     error
}

func (e *Engine) CheckSessions(ctx context.Context, csvPath string) (SessionCheckSummary, error) {
	csvPath = strings.TrimSpace(csvPath)
	if csvPath != "" {
		return e.checkSessionsFromCSV(ctx, preparedCSVPath(csvPath))
	}
	return e.checkAllStoredSessions(ctx)
}

func (e *Engine) checkAllStoredSessions(ctx context.Context) (SessionCheckSummary, error) {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return SessionCheckSummary{}, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return SessionCheckSummary{}, err
	}
	accounts, sessions, _, err = e.recoverAllSessionScopeFromStorageCSVs(accounts, sessions)
	if err != nil {
		return SessionCheckSummary{}, err
	}

	summary := SessionCheckSummary{}
	retryTargets := []sessionAuditTarget{}
	bannedSkipped := countKnownBannedAccounts(accounts)
	logger.Manager(fmt.Sprintf("Check Sessions checking %d account(s) (%d banned).", len(accounts)-bannedSkipped, bannedSkipped))
	updateSessionCheckTitle("Check Sessions", len(accounts), summary, false)
	for index := range accounts {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if storedAccountIsKnownBanned(accounts[index]) {
			summary.Banned++
			summary.Skipped++
			updateSessionCheckTitle("Check Sessions", len(accounts), summary, false)
			continue
		}
		session, ok := findSessionForAccount(sessions, accounts[index])
		if !ok {
			summary.Missing++
			updateSessionCheckTitle("Check Sessions", len(accounts), summary, false)
			continue
		}
		target := sessionAuditTarget{AccountIndex: index, Account: accounts[index], Session: session}
		result := e.auditSession(ctx, target)
		summary.Checked++
		if result.Err == nil && result.Healthy {
			if accounts[index].State == AccountStateFailed {
				accounts[index].State = AccountStateSignedUp
			}
			accounts[index].LastError = ""
			accounts[index].UpdatedAt = e.now()
			sessions = upsertSession(sessions, result.Session)
			summary.Healthy++
		} else if isDefiniteSessionBan(result.Err, result.Healthy) {
			e.markAccountSessionBanned(&accounts[index], result.Err)
			summary.Banned++
		} else {
			accounts[index].LastError = sessionAuditError(result.Err, result.Healthy)
			accounts[index].UpdatedAt = e.now()
			retryTargets = append(retryTargets, target)
		}
		if err := e.saveSessionAuditStorage(accounts, sessions); err != nil {
			return summary, err
		}
		updateSessionCheckTitle("Check Sessions", len(accounts), summary, false)
	}

	if len(retryTargets) > 0 {
		logger.Manager(fmt.Sprintf("Retrying %d session check failure(s) with fresh clients.", len(retryTargets)))
	}
	for _, target := range retryTargets {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		accounts, err = e.store.LoadAccounts()
		if err != nil {
			return summary, err
		}
		sessions, err = e.store.LoadNormalSessions()
		if err != nil {
			return summary, err
		}
		if target.AccountIndex < 0 || target.AccountIndex >= len(accounts) {
			summary.Missing++
			updateSessionCheckTitle("Check Sessions retry", len(accounts)+len(retryTargets), summary, false)
			continue
		}
		target.Account = accounts[target.AccountIndex]
		if session, ok := findSessionForAccount(sessions, target.Account); ok {
			target.Session = session
		}
		result := e.auditSession(ctx, target)
		summary.Checked++
		if result.Err == nil && result.Healthy {
			if accounts[target.AccountIndex].State == AccountStateFailed {
				accounts[target.AccountIndex].State = AccountStateSignedUp
			}
			accounts[target.AccountIndex].LastError = ""
			accounts[target.AccountIndex].UpdatedAt = e.now()
			sessions = upsertSession(sessions, result.Session)
			summary.Healthy++
		} else if isDefiniteSessionBan(result.Err, result.Healthy) {
			e.markAccountSessionBanned(&accounts[target.AccountIndex], result.Err)
			summary.Banned++
		} else {
			accounts[target.AccountIndex].LastError = sessionAuditError(result.Err, result.Healthy)
			accounts[target.AccountIndex].UpdatedAt = e.now()
			summary.Failed++
		}
		if err := e.saveSessionAuditStorage(accounts, sessions); err != nil {
			return summary, err
		}
		updateSessionCheckTitle("Check Sessions retry", len(accounts)+len(retryTargets), summary, false)
	}

	setConsoleTitle(fmt.Sprintf("Check Sessions: discovering orders | %d healthy | %d banned | %d failed", summary.Healthy, summary.Banned, summary.Failed))
	accounts, orders, sessions, reviewCount, discoverErr := e.discoverOrdersForSessionCheck(ctx, accounts, sessions, nil)
	if discoverErr != nil {
		logger.ManagerError("Session check order discovery had errors: " + discoverErr.Error())
		e.sendErrorWebhook(ctx, "Session check order discovery error", discoverErr, nil)
	}
	summary.Orders = len(orders)
	summary.Reviews = reviewCount
	if err := e.store.SaveOrders(orders); err != nil {
		return summary, err
	}
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return summary, err
	}

	accounts, _ = e.store.LoadAccounts()
	sessions, _ = e.store.LoadNormalSessions()
	summary.Referrers = buildReferrerSessionSummary(accounts, sessions, nil)
	setConsoleTitle(fmt.Sprintf("Check Sessions complete: %d healthy | %d banned | %d failed | %d orders | %d reviews", summary.Healthy, summary.Banned, summary.Failed, summary.Orders, summary.Reviews))
	logger.Manager(fmt.Sprintf("Session check complete. Healthy=%d banned=%d failed=%d missing=%d skipped=%d.", summary.Healthy, summary.Banned, summary.Failed, summary.Missing, summary.Skipped))
	return summary, nil
}

func (e *Engine) checkSessionsFromCSV(ctx context.Context, path string) (SessionCheckSummary, error) {
	headers, rows, err := readPreparedTaskRows(path)
	if err != nil {
		return SessionCheckSummary{}, err
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return SessionCheckSummary{}, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return SessionCheckSummary{}, err
	}

	summary := SessionCheckSummary{}
	retryTargets := []sessionAuditTarget{}
	bannedSkipped := countKnownBannedPreparedRows(accounts, rows)
	logger.Manager(fmt.Sprintf("CSV Session Check checking %d row(s) (%d banned).", len(rows)-bannedSkipped, bannedSkipped))
	updateSessionCheckTitle("CSV Session Check", len(rows), summary, false)
	for index := range rows {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if accountIndex := findPreparedAccountIndex(accounts, rows[index]); accountIndex >= 0 && storedAccountIsKnownBanned(accounts[accountIndex]) {
			markCSVSessionBanned(rows[index], firstNonEmptyString(accounts[accountIndex].LastError, "stored account is banned"), e.now())
			summary.Banned++
			summary.Skipped++
			if err := writePreparedTaskRows(path, headers, rows); err != nil {
				return summary, err
			}
			updateSessionCheckTitle("CSV Session Check", len(rows), summary, false)
			continue
		}
		target, ok, err := e.csvSessionAuditTarget(rows[index], accounts, sessions)
		if err != nil {
			rows[index]["status"] = "missing"
			rows[index]["last_error"] = err.Error()
			rows[index]["updated_at"] = e.now().Format(timeRFC3339)
			summary.Missing++
			if err := writePreparedTaskRows(path, headers, rows); err != nil {
				return summary, err
			}
			updateSessionCheckTitle("CSV Session Check", len(rows), summary, false)
			continue
		}
		if !ok {
			summary.Skipped++
			updateSessionCheckTitle("CSV Session Check", len(rows), summary, false)
			continue
		}
		target.RowIndex = index
		result := e.auditSession(ctx, target)
		summary.Checked++
		if result.Err == nil && result.Healthy {
			if accounts[target.AccountIndex].State == AccountStateFailed {
				accounts[target.AccountIndex].State = AccountStateSignedUp
			}
			accounts[target.AccountIndex].LastError = ""
			accounts[target.AccountIndex].UpdatedAt = e.now()
			sessions = upsertSession(sessions, result.Session)
			markCSVSessionHealthy(rows[index])
			summary.Healthy++
		} else if isDefiniteSessionBan(result.Err, result.Healthy) {
			e.markAccountSessionBanned(&accounts[target.AccountIndex], result.Err)
			markCSVSessionBanned(rows[index], sessionAuditError(result.Err, result.Healthy), e.now())
			summary.Banned++
		} else {
			rows[index]["status"] = "retry_pending"
			rows[index]["last_error"] = sessionAuditError(result.Err, result.Healthy)
			rows[index]["updated_at"] = e.now().Format(timeRFC3339)
			accounts[target.AccountIndex].LastError = rows[index]["last_error"]
			accounts[target.AccountIndex].UpdatedAt = e.now()
			retryTargets = append(retryTargets, target)
		}
		if err := e.saveSessionAuditStorage(accounts, sessions); err != nil {
			return summary, err
		}
		if err := writePreparedTaskRows(path, headers, rows); err != nil {
			return summary, err
		}
		updateSessionCheckTitle("CSV Session Check", len(rows), summary, false)
	}

	if len(retryTargets) > 0 {
		logger.Manager(fmt.Sprintf("Retrying %d CSV session check failure(s) with fresh clients.", len(retryTargets)))
	}
	for _, target := range retryTargets {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		accounts, err = e.store.LoadAccounts()
		if err != nil {
			return summary, err
		}
		sessions, err = e.store.LoadNormalSessions()
		if err != nil {
			return summary, err
		}
		if target.AccountIndex < 0 || target.AccountIndex >= len(accounts) || target.RowIndex < 0 || target.RowIndex >= len(rows) {
			summary.Missing++
			updateSessionCheckTitle("CSV Session Check retry", len(rows)+len(retryTargets), summary, false)
			continue
		}
		target.Account = accounts[target.AccountIndex]
		if session, ok := findSessionForAccount(sessions, target.Account); ok {
			target.Session = session
		}
		result := e.auditSession(ctx, target)
		summary.Checked++
		if result.Err == nil && result.Healthy {
			if accounts[target.AccountIndex].State == AccountStateFailed {
				accounts[target.AccountIndex].State = AccountStateSignedUp
			}
			accounts[target.AccountIndex].LastError = ""
			accounts[target.AccountIndex].UpdatedAt = e.now()
			sessions = upsertSession(sessions, result.Session)
			markCSVSessionHealthy(rows[target.RowIndex])
			summary.Healthy++
		} else if isDefiniteSessionBan(result.Err, result.Healthy) {
			e.markAccountSessionBanned(&accounts[target.AccountIndex], result.Err)
			markCSVSessionBanned(rows[target.RowIndex], sessionAuditError(result.Err, result.Healthy), e.now())
			summary.Banned++
		} else {
			accounts[target.AccountIndex].LastError = sessionAuditError(result.Err, result.Healthy)
			accounts[target.AccountIndex].UpdatedAt = e.now()
			rows[target.RowIndex]["status"] = "check_failed"
			rows[target.RowIndex]["last_error"] = accounts[target.AccountIndex].LastError
			rows[target.RowIndex]["updated_at"] = e.now().Format(timeRFC3339)
			summary.Failed++
		}
		if err := e.saveSessionAuditStorage(accounts, sessions); err != nil {
			return summary, err
		}
		if err := writePreparedTaskRows(path, headers, rows); err != nil {
			return summary, err
		}
		updateSessionCheckTitle("CSV Session Check retry", len(rows)+len(retryTargets), summary, false)
	}

	setConsoleTitle(fmt.Sprintf("CSV Session Check: discovering orders | %d healthy | %d banned | %d failed", summary.Healthy, summary.Banned, summary.Failed))
	accounts, orders, sessions, reviewCount, discoverErr := e.discoverOrdersForSessionCheck(ctx, accounts, sessions, rows)
	if discoverErr != nil {
		logger.ManagerError("CSV session check order discovery had errors: " + discoverErr.Error())
		e.sendErrorWebhook(ctx, "CSV session check order discovery error", discoverErr, nil)
	}
	summary.Orders = len(orders)
	summary.Reviews = reviewCount
	updateCSVOrderStatuses(rows, accounts, orders)
	if err := e.store.SaveOrders(orders); err != nil {
		return summary, err
	}
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return summary, err
	}
	if err := writePreparedTaskRows(path, headers, rows); err != nil {
		return summary, err
	}

	accounts, _ = e.store.LoadAccounts()
	sessions, _ = e.store.LoadNormalSessions()
	summary.Referrers = buildReferrerSessionSummary(accounts, sessions, rows)
	setConsoleTitle(fmt.Sprintf("CSV Session Check complete: %d healthy | %d banned | %d failed | %d orders | %d reviews", summary.Healthy, summary.Banned, summary.Failed, summary.Orders, summary.Reviews))
	logger.Manager(fmt.Sprintf("CSV session check complete. Healthy=%d banned=%d failed=%d missing=%d skipped=%d.", summary.Healthy, summary.Banned, summary.Failed, summary.Missing, summary.Skipped))
	return summary, nil
}

func updateSessionCheckTitle(mode string, total int, summary SessionCheckSummary, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("%s complete: %d checked | %d healthy | %d banned | %d failed", mode, summary.Checked, summary.Healthy, summary.Banned, summary.Failed))
		return
	}
	setConsoleTitle(fmt.Sprintf("%s: %d/%d checks | %d healthy | %d banned | %d failed | %d missing", mode, summary.Checked, total, summary.Healthy, summary.Banned, summary.Failed, summary.Missing))
}

func (e *Engine) csvSessionAuditTarget(row map[string]string, accounts []Account, sessions []SessionRecord) (sessionAuditTarget, bool, error) {
	accountIndex := findPreparedAccountIndex(accounts, row)
	if accountIndex < 0 {
		return sessionAuditTarget{}, false, fmt.Errorf("stored account not found")
	}
	session, ok := findSessionForAccount(sessions, accounts[accountIndex])
	if !ok {
		return sessionAuditTarget{}, false, fmt.Errorf("stored session not found")
	}
	return sessionAuditTarget{AccountIndex: accountIndex, Account: accounts[accountIndex], Session: session}, true, nil
}

func (e *Engine) auditSession(ctx context.Context, target sessionAuditTarget) sessionAuditResult {
	refreshed, healthy, err := e.flows.RefreshSession(ctx, target.Session)
	if err != nil || !healthy {
		return sessionAuditResult{Session: target.Session, Healthy: healthy, Err: err}
	}
	if refreshed.ID == "" {
		refreshed.ID = target.Session.ID
	}
	if refreshed.AccountID == "" {
		refreshed.AccountID = target.Account.ID
	}
	if refreshed.CreatedAt.IsZero() {
		refreshed.CreatedAt = target.Session.CreatedAt
	}
	refreshed.UpdatedAt = e.now()
	if err := validateRefreshedSession(target.Session, refreshed); err != nil {
		return sessionAuditResult{Session: target.Session, Healthy: false, Err: err}
	}
	e.handleRewardCreditUpdate(ctx, target.Account, target.Session, refreshed)
	return sessionAuditResult{Session: refreshed, Healthy: true}
}

func (e *Engine) saveSessionAuditStorage(accounts []Account, sessions []SessionRecord) error {
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	return e.store.SaveAccounts(accounts)
}

func markCSVSessionHealthy(row map[string]string) {
	if strings.EqualFold(strings.TrimSpace(row["status"]), "reward_detected") || strings.EqualFold(strings.TrimSpace(row["status"]), "retry_pending") || strings.EqualFold(strings.TrimSpace(row["status"]), "check_failed") {
		row["status"] = "ready"
	}
	row["last_error"] = ""
}

func markCSVSessionBanned(row map[string]string, reason string, now time.Time) {
	row["status"] = "banned"
	row["last_error"] = strings.TrimSpace(reason)
	row["updated_at"] = now.Format(timeRFC3339)
}

func (e *Engine) markAccountSessionBanned(account *Account, err error) {
	account.State = AccountStateFailed
	account.LastError = sessionAuditError(err, false)
	account.UpdatedAt = e.now()
}

func isDefiniteSessionBan(err error, healthy bool) bool {
	if isMissingSessionMaterialError(err) {
		return false
	}
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, transient := range []string{"410 gone", "proxy tunnel", "proxy unauthorized", "proxy authentication", "proxy-authenticate", "407", "failed to dial", "dial tcp", "timeout", "tls", "dns", "no such host", "connection reset", "eof"} {
		if strings.Contains(text, transient) {
			return false
		}
	}
	if strings.Contains(text, "banned") || strings.Contains(text, "invalid session") || strings.Contains(text, "account disabled") || strings.Contains(text, "account was banned") || strings.Contains(text, "unauthorized") || strings.Contains(text, "401") || strings.Contains(text, "403") || strings.Contains(text, "forbidden") {
		return true
	}
	return false
}

func isMissingSessionMaterialError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, needle := range []string{
		"stored session not found",
		"stored session missing",
		"missing normal session",
		"no stored account/session matched",
		"client session is not available",
		"missing access_token",
		"missing access token",
		"missing _sharedid",
		"missing cookie",
		"session id is required",
		"session_id is required",
		"app session id is required",
		"request id is required",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func sessionAuditError(err error, healthy bool) string {
	if err != nil {
		return "session refresh failed: " + err.Error()
	}
	if !healthy {
		return "session refresh returned unhealthy"
	}
	return ""
}

type referrerCounter struct {
	Healthy int
	Banned  int
}

func buildReferrerSessionSummary(accounts []Account, sessions []SessionRecord, rows []map[string]string) []ReferrerSessionSummary {
	include := map[string]bool{}
	if rows != nil {
		for _, row := range rows {
			index := findPreparedAccountIndex(accounts, row)
			if index < 0 {
				continue
			}
			key := accountSummaryKey(accounts[index])
			if key != "" {
				include[key] = true
			}
		}
	}

	counts := map[string]*referrerCounter{}
	seen := map[string]bool{}
	for _, account := range accounts {
		key := accountSummaryKey(account)
		if key == "" || seen[key] {
			continue
		}
		if rows != nil && !include[key] {
			continue
		}
		seen[key] = true

		referrer := strings.TrimSpace(account.MasterUsernameUsed)
		if referrer == "" {
			referrer = "(unknown)"
		}
		counter := counts[referrer]
		if counter == nil {
			counter = &referrerCounter{}
			counts[referrer] = counter
		}
		if account.State == AccountStateFailed {
			counter.Banned++
			continue
		}
		if _, ok := findSessionForAccount(sessions, account); ok {
			counter.Healthy++
		}
	}

	out := make([]ReferrerSessionSummary, 0, len(counts))
	for referrer, counter := range counts {
		if counter.Healthy == 0 {
			continue
		}
		out = append(out, ReferrerSessionSummary{
			Referrer: referrer,
			Healthy:  counter.Healthy,
			Banned:   counter.Banned,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Healthy != out[j].Healthy {
			return out[i].Healthy > out[j].Healthy
		}
		return strings.ToLower(out[i].Referrer) < strings.ToLower(out[j].Referrer)
	})
	return out
}

func accountSummaryKey(account Account) string {
	if strings.TrimSpace(account.ID) != "" {
		return strings.TrimSpace(account.ID)
	}
	if strings.TrimSpace(account.Email) != "" {
		return strings.ToLower(strings.TrimSpace(account.Email))
	}
	return strings.TrimSpace(account.SessionID)
}

func (e *Engine) discoverOrdersForSessionCheck(ctx context.Context, accounts []Account, sessions []SessionRecord, rows []map[string]string) ([]Account, []Order, []SessionRecord, int, error) {
	orders, err := e.store.LoadOrders()
	if err != nil {
		return accounts, nil, sessions, 0, err
	}
	targets := accounts
	if rows != nil {
		orders = hydrateOrdersFromPreparedRows(orders, accounts, rows, e.now())
		targets = accountsForPreparedRows(accounts, rows)
	}
	discovered, err := e.discoverAccountOrders(ctx, targets, orders, sessions)
	orders = discovered.orders
	sessions = discovered.sessions
	reviewCount := 0
	for i := range discovered.currentOrders {
		if discovered.currentOrders[i].State == OrderStateReviewReady && !discovered.currentOrders[i].ReviewCompleted {
			reviewCount++
		}
	}
	return accounts, orders, sessions, reviewCount, err
}

func (e *Engine) logStoredActiveOrders(accounts []Account, orders []Order) {
	accountByID := map[string]Account{}
	activeAccounts := map[string]bool{}
	for _, account := range accounts {
		if strings.TrimSpace(account.ID) == "" {
			continue
		}
		accountByID[account.ID] = account
		activeAccounts[account.ID] = true
	}
	for _, order := range orders {
		if strings.TrimSpace(order.AccountID) == "" || !activeAccounts[order.AccountID] {
			continue
		}
		e.logDetectedOrder(accountByID[order.AccountID], order)
	}
}

func hydrateOrdersFromPreparedRows(orders []Order, accounts []Account, rows []map[string]string, now time.Time) []Order {
	for _, row := range rows {
		status := strings.ToLower(strings.TrimSpace(row["status"]))
		if status != "checked_out" && status != "pending_review" && status != "in_progress" && status != "reviewed" {
			continue
		}
		accountIndex := findPreparedAccountIndex(accounts, row)
		if accountIndex < 0 {
			continue
		}
		orderIDs := preparedRowOrderIDs(row)
		for _, orderID := range orderIDs {
			orderID = strings.TrimSpace(orderID)
			if orderID == "" {
				continue
			}
			order := Order{
				ID:              normalizeStoredOrderID(orderID),
				AccountID:       accounts[accountIndex].ID,
				ExternalOrderID: strings.TrimPrefix(orderID, "order_"),
				State:           OrderStateInProgress,
				CreatedAt:       now,
				UpdatedAt:       now,
				Metadata:        preparedRowOrderMetadata(row),
			}
			if status == "pending_review" {
				order.State = OrderStateReviewReady
			}
			if status == "reviewed" {
				order.State = OrderStateReviewed
				order.ReviewCompleted = true
			}
			orders = upsertOrder(orders, order)
		}
	}
	return orders
}

func preparedRowOrderIDs(row map[string]string) []string {
	values := []string{row["order_ids"]}
	if strings.HasPrefix(strings.TrimSpace(row["order_status"]), "order_") {
		values = append(values, row["order_status"])
	}
	out := []string{}
	for _, value := range values {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '|' || r == ',' || r == ';'
		}) {
			part = strings.TrimSpace(part)
			if part != "" {
				out = appendUniqueString(out, part)
			}
		}
	}
	return out
}

func normalizeStoredOrderID(orderID string) string {
	orderID = strings.TrimSpace(orderID)
	if strings.HasPrefix(orderID, "order_") {
		return orderID
	}
	return "order_" + orderID
}

func preparedRowOrderMetadata(row map[string]string) map[string]any {
	metadata := map[string]any{}
	productNames, total := preparedRowProductSummary(row["products_json"])
	if len(productNames) > 0 {
		metadata["items"] = productNames
		metadata["listing_title"] = productNames[0]
	}
	if total > 0 {
		metadata["subtotal"] = fmt.Sprintf("%d GBP", total)
		metadata["total"] = fmt.Sprintf("%d GBP", total)
	}
	if reward := strings.TrimSpace(row["reward_label"]); reward != "" {
		metadata["reward_label"] = reward
	}
	return metadata
}

func preparedRowProductSummary(raw string) ([]string, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, 0
	}
	var products []map[string]any
	if err := json.Unmarshal([]byte(raw), &products); err != nil {
		return nil, 0
	}
	names := []string{}
	total := 0
	for _, product := range products {
		if name := cleanMetadataString(fmt.Sprint(product["ProductName"])); name != "" {
			names = append(names, name)
		}
		switch value := product["ProductPrice"].(type) {
		case float64:
			total += int(value)
		case int:
			total += value
		case string:
			var parsed int
			if _, err := fmt.Sscanf(value, "%d", &parsed); err == nil {
				total += parsed
			}
		}
	}
	return names, total
}

func accountsForPreparedRows(accounts []Account, rows []map[string]string) []Account {
	out := []Account{}
	seen := map[string]bool{}
	for _, row := range rows {
		index := findPreparedAccountIndex(accounts, row)
		if index < 0 {
			continue
		}
		key := accountSummaryKey(accounts[index])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, accounts[index])
	}
	return out
}

func updateCSVOrderStatuses(rows []map[string]string, accounts []Account, orders []Order) {
	for i := range rows {
		index := findPreparedAccountIndex(accounts, rows[i])
		if index < 0 {
			continue
		}
		accountOrders := ordersForAccount(orders, accounts[index].ID)
		rows[i]["order_ids"] = strings.Join(orderIDsForCSV(accountOrders), "|")
		rows[i]["order_status"] = orderStatusForCSV(accountOrders)
	}
}

func ordersForAccount(orders []Order, accountID string) []Order {
	out := []Order{}
	for _, order := range orders {
		if order.AccountID == accountID {
			out = append(out, order)
		}
	}
	return out
}

func orderIDsForCSV(orders []Order) []string {
	out := []string{}
	for _, order := range orders {
		out = appendUniqueString(out, order.ID)
	}
	return out
}

func orderStatusForCSV(orders []Order) string {
	if len(orders) == 0 {
		return "clear"
	}
	hasProgress := false
	for _, order := range orders {
		if order.State == OrderStateReviewReady && !order.ReviewCompleted {
			return "pending_review"
		}
		if order.State == OrderStateReviewed || order.ReviewCompleted {
			continue
		}
		hasProgress = true
	}
	if hasProgress {
		return "in_progress"
	}
	return "reviewed"
}
