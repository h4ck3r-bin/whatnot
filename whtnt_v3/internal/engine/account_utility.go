package engine

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

const accountUtilityFreshSessionTTL = 5 * time.Minute
const allReadyAccountsCSVPath = "storage/all_ready_accounts.csv"
const readyCheckoutMinimumAmountSafe = 1000
const accountUpdateSectionTimeout = 2 * time.Minute
const reviewCSVOrderCheckTimeout = 35 * time.Second
const readyCheckoutRefreshAttempts = 3
const readyCheckoutRefreshRetryDelay = time.Minute
const updateAccountsFlushBatchSize = 25

type accountDiscoverySnapshot struct {
	Orders        map[string]string `json:"orders,omitempty"`
	Messages      map[string]string `json:"messages,omitempty"`
	Notifications map[string]string `json:"notifications,omitempty"`
}

type accountUpdateWorkTarget struct {
	Number  int
	Account Account
	Session SessionRecord
}

type accountUpdateWorkResult struct {
	Target        accountUpdateWorkTarget
	Session       SessionRecord
	Healthy       bool
	Banned        bool
	Err           error
	OrderResults  []OrderCheckResult
	OrderErr      error
	Messages      []AccountMessageSummary
	MessagesErr   error
	Notifications []AccountNotification
	NotifyErr     error
}

type accountUpdateLiveProgress struct {
	total     int
	checked   atomic.Int64
	processed atomic.Int64
	healthy   atomic.Int64
	banned    atomic.Int64
	failed    atomic.Int64
}

func (p *accountUpdateLiveProgress) markChecked() int {
	if p == nil {
		return 0
	}
	done := int(p.checked.Add(1))
	p.renderTitle()
	return done
}

func (p *accountUpdateLiveProgress) count() int {
	if p == nil {
		return 0
	}
	return int(p.checked.Load())
}

func (p *accountUpdateLiveProgress) updateProcessed(summary AccountUpdateSummary, processed int) {
	if p == nil {
		return
	}
	if processed < 0 {
		processed = 0
	}
	p.processed.Store(int64(processed))
	p.healthy.Store(int64(summary.Healthy))
	p.banned.Store(int64(summary.Banned))
	p.failed.Store(int64(summary.Failed))
	p.renderTitle()
}

func (p *accountUpdateLiveProgress) renderTitle() {
	if p == nil {
		return
	}
	liveChecked := p.count()
	if liveChecked > p.total {
		liveChecked = p.total
	}
	setConsoleTitle(fmt.Sprintf("Update Accounts: %d/%d live checked | %d processed | %d healthy | %d banned | %d failed", liveChecked, p.total, p.processed.Load(), p.healthy.Load(), p.banned.Load(), p.failed.Load()))
}

type creditCheckoutWorkResult struct {
	Target  accountUpdateWorkTarget
	Account Account
	Session SessionRecord
	Amount  int
	Row     map[string]string
	Banned  bool
	Skipped bool
	Err     error
}

type friendCodeCreditTarget struct {
	Number  int
	Code    string
	Status  string
	Account Account
	Master  MasterAccount
	Session SessionRecord
	Normal  bool
}

type friendCodeCreditResult struct {
	Target    friendCodeCreditTarget
	Session   SessionRecord
	Amount    int
	OldAmount int
	Updated   bool
	Banned    bool
	Err       error
}

func (e *Engine) UpdateAccounts(ctx context.Context, csvPath string, concurrency int) (AccountUpdateSummary, error) {
	var summary AccountUpdateSummary
	accounts, targets, sessions, rows, err := e.accountUpdateTargets(csvPath)
	if err != nil {
		return summary, err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return summary, err
	}
	snapshots, err := e.loadAccountDiscoverySnapshots()
	if err != nil {
		return summary, err
	}

	workTargets := make([]accountUpdateWorkTarget, 0, len(targets))
	bannedSkipped := 0
	for index, account := range targets {
		if storedAccountIsKnownBanned(account) {
			summary.Banned++
			summary.Skipped++
			bannedSkipped++
			continue
		}
		session, ok := findSessionForAccount(sessions, account)
		if !ok {
			summary.Missing++
			logger.ManagerError("Account update skipped " + firstNonEmptyString(account.Email, account.Username, account.ID) + ": stored session missing.")
			continue
		}
		workTargets = append(workTargets, accountUpdateWorkTarget{Number: index + 1, Account: account, Session: session})
	}
	if len(workTargets) == 0 {
		return summary, e.saveAccountDiscoverySnapshots(snapshots)
	}
	concurrency = boundedAccountUtilityConcurrency(concurrency, len(workTargets), e.cfg.Engine.TaskConcurrency)
	logger.Manager(fmt.Sprintf("Update Accounts checking %d account(s) (%d banned), starting in number order with %d thread(s).", len(workTargets), bannedSkipped, concurrency))
	progress := &accountUpdateLiveProgress{total: len(workTargets)}
	progress.updateProcessed(summary, summary.Checked)
	workCtx, cancelWorkers := context.WithCancel(ctx)
	defer cancelWorkers()
	results := e.runAccountUpdateWorkers(workCtx, workTargets, orders, concurrency, progress)
	dirtyAccounts := false
	dirtySessions := false
	dirtyOrders := false
	dirtySnapshots := false
	sinceFlush := 0
	flushState := func() error {
		if dirtySessions {
			if err := e.store.SaveNormalSessions(sessions); err != nil {
				return err
			}
			dirtySessions = false
		}
		if dirtyAccounts {
			if err := e.store.SaveAccounts(accounts); err != nil {
				return err
			}
			dirtyAccounts = false
		}
		if dirtyOrders {
			if err := e.store.SaveOrders(orders); err != nil {
				return err
			}
			dirtyOrders = false
		}
		if dirtySnapshots {
			if err := e.saveAccountDiscoverySnapshots(snapshots); err != nil {
				return err
			}
			dirtySnapshots = false
		}
		sinceFlush = 0
		return nil
	}
	returnAfterWorkerDrain := func(err error) (AccountUpdateSummary, error) {
		cancelWorkers()
		for range results {
		}
		return summary, err
	}
	for result := range results {
		if err := ctx.Err(); err != nil {
			return returnAfterWorkerDrain(err)
		}
		account := result.Target.Account
		accountIndex := findAccountIndex(accounts, account.ID)
		summary.Checked++
		if result.Err != nil {
			logger.ManagerError(fmt.Sprintf("Account update #%d failed for %s: %s", result.Target.Number, firstNonEmptyString(account.Email, account.Username, account.ID), result.Err.Error()))
			if result.Banned && accountIndex >= 0 {
				e.markAccountSessionBanned(&accounts[accountIndex], result.Err)
				summary.Banned++
			} else if accountIndex >= 0 {
				accounts[accountIndex].LastError = result.Err.Error()
				accounts[accountIndex].UpdatedAt = e.now()
				summary.Failed++
			}
			dirtyAccounts = true
			sinceFlush++
			if sinceFlush >= updateAccountsFlushBatchSize {
				if err := flushState(); err != nil {
					return returnAfterWorkerDrain(err)
				}
			}
			progress.updateProcessed(summary, summary.Checked)
			continue
		}
		summary.Healthy++
		session := result.Session
		if session.ID != "" {
			sessions = upsertSession(sessions, session)
			if accountIndex >= 0 {
				if accounts[accountIndex].State == AccountStateFailed {
					accounts[accountIndex].State = AccountStateSignedUp
				}
				accounts[accountIndex].SessionID = session.ID
				accounts[accountIndex].LastError = ""
				accounts[accountIndex].UpdatedAt = e.now()
			}
			dirtySessions = true
			dirtyAccounts = true
		}
		if result.OrderErr != nil {
			logger.ManagerError(fmt.Sprintf("Order update failed for %s: %s", account.Email, result.OrderErr.Error()))
		} else {
			var reviewCount int
			orders, reviewCount, err = e.applyAccountOrderResultsInMemory(ctx, account, session, orders, result.OrderResults)
			if err != nil {
				return returnAfterWorkerDrain(err)
			}
			if len(result.OrderResults) > 0 {
				dirtyOrders = true
			}
			summary.PendingReviews += reviewCount
			summary.Orders += len(result.OrderResults)
		}
		if result.MessagesErr != nil {
			logger.ManagerError(fmt.Sprintf("Message update failed for %s: %s", account.Email, result.MessagesErr.Error()))
		} else {
			annotateMessageCredits(result.Messages, sessionCreditLabel(session))
			discoveries, webhooks := e.processMessageDiscoveries(ctx, snapshots, account, result.Messages)
			summary.MessageDiscoveries += discoveries
			summary.Webhooks += webhooks
			if len(result.Messages) > 0 {
				dirtySnapshots = true
			}
		}
		if result.NotifyErr != nil {
			logger.ManagerError(fmt.Sprintf("Notification update failed for %s: %s", account.Email, result.NotifyErr.Error()))
		} else {
			annotateNotificationCredits(result.Notifications, sessionCreditLabel(session))
			discoveries, webhooks := e.processNotificationDiscoveries(ctx, snapshots, account, result.Notifications)
			summary.NotificationDiscoveries += discoveries
			summary.Webhooks += webhooks
			if len(result.Notifications) > 0 {
				dirtySnapshots = true
			}
		}
		sinceFlush++
		if sinceFlush >= updateAccountsFlushBatchSize {
			if err := flushState(); err != nil {
				return returnAfterWorkerDrain(err)
			}
		}
		progress.updateProcessed(summary, summary.Checked)
	}
	if rows != nil {
		_ = rows
	}
	setConsoleTitle(fmt.Sprintf("Update Accounts complete: %d/%d checked | %d healthy | %d banned | %d failed", summary.Checked, len(workTargets), summary.Healthy, summary.Banned, summary.Failed))
	if err := flushState(); err != nil {
		return summary, err
	}
	if err := e.webhook.SendUpdateAccountsSummary(ctx, updateAccountsStatusCounts(accountsForUpdateSummaryTargets(accounts, targets), orders)); err != nil {
		e.sendErrorWebhook(ctx, "Update Accounts summary webhook failed", err, map[string]string{
			"Mode": "Update Accounts",
		})
	} else {
		logger.Manager("Update Accounts summary webhook sent.")
	}
	return summary, nil
}

