package engine

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

var reviewCSVHeaders = []string{
	"review_id",
	"account_id",
	"account_email",
	"account_username",
	"account_credits",
	"order_id",
	"external_order_id",
	"display_id",
	"review_reference_id",
	"items",
	"seller_username",
	"order_status",
	"detected_at",
	"rating_1",
	"rating_2",
	"rating_3",
	"rating_4",
	"review_text",
	"status",
	"last_error",
	"submitted_at",
	"updated_at",
}

func (e *Engine) BuildBulkReviewCSV(ctx context.Context, outputPath string, scopeCSVPath string, concurrency int) (ReviewCSVSummary, error) {
	outputPath = preparedCSVPath(firstNonEmptyString(strings.TrimSpace(outputPath), "storage/reviews.csv"))
	summary := ReviewCSVSummary{Path: outputPath}
	accounts, scopedAccounts, sessions, _, err := e.accountUpdateTargets(scopeCSVPath)
	if err != nil {
		return summary, err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return summary, err
	}

	targets := make([]accountUpdateWorkTarget, 0, len(scopedAccounts))
	bannedSkipped := 0
	for index, account := range scopedAccounts {
		if storedAccountIsKnownBanned(account) {
			summary.Skipped++
			bannedSkipped++
			continue
		}
		session, ok := findSessionForAccount(sessions, account)
		if !ok {
			summary.Failed++
			logger.ManagerError("Review CSV export skipped " + firstNonEmptyString(account.Email, account.Username, account.ID) + ": stored session missing.")
			continue
		}
		targets = append(targets, accountUpdateWorkTarget{Number: index + 1, Account: account, Session: session})
	}
	concurrency = boundedAccountUtilityConcurrency(concurrency, len(targets), e.cfg.Engine.TaskConcurrency)
	logger.Manager(fmt.Sprintf("Review CSV checking %d account(s) (%d banned) in number order with %d thread(s).", len(targets), bannedSkipped, concurrency))
	updateReviewCSVBuildTitle(len(targets), summary, false)
	if err := writeGenericCSVRows(outputPath, reviewCSVHeaders, nil); err != nil {
		return summary, err
	}
	lastRows := []map[string]string{}
	lastLoggedExported := 0
	dirtyAccounts := false
	dirtySessions := false
	dirtyOrders := false
	const reviewCSVStorageFlushEvery = 10

	results := e.runReviewCSVDiscoveryWorkers(ctx, targets, orders, concurrency)
	for result := range results {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.Checked++
		accountIndex := findAccountIndex(accounts, result.Target.Account.ID)
		if result.Err != nil {
			logger.ManagerError(fmt.Sprintf("Review CSV check failed for %s: %s", firstNonEmptyString(result.Target.Account.Email, result.Target.Account.Username, result.Target.Account.ID), result.Err.Error()))
			if result.Banned && accountIndex >= 0 {
				e.markAccountSessionBanned(&accounts[accountIndex], result.Err)
			} else if accountIndex >= 0 {
				accounts[accountIndex].LastError = result.Err.Error()
				accounts[accountIndex].UpdatedAt = e.now()
			}
			summary.Failed++
			dirtyAccounts = true
			if summary.Checked%reviewCSVStorageFlushEvery == 0 {
				if err := e.flushReviewCSVAccountState(accounts, sessions, orders, dirtyAccounts, dirtySessions, dirtyOrders); err != nil {
					return summary, err
				}
				dirtyAccounts = false
				dirtySessions = false
				dirtyOrders = false
			}
			updateReviewCSVBuildTitle(len(targets), summary, false)
			continue
		}
		if accountIndex >= 0 {
			if accounts[accountIndex].State == AccountStateFailed {
				accounts[accountIndex].State = AccountStateSignedUp
			}
			accounts[accountIndex].SessionID = firstNonEmptyString(result.Session.ID, accounts[accountIndex].SessionID)
			accounts[accountIndex].LastError = ""
			accounts[accountIndex].UpdatedAt = e.now()
			dirtyAccounts = true
		}
		sessions = upsertSession(sessions, result.Session)
		dirtySessions = true
		var reviewCount int
		orders, reviewCount, err = e.applyAccountOrderResultsInMemory(ctx, result.Target.Account, result.Session, orders, result.OrderResults)
		if err != nil {
			return summary, err
		}
		if len(result.OrderResults) > 0 {
			dirtyOrders = true
		}
		if reviewCount > 0 {
			rows, err := e.reviewCSVRowsForScope(scopedAccounts)
			if err != nil {
				return summary, err
			}
			if !genericCSVRowsEqual(lastRows, rows) {
				if err := writeGenericCSVRows(outputPath, reviewCSVHeaders, rows); err != nil {
					return summary, err
				}
				lastRows = cloneGenericCSVRows(rows)
				if len(rows) != lastLoggedExported {
					logger.Manager(fmt.Sprintf("Review CSV exported %d review(s) to %s.", len(rows), outputPath))
					lastLoggedExported = len(rows)
				}
			}
			summary.Exported = len(rows)
		}
		if summary.Checked%reviewCSVStorageFlushEvery == 0 {
			if err := e.flushReviewCSVAccountState(accounts, sessions, orders, dirtyAccounts, dirtySessions, dirtyOrders); err != nil {
				return summary, err
			}
			dirtyAccounts = false
			dirtySessions = false
			dirtyOrders = false
		}
		updateReviewCSVBuildTitle(len(targets), summary, false)
	}

	if err := e.flushReviewCSVAccountState(accounts, sessions, orders, dirtyAccounts, dirtySessions, dirtyOrders); err != nil {
		return summary, err
	}
	rows, err := e.reviewCSVRowsForScope(scopedAccounts)
	if err != nil {
		return summary, err
	}
	if !genericCSVRowsEqual(lastRows, rows) {
		if err := writeGenericCSVRows(outputPath, reviewCSVHeaders, rows); err != nil {
			return summary, err
		}
	}
	summary.Exported = len(rows)
	updateReviewCSVBuildTitle(len(targets), summary, true)
	logger.Manager(fmt.Sprintf("Review CSV complete. Exported %d review(s) to %s.", summary.Exported, outputPath))
	return summary, nil
}

func (e *Engine) flushReviewCSVAccountState(accounts []Account, sessions []SessionRecord, orders []Order, dirtyAccounts bool, dirtySessions bool, dirtyOrders bool) error {
	if dirtySessions {
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return err
		}
	}
	if dirtyAccounts {
		if err := e.store.SaveAccounts(accounts); err != nil {
			return err
		}
	}
	if dirtyOrders {
		if err := e.store.SaveOrders(orders); err != nil {
			return err
		}
	}
	return nil
}

func genericCSVRowsEqual(a []map[string]string, b []map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if len(a[index]) != len(b[index]) {
			return false
		}
		for key, value := range a[index] {
			if b[index][key] != value {
				return false
			}
		}
	}
	return true
}

func cloneGenericCSVRows(rows []map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		cloned := make(map[string]string, len(row))
		for key, value := range row {
			cloned[key] = value
		}
		out = append(out, cloned)
	}
	return out
}

func (e *Engine) RunBulkReviewCSV(ctx context.Context, path string) (ReviewCSVSummary, error) {
	path = preparedCSVPath(firstNonEmptyString(strings.TrimSpace(path), "storage/reviews.csv"))
	headers, rows, err := readGenericCSVRows(path, reviewCSVHeaders)
	if err != nil {
		return ReviewCSVSummary{Path: path}, err
	}
	summary := ReviewCSVSummary{Path: path}
	updateReviewCSVRunTitle(len(rows), summary, false)
	for index := range rows {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		status := strings.ToLower(strings.TrimSpace(rows[index]["status"]))
		if status == "submitted" || status == "complete" || status == "completed" || status == "done" || status == "skipped" {
			summary.Skipped++
			updateReviewCSVRunTitle(len(rows), summary, false)
			continue
		}
		reviewID := strings.TrimSpace(rows[index]["review_id"])
		if reviewID == "" {
			rows[index]["status"] = "failed"
			rows[index]["last_error"] = "missing review_id"
			rows[index]["updated_at"] = e.now().Format(time.RFC3339)
			summary.Failed++
			_ = writeGenericCSVRows(path, headers, rows)
			updateReviewCSVRunTitle(len(rows), summary, false)
			continue
		}
		ratings := []int{
			csvRating(rows[index]["rating_1"]),
			csvRating(rows[index]["rating_2"]),
			csvRating(rows[index]["rating_3"]),
			csvRating(rows[index]["rating_4"]),
		}
		text := strings.TrimSpace(rows[index]["review_text"])
		if err := e.SubmitPendingReview(ctx, reviewID, ratings, text); err != nil {
			label := reviewCSVRowLabel(index, rows[index])
			if isReviewNoLongerPendingError(err) {
				rows[index]["status"] = "skipped"
				rows[index]["last_error"] = err.Error()
				rows[index]["updated_at"] = e.now().Format(time.RFC3339)
				summary.Skipped++
				logger.Manager(fmt.Sprintf("Review CSV %s skipped: %s", label, err.Error()))
				_ = writeGenericCSVRows(path, headers, rows)
				updateReviewCSVRunTitle(len(rows), summary, false)
				continue
			}
			rows[index]["status"] = "failed"
			rows[index]["last_error"] = err.Error()
			rows[index]["updated_at"] = e.now().Format(time.RFC3339)
			summary.Failed++
			logger.ManagerError(fmt.Sprintf("Review CSV %s failed: %s", label, err.Error()))
			_ = writeGenericCSVRows(path, headers, rows)
			updateReviewCSVRunTitle(len(rows), summary, false)
			continue
		}
		now := e.now().Format(time.RFC3339)
		rows[index]["status"] = "submitted"
		rows[index]["last_error"] = ""
		rows[index]["submitted_at"] = now
		rows[index]["updated_at"] = now
		summary.Submitted++
		if err := writeGenericCSVRows(path, headers, rows); err != nil {
			return summary, err
		}
		updateReviewCSVRunTitle(len(rows), summary, false)
		if index < len(rows)-1 {
			if err := e.sleepDelay(ctx, "bulk_review_between_reviews"); err != nil {
				return summary, err
			}
		}
	}
	updateReviewCSVRunTitle(len(rows), summary, true)
	logger.Manager(fmt.Sprintf("Review CSV run complete. Submitted %d, failed %d, skipped %d.", summary.Submitted, summary.Failed, summary.Skipped))
	return summary, nil
}