func updateAccountUpdateTitle(total int, summary AccountUpdateSummary, remaining int) {
	if remaining < 0 {
		remaining = 0
	}
	setConsoleTitle(fmt.Sprintf("Update Accounts: %d/%d checked | %d remaining | %d healthy | %d banned | %d failed", summary.Checked, total, remaining, summary.Healthy, summary.Banned, summary.Failed))
}

func updateAccountsStatusCounts(accounts []Account, orders []Order) map[string]int {
	counts := map[string]int{}
	for _, account := range accounts {
		if updateAccountIsBanned(account) {
			counts["Banned"]++
			continue
		}
		accountOrders := ordersForAccount(orders, account.ID)
		if len(accountOrders) == 0 {
			counts["Not ordered yet"]++
			continue
		}
		counts[updateAccountOrderBucket(accountOrders)]++
	}
	return counts
}

func accountsForUpdateSummaryTargets(accounts []Account, targets []Account) []Account {
	out := make([]Account, 0, len(targets))
	for _, target := range targets {
		if index := findAccountIndex(accounts, target.ID); index >= 0 {
			out = append(out, accounts[index])
		} else {
			out = append(out, target)
		}
	}
	return out
}

func updateAccountIsBanned(account Account) bool {
	return storedAccountIsKnownBanned(account)
}

func storedAccountIsKnownBanned(account Account) bool {
	errText := strings.ToLower(strings.TrimSpace(account.LastError))
	if errText == "" {
		return false
	}
	if account.State != AccountStateFailed && !strings.Contains(errText, "banned") {
		return false
	}
	for _, signal := range []string{
		"banned",
		"session banned",
		"account disabled",
		"account was banned",
		"invalid session",
		"unauthorized",
		"401",
		"403",
		"forbidden",
	} {
		if strings.Contains(errText, signal) {
			return true
		}
	}
	return false
}

func countKnownBannedAccounts(accounts []Account) int {
	count := 0
	for _, account := range accounts {
		if storedAccountIsKnownBanned(account) {
			count++
		}
	}
	return count
}

func countKnownBannedPreparedRows(accounts []Account, rows []map[string]string) int {
	count := 0
	for _, row := range rows {
		accountIndex := findPreparedAccountIndex(accounts, row)
		if accountIndex >= 0 && storedAccountIsKnownBanned(accounts[accountIndex]) {
			count++
		}
	}
	return count
}

func updateAccountOrderBucket(orders []Order) string {
	bestRank := -1
	bestBucket := ""
	for _, order := range orders {
		bucket := updateAccountOrderStatusLabel(order)
		rank := rankUpdateAccountOrderStatus(order)
		if rank > bestRank {
			bestRank = rank
			bestBucket = bucket
		}
	}
	if strings.TrimSpace(bestBucket) == "" {
		return "Unknown"
	}
	return bestBucket
}

func updateAccountOrderStatusLabel(order Order) string {
	return firstNonEmptyString(
		metadataValue(order.Metadata, "pretty_status"),
		metadataValue(order.Metadata, "overview_status"),
		metadataValue(order.Metadata, "status"),
		string(order.State),
	)
}

func rankUpdateAccountOrderStatus(order Order) int {
	status := strings.ToLower(strings.Join([]string{
		orderStatusLabel(order),
		order.TrackingNumber,
		metadataValue(order.Metadata, "tracking_number"),
		metadataValue(order.Metadata, "tracking_url"),
		metadataValue(order.Metadata, "shipping_service"),
	}, " "))
	switch {
	case strings.Contains(status, "delivered"):
		return 4
	case strings.Contains(status, "in transit") || strings.Contains(status, "transit") || strings.Contains(status, "out for delivery"):
		return 3
	case strings.Contains(status, "shipped") || strings.Contains(status, "dispatched") || strings.TrimSpace(order.TrackingNumber) != "" || metadataValue(order.Metadata, "tracking_number") != "":
		return 2
	default:
		return 1
	}
}

func boundedAccountUtilityConcurrency(requested int, total int, fallback int) int {
	if requested <= 0 {
		requested = fallback
	}
	if requested <= 0 {
		requested = 1
	}
	if requested > total {
		requested = total
	}
	return requested
}

func (e *Engine) runAccountUpdateWorkers(ctx context.Context, targets []accountUpdateWorkTarget, orders []Order, concurrency int, progress *accountUpdateLiveProgress) <-chan accountUpdateWorkResult {
	jobs := make(chan accountUpdateWorkTarget)
	results := make(chan accountUpdateWorkResult, len(targets))
	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			if err := e.staggerWorkerStart(ctx, workerIndex, concurrency); err != nil {
				return
			}
			for target := range jobs {
				results <- e.runAccountUpdateTarget(ctx, target, orders, progress)
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

func (e *Engine) runAccountUpdateTarget(ctx context.Context, target accountUpdateWorkTarget, orders []Order, progress *accountUpdateLiveProgress) accountUpdateWorkResult {
	defer progress.markChecked()
	out := accountUpdateWorkResult{Target: target, Session: target.Session}
	if err := ctx.Err(); err != nil {
		out.Err = err
		return out
	}
	refreshed, err := e.refreshAccountSessionIfNeeded(ctx, target.Account, target.Session, false)
	if err != nil {
		out.Err = err
		out.Banned = isDefiniteSessionBan(err, true)
		return out
	}
	out.Session = refreshed
	out.Healthy = true
	input := AccountOrdersInput{Account: target.Account, Session: refreshed}
	label := firstNonEmptyString(target.Account.Email, target.Account.Username, target.Account.ID)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
		defer cancel()
		logger.Manager("Update Accounts checking orders for " + label + ".")
		out.OrderResults, out.OrderErr = e.flows.ListOrders(sectionCtx, input)
		if out.OrderErr != nil {
			var fallbackErr error
			out.OrderResults, fallbackErr = e.checkStoredAccountOrders(sectionCtx, target.Account, refreshed, orders)
			if fallbackErr != nil {
				out.OrderErr = joinErr(out.OrderErr, fallbackErr)
			} else if len(out.OrderResults) > 0 {
				out.OrderErr = nil
			}
		}
		if out.OrderErr != nil {
			logger.ManagerError("Update Accounts order check failed for " + label + ": " + out.OrderErr.Error())
		} else {
			logger.Manager(fmt.Sprintf("Update Accounts orders checked for %s: %d found.", label, len(out.OrderResults)))
		}
	}()
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
		defer cancel()
		logger.Manager("Update Accounts checking messages for " + label + ".")
		out.Messages, out.MessagesErr = e.flows.ListConversations(sectionCtx, input)
		if out.MessagesErr != nil {
			logger.ManagerError("Update Accounts message check failed for " + label + ": " + out.MessagesErr.Error())
		} else {
			logger.Manager(fmt.Sprintf("Update Accounts messages checked for %s: %d found.", label, len(out.Messages)))
		}
	}()
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
		defer cancel()
		logger.Manager("Update Accounts checking notifications for " + label + ".")
		out.Notifications, out.NotifyErr = e.flows.ListNotifications(sectionCtx, input)
		if out.NotifyErr != nil {
			logger.ManagerError("Update Accounts notification check failed for " + label + ": " + out.NotifyErr.Error())
		} else {
			logger.Manager(fmt.Sprintf("Update Accounts notifications checked for %s: %d found.", label, len(out.Notifications)))
		}
	}()
	wg.Wait()
	return out
}

func (e *Engine) applyAccountOrderResults(ctx context.Context, account Account, session SessionRecord, orders []Order, results []OrderCheckResult) ([]Order, int, error) {
	return e.applyAccountOrderResultsWithSave(ctx, account, session, orders, results, true)
}

func (e *Engine) applyAccountOrderResultsInMemory(ctx context.Context, account Account, session SessionRecord, orders []Order, results []OrderCheckResult) ([]Order, int, error) {
	return e.applyAccountOrderResultsWithSaveMode(ctx, account, session, orders, results, false, false)
}

func (e *Engine) applyAccountOrderResultsWithSave(ctx context.Context, account Account, session SessionRecord, orders []Order, results []OrderCheckResult, saveEachOrder bool) ([]Order, int, error) {
	return e.applyAccountOrderResultsWithSaveMode(ctx, account, session, orders, results, saveEachOrder, true)
}

func (e *Engine) applyAccountOrderResultsWithSaveMode(ctx context.Context, account Account, session SessionRecord, orders []Order, results []OrderCheckResult, saveEachOrder bool, saveAtEnd bool) ([]Order, int, error) {
	now := e.now()
	reviewCount := 0
	for _, result := range results {
		order := orderFromCheckResult(account.ID, result, now)
		if order.ID == "" {
			continue
		}
		addSessionCreditMetadata(order.Metadata, session)
		previous := Order{}
		hadPrevious := false
		if existingIndex := findOrderIndex(orders, order.ID); existingIndex >= 0 {
			previous = orders[existingIndex]
			hadPrevious = true
		}
		orders = upsertOrder(orders, order)
		orderIndex := findOrderIndex(orders, order.ID)
		if orderIndex < 0 {
			continue
		}
		if shouldLogDiscoveredOrder(previous, orders[orderIndex], hadPrevious) {
			e.logDetectedOrder(account, orders[orderIndex])
		}
		if hadPrevious && orderStatusLabel(previous) != orderStatusLabel(orders[orderIndex]) {
			if err := e.webhook.SendOrderStatusChange(ctx, account, previous, orders[orderIndex]); err != nil {
				e.sendErrorWebhook(ctx, "Order status webhook failed", err, map[string]string{
					"Account": account.Email,
					"Order":   orderLabel(orders[orderIndex]),
				})
				logger.ManagerError(fmt.Sprintf("Order status webhook failed for %s; continuing: %s", orderLabel(orders[orderIndex]), err.Error()))
			}
		}
		if err := e.sendTrackingWebhookImmediately(ctx, account, &orders[orderIndex]); err != nil {
			logger.ManagerError(fmt.Sprintf("Tracking update failed for %s; continuing: %s", orderLabel(orders[orderIndex]), err.Error()))
		}
		if orders[orderIndex].State == OrderStateReviewReady {
			reviewCount++
			if !orders[orderIndex].ReviewCompleted {
				if err := e.queuePendingReview(ctx, account, &orders[orderIndex]); err != nil {
					logger.ManagerError(fmt.Sprintf("Pending review queue failed for %s; continuing: %s", orderLabel(orders[orderIndex]), err.Error()))
				}
			}
		}
		if saveEachOrder {
			if err := e.store.SaveOrders(orders); err != nil {
				return orders, reviewCount, err
			}
		}
	}
	if !saveEachOrder && saveAtEnd {
		if err := e.store.SaveOrders(orders); err != nil {
			return orders, reviewCount, err
		}
	}
	return orders, reviewCount, nil
}

func (e *Engine) ManageAccountDashboard(ctx context.Context, query string, forceRefresh bool) (AccountDashboard, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return AccountDashboard{}, fmt.Errorf("email or username is required")
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return AccountDashboard{}, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return AccountDashboard{}, err
	}
	accounts, index, session, err := e.ensureAccountFromQuery(accounts, sessions, query)
	if err != nil {
		return AccountDashboard{}, err
	}
	account := accounts[index]
	refreshed, err := e.refreshAccountManagerSessionIfNeeded(ctx, account, session, forceRefresh)
	if err != nil {
		return AccountDashboard{}, err
	}
	refreshed = markSessionRecentlyRefreshedForActions(refreshed, e.now())
	sessions = upsertSession(sessions, refreshed)
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return AccountDashboard{}, err
	}

	orders, err := e.store.LoadOrders()
	if err != nil {
		return AccountDashboard{}, err
	}
	session, _ = findSessionForAccount(sessions, account)
	target := AccountOrdersInput{Account: account, Session: session}
	feeds := e.fetchAccountDashboardData(ctx, target, true, orders)
	currentOrders := make([]Order, 0, len(feeds.OrderResults))
	for _, result := range feeds.OrderResults {
		order := orderFromCheckResult(account.ID, result, e.now())
		if order.ID != "" {
			currentOrders = append(currentOrders, order)
		}
	}
	dashboard := AccountDashboard{
		Account:       account,
		CreditBalance: sessionCreditLabel(refreshed),
		Orders:        dashboardOrdersForAccount(currentOrders, account.ID),
		RefreshedAt:   e.now(),
	}
	if feeds.OrderErr != nil {
		dashboard.Warnings = append(dashboard.Warnings, "Orders unavailable: "+feeds.OrderErr.Error())
	} else {
		var discoverErr error
		orders, _, discoverErr = e.applyAccountOrderResultsWithSave(ctx, account, refreshed, orders, feeds.OrderResults, false)
		if discoverErr != nil {
			dashboard.Warnings = append(dashboard.Warnings, "Order update warnings: "+discoverErr.Error())
		}
	}
	if feeds.MessagesErr != nil {
		dashboard.Warnings = append(dashboard.Warnings, "Messages unavailable: "+feeds.MessagesErr.Error())
	} else {
		annotateMessageCredits(feeds.Messages, dashboard.CreditBalance)
		dashboard.Messages = feeds.Messages
	}
	if feeds.NotificationsErr != nil {
		dashboard.Warnings = append(dashboard.Warnings, "Notifications unavailable: "+feeds.NotificationsErr.Error())
	} else {
		annotateNotificationCredits(feeds.Notifications, dashboard.CreditBalance)
		dashboard.Notifications = feeds.Notifications
	}
	if feeds.AddressesErr != nil {
		dashboard.Warnings = append(dashboard.Warnings, "Addresses unavailable: "+feeds.AddressesErr.Error())
	} else {
		dashboard.Addresses = feeds.Addresses
	}
	if feeds.PaymentMethodsErr != nil {
		dashboard.Warnings = append(dashboard.Warnings, "Payment methods unavailable: "+feeds.PaymentMethodsErr.Error())
	} else {
		dashboard.PaymentMethods = feeds.PaymentMethods
	}
	setConsoleTitle(fmt.Sprintf("Manage Account: %s | %s | %d orders | %d messages | %d notifications",
		firstNonEmptyString(dashboard.Account.Username, dashboard.Account.Email, dashboard.Account.ID),
		firstNonEmptyString(dashboard.CreditBalance, "credits unknown"),
		len(dashboard.Orders),
		len(dashboard.Messages),
		len(dashboard.Notifications),
	))
	return dashboard, nil
}

func (e *Engine) BuildCreditCheckoutCSV(ctx context.Context, concurrency int) (CreditCheckoutCSVSummary, error) {
	return e.BuildBulkCheckoutCSV(ctx, allReadyAccountsCSVPath, "", concurrency)
}