func reviewCSVRowLabel(index int, row map[string]string) string {
	return fmt.Sprintf("row %d (%s | order %s)",
		index+1,
		firstNonEmptyString(row["account_email"], row["account_username"], row["account_id"], "unknown account"),
		firstNonEmptyString(row["display_id"], row["external_order_id"], row["order_id"], row["review_id"], "unknown order"),
	)
}

func (e *Engine) ExportTrackingCodes(ctx context.Context, outputPath string) (TrackingExportSummary, error) {
	outputPath = preparedTextPath(firstNonEmptyString(strings.TrimSpace(outputPath), "storage/tracking_codes.txt"))
	summary := TrackingExportSummary{Path: outputPath}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return summary, err
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return summary, err
	}
	setConsoleTitle(fmt.Sprintf("Tracking export: scanning %d orders", len(orders)))
	accountByID := make(map[string]Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	lines := []string{}
	seen := map[string]bool{}
	for _, order := range orders {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if !trackingExportOrderEligible(order) {
			summary.Skipped++
			continue
		}
		tracking := firstNonEmptyString(order.TrackingNumber, metadataValue(order.Metadata, "tracking_number"))
		if tracking == "" {
			summary.Skipped++
			continue
		}
		account := accountByID[order.AccountID]
		line := strings.Join([]string{
			tracking,
			firstNonEmptyString(account.Email, account.Username, order.AccountID),
			firstNonEmptyString(metadataValue(order.Metadata, "display_id"), order.ExternalOrderID, order.ID),
			firstNonEmptyString(metadataValue(order.Metadata, "pretty_status"), metadataValue(order.Metadata, "overview_status"), string(order.State)),
		}, " | ")
		key := strings.ToLower(strings.TrimSpace(tracking))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		lines = append(lines, line)
	}
	if err := writeTextLines(outputPath, lines); err != nil {
		return summary, err
	}
	summary.Exported = len(lines)
	setConsoleTitle(fmt.Sprintf("Tracking export complete: %d saved | %d skipped", summary.Exported, summary.Skipped))
	logger.Manager(fmt.Sprintf("Tracking code export complete. Exported %d code(s) to %s.", summary.Exported, outputPath))
	return summary, nil
}