func (e *Engine) BuildBulkCheckoutCSV(ctx context.Context, outputPath string, scopeCSVPath string, concurrency int) (CreditCheckoutCSVSummary, error) {
	outputPath = preparedCSVPath(firstNonEmptyString(strings.TrimSpace(outputPath), allReadyAccountsCSVPath))
	summary := CreditCheckoutCSVSummary{Path: outputPath}
	accounts, scopedAccounts, sessions, _, err := e.accountUpdateTargets(scopeCSVPath)
	if err != nil {
		return summary, err
	}

	targets := make([]accountUpdateWorkTarget, 0, len(scopedAccounts))
	bannedSkipped := 0
	for index := range scopedAccounts {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		account := scopedAccounts[index]
		if storedAccountIsKnownBanned(account) {
			summary.Banned++
			summary.Skipped++
			bannedSkipped++
			continue
		}
		session, ok := findSessionForAccount(sessions, account)
		if !ok {
			summary.Failed++
			logger.ManagerError("Ready checkout export skipped " + firstNonEmptyString(account.Email, account.Username, account.ID) + ": stored session missing.")
			continue
		}
		targets = append(targets, accountUpdateWorkTarget{Number: index + 1, Account: account, Session: session})
	}
	concurrency = boundedAccountUtilityConcurrency(concurrency, len(targets), e.cfg.Engine.TaskConcurrency)
	logger.Manager(fmt.Sprintf("Ready checkout CSV checking %d account(s) (%d banned) in number order with %d thread(s).", len(targets), bannedSkipped, concurrency))
	updateReadyCheckoutCSVTitle(len(targets), summary, false)
	if err := writePreparedTaskRows(outputPath, preparedTaskHeaders, nil); err != nil {
		return summary, err
	}
	results := e.runCreditCheckoutWorkers(ctx, targets, concurrency)
	rowsByNumber := map[int]map[string]string{}
	for result := range results {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		summary.Checked++
		accountIndex := findAccountIndex(accounts, result.Target.Account.ID)
		if result.Err != nil {
			logger.ManagerError(fmt.Sprintf("Ready checkout credit check failed for %s: %s", firstNonEmptyString(result.Target.Account.Email, result.Target.Account.Username, result.Target.Account.ID), result.Err.Error()))
			if result.Banned && accountIndex >= 0 {
				e.markAccountSessionBanned(&accounts[accountIndex], result.Err)
				summary.Banned++
			} else if accountIndex >= 0 {
				accounts[accountIndex].LastError = "credit check failed: " + result.Err.Error()
				accounts[accountIndex].UpdatedAt = e.now()
				summary.Failed++
			}
			if saveErr := e.store.SaveAccounts(accounts); saveErr != nil {
				return summary, saveErr
			}
			updateReadyCheckoutCSVTitle(len(targets), summary, false)
			continue
		}
		if result.Skipped {
			updateReadyCheckoutCSVTitle(len(targets), summary, false)
			continue
		}
		if accountIndex >= 0 {
			if result.Account.State == AccountStateFailed {
				result.Account.State = AccountStateSignedUp
			}
			accounts[accountIndex] = result.Account
		}
		sessions = upsertSession(sessions, result.Session)
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return summary, err
		}
		if err := e.store.SaveAccounts(accounts); err != nil {
			return summary, err
		}
		if result.Row != nil {
			rowsByNumber[result.Target.Number] = result.Row
			summary.Eligible++
			rows := orderedCreditCheckoutRows(targets, rowsByNumber)
			if err := writePreparedTaskRows(outputPath, preparedTaskHeaders, rows); err != nil {
				return summary, err
			}
			summary.Exported = len(rows)
			updateReadyCheckoutCSVTitle(len(targets), summary, false)
			logger.Manager(fmt.Sprintf("Ready checkout CSV updated. Exported %d account(s) to %s.", summary.Exported, outputPath))
		}
	}

	rows := orderedCreditCheckoutRows(targets, rowsByNumber)
	if err := writePreparedTaskRows(outputPath, preparedTaskHeaders, rows); err != nil {
		return summary, err
	}
	summary.Exported = len(rows)
	updateReadyCheckoutCSVTitle(len(targets), summary, true)
	logger.Manager(fmt.Sprintf("Ready checkout CSV complete. Exported %d account(s) to %s.", summary.Exported, outputPath))
	return summary, nil
}

func updateReadyCheckoutCSVTitle(total int, summary CreditCheckoutCSVSummary, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("Ready checkout CSV complete: %d/%d checked | %d eligible | %d exported | %d failed", summary.Checked, total, summary.Eligible, summary.Exported, summary.Failed))
		return
	}
	setConsoleTitle(fmt.Sprintf("Ready checkout CSV: %d/%d checked | %d eligible | %d exported | %d failed", summary.Checked, total, summary.Eligible, summary.Exported, summary.Failed))
}

func orderedCreditCheckoutRows(targets []accountUpdateWorkTarget, rowsByNumber map[int]map[string]string) []map[string]string {
	rows := make([]map[string]string, 0, len(rowsByNumber))
	for _, target := range targets {
		if row := rowsByNumber[target.Number]; row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func (e *Engine) runCreditCheckoutWorkers(ctx context.Context, targets []accountUpdateWorkTarget, concurrency int) <-chan creditCheckoutWorkResult {
	results := make(chan creditCheckoutWorkResult, len(targets))
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
				results <- e.runCreditCheckoutTarget(ctx, target)
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

func (e *Engine) runCreditCheckoutTarget(ctx context.Context, target accountUpdateWorkTarget) creditCheckoutWorkResult {
	out := creditCheckoutWorkResult{Target: target, Account: target.Account, Session: target.Session}
	if err := ctx.Err(); err != nil {
		out.Err = err
		return out
	}
	refreshed, err := e.refreshReadyCheckoutSessionWithRetries(ctx, target.Account, target.Session)
	if err != nil {
		out.Err = err
		out.Banned = isDefiniteSessionBan(err, true)
		return out
	}
	now := e.now()
	account := target.Account
	session := refreshed
	normalizePreparedSignupSessionForExport(&account, &session, now)
	if err := validatePreparedSignupSessionExport(account, session); err != nil {
		out.Err = err
		return out
	}
	out.Account = account
	out.Session = session
	amount, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe")
	if !ok || amount <= readyCheckoutMinimumAmountSafe {
		out.Skipped = true
		return out
	}
	reward := strconv.Itoa(amount)
	label := rewardLabel(reward)
	out.Amount = amount
	out.Row = preparedRowFromAccount(account, session, reward, e.productsJSONForReward(label), "ready", now)
	return out
}

func (e *Engine) refreshReadyCheckoutSessionWithRetries(ctx context.Context, account Account, session SessionRecord) (SessionRecord, error) {
	var lastErr error
	for attempt := 1; attempt <= readyCheckoutRefreshAttempts; attempt++ {
		refreshed, err := e.refreshAccountSessionIfNeeded(ctx, account, session, true)
		if err == nil {
			return refreshed, nil
		}
		lastErr = err
		if !isTransientSessionRefreshError(err) || attempt == readyCheckoutRefreshAttempts {
			break
		}
		logger.ManagerNotice(fmt.Sprintf(
			"Ready checkout credit check retry for %s after transient refresh error (%d/%d): %s. Waiting %s.",
			firstNonEmptyString(account.Email, account.Username, account.ID),
			attempt,
			readyCheckoutRefreshAttempts,
			err.Error(),
			readyCheckoutRefreshRetryDelay,
		))
		timer := time.NewTimer(readyCheckoutRefreshRetryDelay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return SessionRecord{}, ctx.Err()
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("refresh session failed")
	}
	if isTransientSessionRefreshError(lastErr) {
		return SessionRecord{}, fmt.Errorf("transient refresh failed after %d separate attempts: %w", readyCheckoutRefreshAttempts, lastErr)
	}
	return SessionRecord{}, lastErr
}

func (e *Engine) CheckFriendCodeCredits(ctx context.Context, concurrency int) (FriendCodeCreditSummary, error) {
	var summary FriendCodeCreditSummary
	records, err := loadFriendCodeRecords(e.cfg.Engine.FriendCodesPath)
	if err != nil {
		return summary, err
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return summary, err
	}
	normalSessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return summary, err
	}
	masters, err := e.store.LoadMasters()
	if err != nil {
		return summary, err
	}
	masterSessions, err := e.store.LoadMasterSessions()
	if err != nil {
		return summary, err
	}
	targets := make([]friendCodeCreditTarget, 0, len(records))
	bannedSkipped := 0
	for index, record := range records {
		code := strings.TrimSpace(record.Code)
		if code == "" {
			summary.Skipped++
			continue
		}
		accountIndex := findAccountByFriendCodeUsername(accounts, code)
		if accountIndex >= 0 {
			account := accounts[accountIndex]
			if storedAccountIsKnownBanned(account) {
				summary.Banned++
				summary.Skipped++
				bannedSkipped++
				continue
			}
			session, ok := findSessionForAccount(normalSessions, account)
			if !ok {
				for _, query := range []string{code, account.Username, account.Email, account.ID, account.SessionID} {
					if recovered, recoveredOK := findSessionByQuery(normalSessions, query); recoveredOK {
						session = recovered
						ok = true
						break
					}
				}
			}
			if !ok {
				summary.Missing++
				logger.ManagerError("Friend code credit check skipped " + code + ": normal account exists but normal session is missing.")
				continue
			}
			targets = append(targets, friendCodeCreditTarget{Number: index + 1, Code: code, Status: record.Status, Account: account, Session: session, Normal: true})
			continue
		}
		if session, ok := findNormalSessionByFriendCodeUsername(normalSessions, code); ok {
			account := accountFromSessionRecord(session, e.now())
			if strings.TrimSpace(account.Username) == "" {
				account.Username = strings.TrimPrefix(strings.TrimSpace(code), "@")
			}
			account.LastError = ""
			account.UpdatedAt = e.now()
			accounts = upsertAccount(accounts, account)
			targets = append(targets, friendCodeCreditTarget{Number: index + 1, Code: code, Status: record.Status, Account: account, Session: session, Normal: true})
			continue
		}

		masterIndex := findMasterByFriendCode(masters, code)
		if masterIndex < 0 {
			summary.Missing++
			logger.ManagerError("Friend code credit check skipped " + code + ": no normal account username or stored master matched this friend_codes.csv code.")
			continue
		}
		master := masters[masterIndex]
		if master.State == MasterStateFailed || master.State == MasterStateDormant {
			summary.Skipped++
			continue
		}
		session, ok := findSessionForMaster(masterSessions, master)
		if !ok {
			summary.Missing++
			logger.ManagerError("Friend code credit check skipped " + firstNonEmptyString(master.Username, master.Email, master.ID) + ": master session missing.")
			continue
		}
		targets = append(targets, friendCodeCreditTarget{Number: index + 1, Code: code, Status: record.Status, Master: master, Session: session})
	}
	concurrency = boundedAccountUtilityConcurrency(concurrency, len(targets), e.cfg.Engine.TaskConcurrency)
	logger.Manager(fmt.Sprintf("Checking %d friend code credit balance(s) (%d banned) from %s with %d thread(s).", len(targets), bannedSkipped, e.cfg.Engine.FriendCodesPath, concurrency))
	updateFriendCodeCreditTitle(len(targets), summary, 0)
	results := e.runFriendCodeCreditWorkers(ctx, targets, concurrency)
	now := e.now()
	creditCounts := map[int]int{}
	totalCredits := 0
	summaryCurrency := "GBP"
	for result := range results {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		remaining := len(targets) - summary.Checked - 1
		accountIndex := findAccountIndex(accounts, result.Target.Account.ID)
		masterIndex := findMasterIndex(masters, result.Target.Master.ID)
		summary.Checked++
		if result.Err != nil {
			logger.ManagerError(fmt.Sprintf("Friend code credit check failed for %s: %s", friendCodeTargetLabel(result.Target), result.Err.Error()))
			if result.Banned {
				summary.Banned++
				if result.Target.Normal && accountIndex >= 0 {
					accounts[accountIndex].State = AccountStateFailed
					accounts[accountIndex].LastError = result.Err.Error()
					accounts[accountIndex].UpdatedAt = now
				} else if masterIndex >= 0 {
					masters[masterIndex].State = MasterStateFailed
					masters[masterIndex].LastError = result.Err.Error()
					masters[masterIndex].UpdatedAt = now
				}
			} else {
				summary.Failed++
				if result.Target.Normal && accountIndex >= 0 {
					accounts[accountIndex].LastError = result.Err.Error()
					accounts[accountIndex].UpdatedAt = now
				} else if masterIndex >= 0 {
					masters[masterIndex].LastError = result.Err.Error()
					masters[masterIndex].UpdatedAt = now
				}
			}
			if err := e.store.SaveAccounts(accounts); err != nil {
				return summary, err
			}
			if err := e.store.SaveMasters(masters); err != nil {
				return summary, err
			}
			updateFriendCodeCreditTitle(len(targets), summary, remaining)
			continue
		}
		summary.Healthy++
		e.handleRewardCreditUpdate(ctx, friendCodeRewardAccount(result.Target), result.Target.Session, result.Session)
		if result.Target.Normal && accountIndex >= 0 {
			if accounts[accountIndex].State == AccountStateFailed {
				accounts[accountIndex].State = AccountStateSignedUp
			}
			accounts[accountIndex].LastError = ""
			accounts[accountIndex].UpdatedAt = now
			accounts[accountIndex].SessionID = firstNonEmptyString(result.Session.ID, accounts[accountIndex].SessionID)
			normalSessions = upsertSession(normalSessions, result.Session)
		} else if masterIndex >= 0 {
			masters[masterIndex].LastHealthCheckAt = &now
			masters[masterIndex].LastError = ""
			masters[masterIndex].UpdatedAt = now
			masterSessions = upsertSession(masterSessions, result.Session)
		}
		if err := e.store.SaveNormalSessions(normalSessions); err != nil {
			return summary, err
		}
		if err := e.store.SaveMasterSessions(masterSessions); err != nil {
			return summary, err
		}
		if err := e.store.SaveAccounts(accounts); err != nil {
			return summary, err
		}
		if err := e.store.SaveMasters(masters); err != nil {
			return summary, err
		}
		amountLabel := rewardCreditLine(result.Amount, metadataValue(result.Session.Metadata, "reward_balance_currency"))
		logger.Manager(fmt.Sprintf("Friend code credit confirmed for %s: %s.", friendCodeTargetLabel(result.Target), amountLabel))
		if currency := metadataValue(result.Session.Metadata, "reward_balance_currency"); currency != "" {
			summaryCurrency = currency
		}
		creditCounts[result.Amount]++
		totalCredits += result.Amount
		if result.Updated {
			summary.Updated++
			logger.Manager(fmt.Sprintf("Friend code %s credit updated: %s -> %s.", friendCodeTargetLabel(result.Target), rewardCreditLine(result.OldAmount, metadataValue(result.Session.Metadata, "reward_balance_currency")), amountLabel))
		} else {
			logger.Manager(fmt.Sprintf("Friend code %s credit checked: %s.", friendCodeTargetLabel(result.Target), amountLabel))
		}
		updateFriendCodeCreditTitle(len(targets), summary, remaining)
	}
	if err := e.store.SaveNormalSessions(normalSessions); err != nil {
		return summary, err
	}
	if err := e.store.SaveMasterSessions(masterSessions); err != nil {
		return summary, err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return summary, err
	}
	if err := e.store.SaveMasters(masters); err != nil {
		return summary, err
	}
	e.mirrorMasterFriendCodes(masters)
	if err := e.webhook.SendFriendCodeCreditSummary(ctx, totalCredits, summaryCurrency, creditCounts, summary); err != nil {
		e.sendErrorWebhook(ctx, "Friend code credit summary webhook failed", err, map[string]string{
			"Mode": "Check Friend Code Credits",
		})
	} else {
		logger.Manager("Friend code credit summary webhook sent.")
	}
	setConsoleTitle(fmt.Sprintf("Friend code credits complete: %d/%d checked | %d healthy | %d banned | %d failed", summary.Checked, len(targets), summary.Healthy, summary.Banned, summary.Failed))
	return summary, nil
}

func updateFriendCodeCreditTitle(total int, summary FriendCodeCreditSummary, remaining int) {
	if remaining < 0 {
		remaining = 0
	}
	setConsoleTitle(fmt.Sprintf("Friend code credits: %d/%d checked | %d remaining | %d healthy | %d banned | %d failed", summary.Checked, total, remaining, summary.Healthy, summary.Banned, summary.Failed))
}

func findMasterByFriendCode(masters []MasterAccount, code string) int {
	code = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(code)), "@")
	if code == "" {
		return -1
	}
	for index, master := range masters {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(master.Username), "@"), code) {
			return index
		}
	}
	for index, master := range masters {
		if strings.EqualFold(strings.TrimSpace(master.ID), code) ||
			strings.EqualFold(strings.TrimSpace(master.AccountID), code) ||
			strings.EqualFold(strings.TrimSpace(master.Email), code) {
			return index
		}
	}
	return -1
}

func findAccountByFriendCodeUsername(accounts []Account, code string) int {
	code = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(code)), "@")
	if code == "" {
		return -1
	}
	for index, account := range accounts {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(account.Username), "@"), code) {
			return index
		}
	}
	return -1
}

func findNormalSessionByFriendCodeUsername(sessions []SessionRecord, code string) (SessionRecord, bool) {
	code = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(code)), "@")
	if code == "" {
		return SessionRecord{}, false
	}
	for _, session := range sessions {
		username := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(metadataValue(session.Metadata, "username"))), "@")
		if username == code {
			return session, true
		}
	}
	return SessionRecord{}, false
}

func friendCodeTargetLabel(target friendCodeCreditTarget) string {
	if target.Normal {
		return firstNonEmptyString(target.Code, target.Account.Username, target.Account.Email, target.Account.ID)
	}
	return firstNonEmptyString(target.Code, target.Master.Username, target.Master.Email, target.Master.ID)
}

func friendCodeRewardAccount(target friendCodeCreditTarget) Account {
	if target.Normal {
		return target.Account
	}
	return Account{
		ID:        firstNonEmptyString(target.Master.AccountID, target.Master.ID),
		Username:  target.Master.Username,
		Email:     target.Master.Email,
		SessionID: target.Master.SessionID,
		State:     AccountStateSignedUp,
	}
}