func (e *Engine) ExportOngoingOrders(ctx context.Context, outputPath string, scopeCSVPath string, concurrency int) (OngoingOrdersExportSummary, error) {
	outputPath = preparedCSVPath(firstNonEmptyString(strings.TrimSpace(outputPath), "storage/ongoing_orders.csv"))
	summary := OngoingOrdersExportSummary{Path: outputPath}
	scopeCSVPath = strings.TrimSpace(scopeCSVPath)
	refreshStartedAt := e.now().Add(-5 * time.Second)
	scopeLabel := "all sessions"
	if scopeCSVPath != "" {
		scopeLabel = scopeCSVPath
	}
	setConsoleTitle("Ongoing orders export: updating " + scopeLabel)
	logger.Manager("Ongoing orders export updating " + scopeLabel + " before exporting truth-only order state.")
	if _, err := e.UpdateAccounts(ctx, scopeCSVPath, concurrency); err != nil {
		return summary, err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return summary, err
	}
	accounts, scopedAccounts, sessions, _, err := e.accountUpdateTargets(scopeCSVPath)
	if err != nil {
		return summary, err
	}
	setConsoleTitle(fmt.Sprintf("Ongoing orders export: scanning %d fresh order(s)", len(orders)))

	accountByID := make(map[string]Account, len(accounts))
	sessionByAccountID := make(map[string]SessionRecord, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
		if session, ok := findSessionForAccount(sessions, account); ok {
			sessionByAccountID[account.ID] = session
		}
	}
	scopedAccountIDs := make(map[string]bool, len(scopedAccounts))
	for _, account := range scopedAccounts {
		if strings.TrimSpace(account.ID) != "" {
			scopedAccountIDs[account.ID] = true
		}
	}

	statusCounts := map[string]int{}
	rows := [][]string{}
	for _, order := range orders {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		if !scopedAccountIDs[order.AccountID] {
			continue
		}
		if order.UpdatedAt.IsZero() || order.UpdatedAt.Before(refreshStartedAt) {
			summary.Skipped++
			continue
		}
		account := accountByID[order.AccountID]
		session := sessionByAccountID[order.AccountID]
		status := ongoingOrderStatusLabel(order)
		statusCounts[status]++
		if !ongoingOrderExportEligible(order) {
			summary.Skipped++
			continue
		}
		rows = append(rows, ongoingOrderCSVRow(account, session, order, status))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i][9] != rows[j][9] {
			return strings.ToLower(rows[i][9]) < strings.ToLower(rows[j][9])
		}
		if rows[i][8] != rows[j][8] {
			return strings.ToLower(rows[i][8]) < strings.ToLower(rows[j][8])
		}
		return rows[i][14] > rows[j][14]
	})
	if err := writeCSVRows(outputPath, ongoingOrderExportHeader(), rows); err != nil {
		return summary, err
	}
	summary.Exported = len(rows)
	summary.Statuses = ongoingOrderStatusCounts(statusCounts)
	if err := e.webhook.SendOngoingOrdersSummary(ctx, summary); err != nil {
		e.sendErrorWebhook(ctx, "Ongoing orders summary webhook failed", err, map[string]string{
			"Mode": "Export Ongoing Orders",
			"CSV":  outputPath,
		})
		logger.ManagerError("Ongoing orders summary webhook failed; continuing: " + err.Error())
	} else {
		logger.Manager("Ongoing orders summary webhook sent.")
	}
	setConsoleTitle(fmt.Sprintf("Ongoing orders export complete: %d ongoing | %d done/skipped", summary.Exported, summary.Skipped))
	logger.Manager(fmt.Sprintf("Ongoing orders export complete. Exported %d order(s) to %s.", summary.Exported, outputPath))
	return summary, nil
}