func (e *Engine) runFriendCodeCreditWorkers(ctx context.Context, targets []friendCodeCreditTarget, concurrency int) <-chan friendCodeCreditResult {
	results := make(chan friendCodeCreditResult, len(targets))
	if len(targets) == 0 {
		close(results)
		return results
	}
	jobs := make(chan friendCodeCreditTarget)
	var wg sync.WaitGroup
	for workerIndex := 0; workerIndex < concurrency; workerIndex++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			if err := e.staggerWorkerStart(ctx, workerIndex, concurrency); err != nil {
				return
			}
			for target := range jobs {
				results <- e.runFriendCodeCreditTarget(ctx, target)
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

func (e *Engine) runFriendCodeCreditTarget(ctx context.Context, target friendCodeCreditTarget) friendCodeCreditResult {
	out := friendCodeCreditResult{Target: target, Session: target.Session}
	oldAmount, _ := metadataIntValue(target.Session.Metadata, "reward_balance_amount_safe")
	var refreshed SessionRecord
	var healthy bool
	var err error
	if target.Normal {
		refreshed, healthy, err = e.flows.RefreshSession(ctx, target.Session)
	} else {
		refreshed, healthy, err = e.flows.CheckMasterHealth(ctx, target.Master, target.Session)
	}
	if err != nil {
		out.Err = err
		out.Banned = isDefiniteSessionBan(err, healthy)
		return out
	}
	if !healthy {
		out.Err = fmt.Errorf("friend code session refresh returned unhealthy")
		out.Banned = true
		return out
	}
	out.Session = refreshed
	out.OldAmount = oldAmount
	if errText := metadataValue(refreshed.Metadata, "reward_credit_check_error"); errText != "" {
		out.Err = fmt.Errorf("reward credit check failed: %s", errText)
		return out
	}
	newAmount, ok := metadataIntValue(refreshed.Metadata, "reward_balance_amount_safe")
	if !ok {
		out.Err = fmt.Errorf("reward credit check returned no proven balance")
		return out
	}
	out.Amount = newAmount
	out.Updated = newAmount != oldAmount
	return out
}

type accountDashboardData struct {
	OrderResults      []OrderCheckResult
	OrderErr          error
	Messages          []AccountMessageSummary
	MessagesErr       error
	Notifications     []AccountNotification
	NotificationsErr  error
	Addresses         []AccountAddress
	AddressesErr      error
	PaymentMethods    []AccountPaymentMethod
	PaymentMethodsErr error
}

func (e *Engine) fetchAccountDashboardData(ctx context.Context, target AccountOrdersInput, includeProfile bool, storedOrders []Order) accountDashboardData {
	var data accountDashboardData
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
		defer cancel()
		data.OrderResults, data.OrderErr = e.flows.ListOrders(sectionCtx, target)
		if data.OrderErr != nil {
			var fallbackErr error
			data.OrderResults, fallbackErr = e.checkStoredAccountOrders(sectionCtx, target.Account, target.Session, storedOrders)
			if fallbackErr != nil {
				data.OrderErr = joinErr(data.OrderErr, fallbackErr)
			} else if len(data.OrderResults) > 0 {
				data.OrderErr = nil
			}
		}
	}()
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
		defer cancel()
		data.Messages, data.MessagesErr = e.flows.ListConversations(sectionCtx, target)
	}()
	go func() {
		defer wg.Done()
		sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
		defer cancel()
		data.Notifications, data.NotificationsErr = e.flows.ListNotifications(sectionCtx, target)
	}()
	if includeProfile {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
			defer cancel()
			data.Addresses, data.AddressesErr = e.flows.ListAddresses(sectionCtx, target)
		}()
		go func() {
			defer wg.Done()
			sectionCtx, cancel := context.WithTimeout(ctx, accountUpdateSectionTimeout)
			defer cancel()
			data.PaymentMethods, data.PaymentMethodsErr = e.flows.ListPaymentMethods(sectionCtx, target)
		}()
	}
	wg.Wait()
	return data
}

func (e *Engine) ListAccountAddresses(ctx context.Context, query string) ([]AccountAddress, error) {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return e.flows.ListAddresses(ctx, AccountOrdersInput{Account: account, Session: session})
}

func (e *Engine) CreateAccountAddress(ctx context.Context, query string, address AccountAddressInput) (AccountAddress, error) {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return AccountAddress{}, err
	}
	return e.flows.CreateAddress(ctx, AccountOrdersInput{Account: account, Session: session}, address)
}

func (e *Engine) UpdateAccountAddress(ctx context.Context, query string, address AccountAddressInput) (AccountAddress, error) {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return AccountAddress{}, err
	}
	return e.flows.UpdateAddress(ctx, AccountOrdersInput{Account: account, Session: session}, address)
}

func (e *Engine) DeleteAccountAddress(ctx context.Context, query string, addressID string) error {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return err
	}
	return e.flows.DeleteAddress(ctx, AccountOrdersInput{Account: account, Session: session}, addressID)
}

func (e *Engine) ListAccountPaymentMethods(ctx context.Context, query string) ([]AccountPaymentMethod, error) {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return e.flows.ListPaymentMethods(ctx, AccountOrdersInput{Account: account, Session: session})
}

func (e *Engine) MarkAccountNotificationsViewed(ctx context.Context, query string) error {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return err
	}
	return e.flows.MarkNotificationsViewed(ctx, AccountOrdersInput{Account: account, Session: session})
}

func (e *Engine) AcknowledgeAccountNotification(ctx context.Context, query string, notificationID string) error {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return err
	}
	return e.flows.AcknowledgeNotification(ctx, AccountOrdersInput{Account: account, Session: session}, notificationID)
}

func (e *Engine) AddAccountPaymentMethod(ctx context.Context, query string, payment AccountPaymentInput) (AccountPaymentMethod, error) {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return AccountPaymentMethod{}, err
	}
	method, refreshed, err := e.flows.AddPaymentMethod(ctx, AccountOrdersInput{Account: account, Session: session}, payment)
	if err != nil {
		return AccountPaymentMethod{}, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err == nil {
		sessions = upsertSession(sessions, refreshed)
		_ = e.store.SaveNormalSessions(sessions)
	}
	return method, nil
}

func (e *Engine) DeleteAccountPaymentMethod(ctx context.Context, query string, paymentID string) error {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return err
	}
	return e.flows.DeletePaymentMethod(ctx, AccountOrdersInput{Account: account, Session: session}, paymentID)
}

func (e *Engine) OpenConversation(ctx context.Context, query string, conversationID string) (AccountConversation, error) {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return AccountConversation{}, err
	}
	return e.flows.GetConversation(ctx, AccountOrdersInput{Account: account, Session: session}, conversationID)
}

func (e *Engine) SendAccountMessage(ctx context.Context, request AccountMessageRequest) error {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, request.Query)
	if err != nil {
		return err
	}
	if strings.TrimSpace(request.OrderID) != "" && strings.TrimSpace(request.ConversationID) == "" {
		order, ok := e.orderByAccountAndInput(account.ID, request.OrderID)
		if !ok {
			return fmt.Errorf("no stored order matched %q", request.OrderID)
		}
		request.ConversationID = metadataValue(order.Metadata, "conversation_id")
		if strings.TrimSpace(request.Username) == "" {
			request.Username = metadataValue(order.Metadata, "seller_username")
		}
	}
	return e.flows.SendMessage(ctx, AccountOrdersInput{Account: account, Session: session}, request)
}

func (e *Engine) FollowAccountUser(ctx context.Context, query string, username string) error {
	account, session, err := e.accountAndFreshSessionByQuery(ctx, query)
	if err != nil {
		return err
	}
	return e.flows.FollowUser(ctx, AccountOrdersInput{Account: account, Session: session}, username)
}

func (e *Engine) BulkSubmitPendingReviews(ctx context.Context) (BulkReviewSummary, error) {
	var summary BulkReviewSummary
	reviews, err := e.ListPendingReviews()
	if err != nil {
		return summary, err
	}
	updateBulkReviewTitle(len(reviews), summary, false)
	for _, review := range reviews {
		if err := ctx.Err(); err != nil {
			return summary, err
		}
		text, err := e.reviews.Pick(e.cfg.Engine.ReviewTextChance)
		if err != nil {
			summary.Failed++
			logger.ManagerError("Bulk review text selection failed: " + err.Error())
			updateBulkReviewTitle(len(reviews), summary, false)
			continue
		}
		if err := e.SubmitPendingReview(ctx, review.ID, []int{5, 5, 5, 5}, text); err != nil {
			summary.Failed++
			logger.ManagerError(fmt.Sprintf("Bulk review failed for %s: %s", firstNonEmptyString(review.DisplayID, review.ExternalOrderID, review.OrderID), err.Error()))
			updateBulkReviewTitle(len(reviews), summary, false)
			continue
		}
		summary.Submitted++
		updateBulkReviewTitle(len(reviews), summary, false)
	}
	updateBulkReviewTitle(len(reviews), summary, true)
	return summary, nil
}

func updateBulkReviewTitle(total int, summary BulkReviewSummary, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("Bulk reviews complete: %d/%d submitted | %d failed", summary.Submitted, total, summary.Failed))
		return
	}
	setConsoleTitle(fmt.Sprintf("Bulk reviews: %d/%d submitted | %d failed", summary.Submitted, total, summary.Failed))
}