func ongoingOrderExportHeader() []string {
	return []string{
		"account_email",
		"account_username",
		"account_credits",
		"referrer",
		"account_id",
		"order_id",
		"external_order_id",
		"display_id",
		"seller_username",
		"status",
		"item",
		"tracking_number",
		"tracking_url",
		"shipping_service",
		"order_date",
		"total_paid",
		"last_checked_at",
		"updated_at",
	}
}

func ongoingOrderCSVRow(account Account, session SessionRecord, order Order, status string) []string {
	lastCheckedAt := ""
	if order.LastCheckedAt != nil && !order.LastCheckedAt.IsZero() {
		lastCheckedAt = order.LastCheckedAt.Format(time.RFC3339)
	}
	updatedAt := ""
	if !order.UpdatedAt.IsZero() {
		updatedAt = order.UpdatedAt.Format(time.RFC3339)
	}
	return []string{
		account.Email,
		account.Username,
		sessionCreditLabel(session),
		account.MasterUsernameUsed,
		firstNonEmptyString(account.ID, order.AccountID),
		order.ID,
		order.ExternalOrderID,
		firstNonEmptyString(metadataValue(order.Metadata, "display_id"), order.ExternalOrderID, order.ID),
		metadataValue(order.Metadata, "seller_username"),
		status,
		effectiveOrderItem(order),
		firstNonEmptyString(order.TrackingNumber, metadataValue(order.Metadata, "tracking_number")),
		metadataValue(order.Metadata, "tracking_url"),
		metadataValue(order.Metadata, "shipping_service"),
		metadataValue(order.Metadata, "order_date"),
		formatMoneyLine(metadataValue(order.Metadata, "total")),
		lastCheckedAt,
		updatedAt,
	}
}

func ongoingOrderExportEligible(order Order) bool {
	if strings.TrimSpace(firstNonEmptyString(order.ID, order.ExternalOrderID, metadataValue(order.Metadata, "display_id"))) == "" {
		return false
	}
	if order.ReviewCompleted || order.State == OrderStateReviewed || order.State == OrderStateReviewReady || order.State == OrderStateFailed {
		return false
	}
	status := strings.ToLower(strings.Join([]string{
		string(order.State),
		orderStatusLabel(order),
		metadataValue(order.Metadata, "pretty_status"),
		metadataValue(order.Metadata, "overview_status"),
		metadataValue(order.Metadata, "status"),
		metadataValue(order.Metadata, "shipment_status"),
	}, " "))
	doneSignals := []string{
		"pending review",
		"review ready",
		"rate your experience",
		"reviewed",
		"delivered",
		"completed",
		"complete",
		"cancelled",
		"canceled",
		"refunded",
		"returned",
	}
	for _, signal := range doneSignals {
		if strings.Contains(status, signal) {
			return false
		}
	}
	return true
}

func ongoingOrderStatusLabel(order Order) string {
	return firstNonEmptyString(
		metadataValue(order.Metadata, "pretty_status"),
		metadataValue(order.Metadata, "overview_status"),
		metadataValue(order.Metadata, "status"),
		string(order.State),
		"Unknown",
	)
}

func ongoingOrderStatusCounts(counts map[string]int) []OngoingOrdersStatusCount {
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if counts[statuses[i]] != counts[statuses[j]] {
			return counts[statuses[i]] > counts[statuses[j]]
		}
		return strings.ToLower(statuses[i]) < strings.ToLower(statuses[j])
	})
	out := make([]OngoingOrdersStatusCount, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, OngoingOrdersStatusCount{Status: status, Count: counts[status]})
	}
	return out
}

func updateReviewCSVBuildTitle(total int, summary ReviewCSVSummary, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("Review CSV create complete: %d/%d checked | %d reviews | %d failed", summary.Checked, total, summary.Exported, summary.Failed))
		return
	}
	setConsoleTitle(fmt.Sprintf("Review CSV create: %d/%d checked | %d reviews | %d failed", summary.Checked, total, summary.Exported, summary.Failed))
}

func updateReviewCSVRunTitle(total int, summary ReviewCSVSummary, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("Review CSV run complete: %d/%d submitted | %d failed | %d skipped", summary.Submitted, total, summary.Failed, summary.Skipped))
		return
	}
	setConsoleTitle(fmt.Sprintf("Review CSV run: %d/%d submitted | %d failed | %d skipped", summary.Submitted, total, summary.Failed, summary.Skipped))
}

func (e *Engine) runReviewCSVDiscoveryWorkers(ctx context.Context, targets []accountUpdateWorkTarget, orders []Order, concurrency int) <-chan accountUpdateWorkResult {
	results := make(chan accountUpdateWorkResult, len(targets))
	if len(targets) == 0 {
		close(results)
		return results
	}
	jobs := make(chan accountUpdateWorkTarget)
	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			if err := e.staggerWorkerStart(ctx, workerIndex, concurrency); err != nil {
				return
			}
			for target := range jobs {
				results <- e.runReviewCSVDiscoveryTarget(ctx, target, orders)
			}
		}(workerIndex)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	go func() {
		defer close(jobs)
		for _, target := range targets {
			select {
			case jobs <- target:
			case <-ctx.Done():
				return
			}
		}
	}()
	return results
}

func (e *Engine) runReviewCSVDiscoveryTarget(ctx context.Context, target accountUpdateWorkTarget, orders []Order) accountUpdateWorkResult {
	out := accountUpdateWorkResult{Target: target, Session: target.Session}
	refreshed, err := e.refreshAccountSessionForReviewDiscovery(ctx, target.Account, target.Session)
	if err != nil {
		out.Err = err
		out.Banned = isDefiniteSessionBan(err, true)
		return out
	}
	out.Session = refreshed
	sectionCtx, cancel := context.WithTimeout(ctx, reviewCSVOrderCheckTimeout)
	defer cancel()
	input := AccountOrdersInput{Account: target.Account, Session: refreshed}
	out.OrderResults, out.OrderErr = e.flows.ListReviewOrders(sectionCtx, input)
	if out.OrderErr != nil {
		var fallbackErr error
		out.OrderResults, fallbackErr = e.checkStoredAccountOrders(sectionCtx, target.Account, refreshed, orders)
		if fallbackErr != nil {
			out.Err = joinErr(out.OrderErr, fallbackErr)
			return out
		}
	}
	return out
}

func (e *Engine) refreshAccountSessionForReviewDiscovery(ctx context.Context, account Account, session SessionRecord) (SessionRecord, error) {
	if accountUtilityAuthSessionIsFresh(session, e.now()) {
		return session, nil
	}
	refreshed, healthy, err := e.flows.RefreshSessionWithoutCredits(ctx, session)
	if err != nil {
		return SessionRecord{}, err
	}
	if !healthy {
		return SessionRecord{}, fmt.Errorf("session for %s is unhealthy", firstNonEmptyString(account.Email, account.Username, account.ID))
	}
	if err := validateRefreshedSession(session, refreshed); err != nil {
		return SessionRecord{}, err
	}
	return refreshed, nil
}

func accountUtilityAuthSessionIsFresh(session SessionRecord, now time.Time) bool {
	return !session.UpdatedAt.IsZero() && now.Sub(session.UpdatedAt) <= accountUtilityFreshSessionTTL
}

func (e *Engine) reviewCSVRowsForScope(scope []Account) ([]map[string]string, error) {
	reviews, err := e.ListPendingReviews()
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, account := range scope {
		if account.ID != "" {
			allowed[account.ID] = true
		}
	}
	rows := []map[string]string{}
	seen := map[string]bool{}
	for _, review := range reviews {
		if len(allowed) > 0 && !allowed[review.AccountID] {
			continue
		}
		if seen[review.ID] {
			continue
		}
		seen[review.ID] = true
		rows = append(rows, reviewCSVRow(review))
	}
	return rows, nil
}