func (e *Engine) accountUpdateTargets(csvPath string) ([]Account, []Account, []SessionRecord, []map[string]string, error) {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	csvPath = strings.TrimSpace(csvPath)
	if csvPath == "" {
		recoveredAccounts, recoveredSessions, _, err := e.recoverAllSessionScopeFromStorageCSVs(accounts, sessions)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		accounts = recoveredAccounts
		sessions = recoveredSessions
		return accounts, accounts, sessions, nil, nil
	}
	_, rows, err := readPreparedTaskRows(preparedCSVPath(csvPath))
	if err != nil {
		return nil, nil, nil, nil, err
	}
	updatedAccounts, targets, recovered := accountUpdateTargetsForPreparedRows(accounts, sessions, rows, e.now())
	if recovered > 0 {
		if err := e.store.SaveAccounts(updatedAccounts); err != nil {
			return nil, nil, nil, nil, err
		}
		logger.Manager(fmt.Sprintf("Update Accounts recovered %d account record(s) from CSV/session data.", recovered))
	}
	return updatedAccounts, targets, sessions, rows, nil
}

func accountUpdateTargetsForPreparedRows(accounts []Account, sessions []SessionRecord, rows []map[string]string, now time.Time) ([]Account, []Account, int) {
	return accountUpdateTargetsForPreparedRowsWithLogging(accounts, sessions, rows, now, true)
}

func accountUpdateTargetsForPreparedRowsSilent(accounts []Account, sessions []SessionRecord, rows []map[string]string, now time.Time) ([]Account, []Account, int) {
	return accountUpdateTargetsForPreparedRowsWithLogging(accounts, sessions, rows, now, false)
}

func accountUpdateTargetsForPreparedRowsWithLogging(accounts []Account, sessions []SessionRecord, rows []map[string]string, now time.Time, verbose bool) ([]Account, []Account, int) {
	out := []Account{}
	recovered := 0
	for rowNumber, row := range rows {
		index := findPreparedAccountIndex(accounts, row)
		session, hasSession := sessionForPreparedUpdateRow(sessions, row)
		if index < 0 {
			if !hasSession {
				if verbose {
					logger.ManagerError(fmt.Sprintf("Update Accounts CSV row %d skipped: no stored account/session matched %s.", rowNumber+1, firstNonEmptyString(row["email"], row["username"], row["account_id"], row["session_id"])))
				}
				continue
			}
			account := accountFromSessionRecord(session, now)
			account.SessionID = firstNonEmptyString(session.ID, account.SessionID)
			accounts = upsertAccount(accounts, account)
			recovered++
			index = findAccountIndex(accounts, account.ID)
			if index < 0 {
				if verbose {
					logger.ManagerError(fmt.Sprintf("Update Accounts CSV row %d skipped: recovered account could not be indexed for %s.", rowNumber+1, firstNonEmptyString(row["email"], row["username"], row["account_id"], row["session_id"])))
				}
				continue
			}
		}
		account := accounts[index]
		applyPreparedCheckoutRowIdentity(&account, nil, row)
		if hasSession {
			applyPreparedCheckoutRowIdentity(&account, &session, row)
			if account.SessionID == "" {
				account.SessionID = session.ID
			}
			if sessionIndex := findSessionIndexForPreparedUpdateRow(sessions, row, session); sessionIndex >= 0 {
				sessions[sessionIndex] = session
			}
		}
		account.UpdatedAt = now
		if account.LastError != "" && hasSession {
			account.LastError = ""
		}
		accounts[index] = account
		out = append(out, accounts[index])
	}
	return accounts, out, recovered
}

func sessionForPreparedUpdateRow(sessions []SessionRecord, row map[string]string) (SessionRecord, bool) {
	for _, query := range []string{row["session_id"], row["account_id"], row["email"], row["username"]} {
		if session, ok := findSessionByQuery(sessions, query); ok {
			return session, true
		}
	}
	return SessionRecord{}, false
}

func findSessionIndexForPreparedUpdateRow(sessions []SessionRecord, row map[string]string, target SessionRecord) int {
	for i, session := range sessions {
		if target.ID != "" && session.ID == target.ID {
			return i
		}
		if target.AccountID != "" && session.AccountID == target.AccountID {
			return i
		}
		if email := firstNonEmptyString(metadataValue(target.Metadata, "email"), row["email"]); email != "" && strings.EqualFold(metadataValue(session.Metadata, "email"), email) {
			return i
		}
		if username := firstNonEmptyString(metadataValue(target.Metadata, "username"), row["username"]); username != "" && strings.EqualFold(metadataValue(session.Metadata, "username"), username) {
			return i
		}
	}
	return -1
}

func (e *Engine) accountAndSessionByQuery(query string) (Account, SessionRecord, error) {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return Account{}, SessionRecord{}, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return Account{}, SessionRecord{}, err
	}
	accounts, index, session, err := e.ensureAccountFromQuery(accounts, sessions, query)
	if err != nil {
		return Account{}, SessionRecord{}, err
	}
	return accounts[index], session, nil
}

func (e *Engine) ensureAccountFromQuery(accounts []Account, sessions []SessionRecord, query string) ([]Account, int, SessionRecord, error) {
	index := findAccountByQuery(accounts, query)
	if index >= 0 {
		session, ok := findSessionForAccount(sessions, accounts[index])
		if !ok {
			return accounts, -1, SessionRecord{}, fmt.Errorf("stored session missing for %s", firstNonEmptyString(accounts[index].Email, accounts[index].Username, accounts[index].ID))
		}
		return accounts, index, session, nil
	}
	session, ok := findSessionByQuery(sessions, query)
	if !ok {
		return accounts, -1, SessionRecord{}, fmt.Errorf("no stored account/session matched %q", query)
	}
	account := accountFromSessionRecord(session, e.now())
	accounts = upsertAccount(accounts, account)
	if err := e.store.SaveAccounts(accounts); err != nil {
		return accounts, -1, SessionRecord{}, err
	}
	logger.Manager("Recreated missing account record from stored session for " + firstNonEmptyString(account.Email, account.Username, account.ID) + ".")
	index = findAccountIndex(accounts, account.ID)
	if index < 0 {
		return accounts, -1, SessionRecord{}, fmt.Errorf("stored account not found after recreating from session for %s", query)
	}
	return accounts, index, session, nil
}

func (e *Engine) accountAndFreshSessionByQuery(ctx context.Context, query string) (Account, SessionRecord, error) {
	account, session, err := e.accountAndSessionByQuery(query)
	if err != nil {
		return Account{}, SessionRecord{}, err
	}
	refreshed, err := e.refreshAccountManagerSessionAuthIfNeeded(ctx, account, session, false)
	if err != nil {
		return Account{}, SessionRecord{}, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err == nil {
		sessions = upsertSession(sessions, refreshed)
		_ = e.store.SaveNormalSessions(sessions)
	}
	return account, refreshed, nil
}

func (e *Engine) refreshAccountManagerSessionAuthIfNeeded(ctx context.Context, account Account, session SessionRecord, force bool) (SessionRecord, error) {
	return e.refreshAccountSessionAuthIfNeeded(ctx, account, sessionWithFastProxyAuthRetry(session), force)
}

func (e *Engine) refreshAccountManagerSessionIfNeeded(ctx context.Context, account Account, session SessionRecord, force bool) (SessionRecord, error) {
	return e.refreshAccountSessionIfNeeded(ctx, account, sessionWithFastProxyAuthRetry(session), force)
}

func (e *Engine) refreshAccountSessionAuthIfNeeded(ctx context.Context, account Account, session SessionRecord, force bool) (SessionRecord, error) {
	if !force && accountUtilityAuthSessionIsFresh(session, e.now()) {
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

func (e *Engine) refreshAccountSessionIfNeeded(ctx context.Context, account Account, session SessionRecord, force bool) (SessionRecord, error) {
	if !force && accountUtilitySessionIsFresh(session, e.now()) {
		return session, nil
	}
	refreshed, healthy, err := e.flows.RefreshSession(ctx, session)
	if err != nil {
		return SessionRecord{}, err
	}
	if !healthy {
		return SessionRecord{}, fmt.Errorf("session for %s is unhealthy", firstNonEmptyString(account.Email, account.Username, account.ID))
	}
	if err := validateRefreshedSession(session, refreshed); err != nil {
		return SessionRecord{}, err
	}
	e.handleRewardCreditUpdate(ctx, account, session, refreshed)
	return refreshed, nil
}

func sessionWithFastProxyAuthRetry(session SessionRecord) SessionRecord {
	metadata := map[string]any{}
	for key, value := range session.Metadata {
		metadata[key] = value
	}
	metadata["fast_proxy_auth_retry"] = true
	session.Metadata = metadata
	return session
}

func markSessionRecentlyRefreshedForActions(session SessionRecord, now time.Time) SessionRecord {
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	session.Metadata["last_session_refresh_at"] = now.UTC().Format(time.RFC3339)
	return session
}

func accountUtilitySessionIsFresh(session SessionRecord, now time.Time) bool {
	if session.UpdatedAt.IsZero() || now.Sub(session.UpdatedAt) > accountUtilityFreshSessionTTL {
		return false
	}
	return metadataValue(session.Metadata, "reward_credit_checked_at") != "" ||
		metadataValue(session.Metadata, "reward_balance_currency") != "" ||
		metadataValue(session.Metadata, "reward_balance_amount_safe") != ""
}

func (e *Engine) orderByAccountAndInput(accountID string, input string) (Order, bool) {
	orders, err := e.store.LoadOrders()
	if err != nil {
		return Order{}, false
	}
	input = strings.TrimSpace(input)
	for _, order := range orders {
		if order.AccountID != accountID {
			continue
		}
		if strings.EqualFold(order.ID, input) ||
			strings.EqualFold(order.ExternalOrderID, input) ||
			strings.EqualFold(metadataValue(order.Metadata, "display_id"), input) ||
			strings.EqualFold(metadataValue(order.Metadata, "encoded_id"), input) {
			return order, true
		}
	}
	return Order{}, false
}

func findAccountByQuery(accounts []Account, query string) int {
	query = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	for i, account := range accounts {
		if strings.ToLower(strings.TrimSpace(account.Email)) == query ||
			strings.ToLower(strings.TrimSpace(account.Username)) == query ||
			strings.ToLower(strings.TrimSpace(account.ID)) == query {
			return i
		}
	}
	for i, account := range accounts {
		if strings.Contains(strings.ToLower(account.Email), query) || strings.Contains(strings.ToLower(account.Username), query) {
			return i
		}
	}
	return -1
}

func dashboardOrdersForAccount(orders []Order, accountID string) []AccountDashboardOrder {
	out := []AccountDashboardOrder{}
	for _, order := range orders {
		if order.AccountID != accountID {
			continue
		}
		items := metadataStringList(order.Metadata, "items")
		if len(items) == 0 {
			if item := metadataValue(order.Metadata, "listing_title"); item != "" {
				items = []string{item}
			}
		}
		out = append(out, AccountDashboardOrder{
			OrderID:          order.ID,
			ExternalOrderID:  order.ExternalOrderID,
			Display:          firstNonEmptyString(metadataValue(order.Metadata, "display_id"), order.ExternalOrderID, order.ID),
			Items:            items,
			SellerUsername:   metadataValue(order.Metadata, "seller_username"),
			Status:           firstNonEmptyString(metadataValue(order.Metadata, "pretty_status"), metadataValue(order.Metadata, "overview_status"), string(order.State)),
			Tracking:         order.TrackingNumber,
			TrackingURL:      metadataValue(order.Metadata, "tracking_url"),
			Total:            metadataValue(order.Metadata, "total"),
			OrderDate:        metadataValue(order.Metadata, "order_date"),
			ReviewPending:    order.State == OrderStateReviewReady && !order.ReviewCompleted,
			ReviewReference:  order.ReviewReferenceID,
			ConversationID:   metadataValue(order.Metadata, "conversation_id"),
			ConversationUser: metadataValue(order.Metadata, "seller_username"),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Compare(out[i].OrderDate, out[j].OrderDate) > 0
	})
	return out
}

func (e *Engine) processMessageDiscoveries(ctx context.Context, snapshots map[string]accountDiscoverySnapshot, account Account, messages []AccountMessageSummary) (int, int) {
	key := accountSummaryKey(account)
	snapshot := snapshots[key]
	if snapshot.Messages == nil {
		snapshot.Messages = map[string]string{}
	}
	discoveries := 0
	webhooks := 0
	for _, message := range messages {
		if strings.TrimSpace(message.ConversationID) == "" {
			continue
		}
		signature := messageSignature(message)
		if snapshot.Messages[message.ConversationID] == signature {
			continue
		}
		snapshot.Messages[message.ConversationID] = signature
		if message.Unread {
			discoveries++
			if err := e.webhook.SendMessageDiscovery(ctx, account, message); err != nil {
				e.sendErrorWebhook(ctx, "Message discovery webhook failed", err, map[string]string{"Account": account.Email})
			} else {
				webhooks++
			}
		}
	}
	snapshots[key] = snapshot
	return discoveries, webhooks
}

func annotateMessageCredits(messages []AccountMessageSummary, credits string) {
	for i := range messages {
		messages[i].AccountCredits = credits
	}
}

func annotateNotificationCredits(notifications []AccountNotification, credits string) {
	for i := range notifications {
		notifications[i].AccountCredits = credits
	}
}

func (e *Engine) processNotificationDiscoveries(ctx context.Context, snapshots map[string]accountDiscoverySnapshot, account Account, notifications []AccountNotification) (int, int) {
	key := accountSummaryKey(account)
	snapshot := snapshots[key]
	if snapshot.Notifications == nil {
		snapshot.Notifications = map[string]string{}
	}
	discoveries := 0
	webhooks := 0
	for _, notification := range notifications {
		id := firstNonEmptyString(notification.ID, notification.ActivityID)
		if id == "-" {
			continue
		}
		signature := notificationSignature(notification)
		if snapshot.Notifications[id] == signature {
			continue
		}
		snapshot.Notifications[id] = signature
		if notification.Unread {
			discoveries++
			if isRewardOnlyNotification(notification) {
				continue
			}
			if err := e.webhook.SendNotificationDiscovery(ctx, account, notification); err != nil {
				e.sendErrorWebhook(ctx, "Notification discovery webhook failed", err, map[string]string{"Account": account.Email})
			} else {
				webhooks++
			}
		}
	}
	snapshots[key] = snapshot
	return discoveries, webhooks
}

func isRewardOnlyNotification(notification AccountNotification) bool {
	text := strings.ToLower(strings.Join([]string{
		notification.Message,
		notification.ActivityType,
		notification.RelatedListing,
		notification.DeepLink,
	}, " "))
	if isNonActionableNotificationText(text) {
		return true
	}
	rewardSignals := []string{
		"£10.00",
		"£20.00",
		"£200.00",
		"gbp 10.00",
		"gbp 20.00",
		"gbp 200.00",
		"10.00 gbp",
		"20.00 gbp",
		"200.00 gbp",
		"signing up through a referral",
		"referral from",
		"you got £",
		"reward credit",
	}
	for _, signal := range rewardSignals {
		if strings.Contains(text, strings.ToLower(signal)) {
			return true
		}
	}
	return false
}

func isNonActionableNotificationText(text string) bool {
	text = normalizeNotificationFilterText(text)
	if strings.Contains(text, "has started following you") {
		return true
	}
	if strings.Contains(text, "your order for") &&
		strings.Contains(text, "has been completed") &&
		strings.Contains(text, "rate your experience") {
		return true
	}
	if strings.Contains(text, "for referring") &&
		strings.Contains(text, "completed a purchase") &&
		(strings.Contains(text, "you've earned £") ||
			strings.Contains(text, "you’ve earned £") ||
			strings.Contains(text, "you have earned £")) {
		return true
	}
	return false
}

func normalizeNotificationFilterText(value string) string {
	value = strings.NewReplacer(
		"**", "",
		"Â£", "£",
		"’", "'",
		"\u00a0", " ",
		"\r", " ",
		"\n", " ",
		"\t", " ",
	).Replace(value)
	value = strings.ToLower(value)
	for strings.Contains(value, "  ") {
		value = strings.ReplaceAll(value, "  ", " ")
	}
	return strings.TrimSpace(value)
}

func messageSignature(message AccountMessageSummary) string {
	return strings.Join([]string{
		message.Username,
		message.Preview,
		message.LinkedOrder,
		message.LinkedItem,
		message.LastMessageAt,
		fmt.Sprint(message.Unread),
	}, "|")
}

func notificationSignature(notification AccountNotification) string {
	return strings.Join([]string{
		notification.ActivityID,
		notification.ActivityType,
		notification.Message,
		notification.CreatedAt,
		fmt.Sprint(notification.Unread),
	}, "|")
}

func (e *Engine) loadAccountDiscoverySnapshots() (map[string]accountDiscoverySnapshot, error) {
	var snapshots map[string]accountDiscoverySnapshot
	if err := e.store.LoadBackendJSON("account_discovery_snapshots", &snapshots); err != nil {
		return nil, err
	}
	if snapshots == nil {
		snapshots = map[string]accountDiscoverySnapshot{}
	}
	return snapshots, nil
}

func (e *Engine) saveAccountDiscoverySnapshots(snapshots map[string]accountDiscoverySnapshot) error {
	if snapshots == nil {
		snapshots = map[string]accountDiscoverySnapshot{}
	}
	return e.store.SaveBackendJSON("account_discovery_snapshots", snapshots)
}