func reviewCSVRow(review PendingReviewItem) map[string]string {
	updatedAt := ""
	if !review.UpdatedAt.IsZero() {
		updatedAt = review.UpdatedAt.Format(time.RFC3339)
	}
	detectedAt := ""
	if !review.DetectedAt.IsZero() {
		detectedAt = review.DetectedAt.Format(time.RFC3339)
	}
	return map[string]string{
		"review_id":           review.ID,
		"account_id":          review.AccountID,
		"account_email":       review.AccountEmail,
		"account_username":    review.AccountUsername,
		"account_credits":     review.AccountCredits,
		"order_id":            review.OrderID,
		"external_order_id":   review.ExternalOrderID,
		"display_id":          review.DisplayID,
		"review_reference_id": review.ReviewReferenceID,
		"items":               strings.Join(review.Items, " | "),
		"seller_username":     metadataValue(review.Metadata, "seller_username"),
		"order_status":        firstNonEmptyString(metadataValue(review.Metadata, "pretty_status"), metadataValue(review.Metadata, "overview_status"), "Review pending"),
		"detected_at":         detectedAt,
		"rating_1":            "5",
		"rating_2":            "5",
		"rating_3":            "5",
		"rating_4":            "5",
		"review_text":         "",
		"status":              "pending",
		"last_error":          review.LastError,
		"updated_at":          updatedAt,
	}
}

func trackingExportOrderEligible(order Order) bool {
	if order.ReviewCompleted || order.State == OrderStateReviewed || order.State == OrderStateReviewReady {
		return false
	}
	status := strings.ToLower(strings.Join([]string{
		string(order.State),
		metadataValue(order.Metadata, "pretty_status"),
		metadataValue(order.Metadata, "overview_status"),
		metadataValue(order.Metadata, "status"),
	}, " "))
	if strings.Contains(status, "pending review") ||
		strings.Contains(status, "review ready") ||
		strings.Contains(status, "completed") ||
		strings.Contains(status, "delivered") ||
		strings.Contains(status, "reviewed") {
		return false
	}
	return true
}

func csvRating(value string) int {
	rating, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || rating < 1 {
		return 5
	}
	if rating > 5 {
		return 5
	}
	return rating
}

func readGenericCSVRows(path string, defaultHeaders []string) ([]string, []map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return append([]string(nil), defaultHeaders...), nil, nil
		}
		return nil, nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return append([]string(nil), defaultHeaders...), nil, nil
		}
		return nil, nil, err
	}
	header = normalizeGenericHeaders(header, defaultHeaders)
	rows := []map[string]string{}
	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, err
		}
		row := make(map[string]string, len(header))
		for index, key := range header {
			if index < len(record) {
				row[key] = strings.TrimSpace(record[index])
			}
		}
		rows = append(rows, row)
	}
	return header, rows, nil
}

func writeGenericCSVRows(path string, headers []string, rows []map[string]string) error {
	headers = normalizeGenericHeaders(headers, headers)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	writer := csv.NewWriter(temp)
	if err := writer.Write(headers); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return err
	}
	for _, row := range rows {
		record := make([]string, len(headers))
		for index, key := range headers {
			record[index] = row[key]
		}
		if err := writer.Write(record); err != nil {
			_ = temp.Close()
			_ = os.Remove(tempName)
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempName)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return replacePreparedFile(tempName, path)
}

func normalizeGenericHeaders(headers []string, defaults []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(headers)+len(defaults))
	for index, header := range headers {
		key := strings.ToLower(strings.TrimSpace(header))
		if index == 0 {
			key = strings.TrimPrefix(key, "\ufeff")
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, key := range defaults {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out
}

func preparedTextPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "storage/tracking_codes.txt"
	}
	if !strings.ContainsAny(path, `/\`) {
		path = filepath.Join("storage", path)
	}
	if strings.TrimSpace(filepath.Ext(path)) == "" {
		path += ".txt"
	}
	return filepath.Clean(path)
}

func writeTextLines(path string, lines []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	for _, line := range lines {
		if _, err := temp.WriteString(line + "\n"); err != nil {
			_ = temp.Close()
			_ = os.Remove(tempName)
			return err
		}
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return replacePreparedFile(tempName, path)
}
