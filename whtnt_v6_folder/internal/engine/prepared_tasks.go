package engine

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

const defaultPreparedTasksCSVPath = "storage/prepared_tasks.csv"

type preparedSignupAttemptError struct {
	cause error
}

func (e preparedSignupAttemptError) Error() string {
	if e.cause == nil {
		return "prepared signup attempt failed"
	}
	return e.cause.Error()
}

func (e preparedSignupAttemptError) Unwrap() error {
	return e.cause
}

type preparedSignupPersistError struct {
	cause error
}

func (e preparedSignupPersistError) Error() string {
	if e.cause == nil {
		return "prepared signup persist failed"
	}
	return e.cause.Error()
}

func (e preparedSignupPersistError) Unwrap() error {
	return e.cause
}

type preparedSignupResult struct {
	job preparedSignupJob
	err error
}

type preparedSignupJob struct {
	slot    int
	attempt int
}

type preparedSignupPersistRequest struct {
	signup        SignupResult
	referral      referralSelection
	status        string
	masterCounted bool
	reply         chan preparedSignupPersistResponse
}

type preparedSignupPersistResponse struct {
	masterCounted bool
	err           error
}

var preparedTaskHeaders = []string{
	"email",
	"account_id",
	"session_id",
	"username",
	"password",
	"first_name",
	"last_name",
	"referrer_username",
	"reward_amount",
	"reward_label",
	"status",
	"last_error",
	"address_line1",
	"address_line2",
	"town",
	"postcode",
	"address_last_name",
	"card_number",
	"card_cvc",
	"card_exp_month",
	"card_exp_year",
	"card_postcode",
	"payment_method_id",
	"follow_seller",
	"products_json",
	"order_status",
	"order_ids",
	"updated_at",
}

func (e *Engine) RunPreparedSignupCSV(ctx context.Context, path string, count int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("prepared signup recovered panic: %v", recovered)
		}
	}()
	path = preparedCSVPath(path)
	if count < 1 {
		return fmt.Errorf("prepared signup count must be greater than zero")
	}

	concurrency := e.cfg.Engine.TaskConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > count {
		concurrency = count
	}
	logger.Manager(fmt.Sprintf("Aged CSV mode using concurrency %d.", concurrency))
	updateAgedCSVSignupTitle(count, 0, 0, 0, 0)

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	persistRequests := make(chan preparedSignupPersistRequest)
	persistDone := make(chan struct{})
	go func() {
		defer close(persistDone)
		for request := range persistRequests {
			masterCounted := request.masterCounted
			err := e.persistPreparedSignupProgress(path, request.signup, request.referral, &masterCounted, request.status)
			request.reply <- preparedSignupPersistResponse{masterCounted: masterCounted, err: err}
			close(request.reply)
		}
	}()
	jobs := make(chan preparedSignupJob, concurrency)
	results := make(chan preparedSignupResult, concurrency)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		workerIndex := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.staggerWorkerStart(workCtx, workerIndex, concurrency); err != nil {
				return
			}
			for job := range jobs {
				if workCtx.Err() != nil {
					return
				}
				err := e.createPreparedSignupSafely(workCtx, persistRequests, job.slot, count)
				select {
				case results <- preparedSignupResult{job: job, err: err}:
				case <-workCtx.Done():
					return
				}
			}
		}()
	}

	successful := 0
	inFlight := 0
	retries := 0
	failed := 0
	launched := 0
	nextSlot := 1
	pendingRetries := []preparedSignupJob{}
	slotFailures := map[int]int{}
	var firstErr error
	sendJob := func(job preparedSignupJob) bool {
		select {
		case jobs <- job:
			launched++
			inFlight++
			updateAgedCSVSignupTitle(count, successful, inFlight, retries, launched)
			return true
		case <-workCtx.Done():
			return false
		}
	}
	nextJob := func() (preparedSignupJob, bool) {
		if len(pendingRetries) > 0 {
			job := pendingRetries[0]
			copy(pendingRetries, pendingRetries[1:])
			pendingRetries = pendingRetries[:len(pendingRetries)-1]
			return job, true
		}
		job := preparedSignupJob{slot: nextSlot, attempt: 1}
		nextSlot++
		return job, true
	}
	for inFlight < concurrency && successful+inFlight < count {
		job, _ := nextJob()
		if !sendJob(job) {
			break
		}
	}

	for successful < count && firstErr == nil {
		select {
		case <-workCtx.Done():
			firstErr = workCtx.Err()
		case result := <-results:
			inFlight--
			if result.err != nil {
				if isPreparedSignupHardFailure(result.err) {
					failed++
					firstErr = result.err
					cancel()
					setConsoleTitle(fmt.Sprintf("Aged CSV failed: %d/%d generated, %d retry, %d failed", successful, count, retries, failed))
					break
				}
				retries++
				slotFailures[result.job.slot]++
				if slotFailures[result.job.slot] >= 3 {
					failed++
					delete(slotFailures, result.job.slot)
					logger.ManagerError(fmt.Sprintf("Prepared signup task %d failed 3 time(s); cancelling that task and restarting with a clean one: %s", result.job.slot, result.err.Error()))
				} else {
					nextAttempt := result.job.attempt + 1
					pendingRetries = append(pendingRetries, preparedSignupJob{slot: result.job.slot, attempt: nextAttempt})
					logger.ManagerError(fmt.Sprintf("Prepared signup task %d failed attempt %d/3; retrying with a fresh client: %s", result.job.slot, slotFailures[result.job.slot], result.err.Error()))
				}
				updateAgedCSVSignupTitle(count, successful, inFlight, retries, launched)
			} else {
				successful++
				delete(slotFailures, result.job.slot)
				logger.Manager(fmt.Sprintf("Aged CSV progress: %d/%d successful.", successful, count))
				updateAgedCSVSignupTitle(count, successful, inFlight, retries, launched)
			}
			for firstErr == nil && inFlight < concurrency && successful+inFlight < count {
				job, _ := nextJob()
				if !sendJob(job) {
					firstErr = workCtx.Err()
					break
				}
			}
		}
	}
	close(jobs)
	wg.Wait()
	close(persistRequests)
	<-persistDone
	if firstErr != nil {
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	logger.Manager(fmt.Sprintf("Prepared CSV mode complete. Added %d row(s) to %s.", count, path))
	setConsoleTitle(fmt.Sprintf("Aged CSV complete: %d generated", count))
	return nil
}

func updateAgedCSVSignupTitle(total int, successful int, inFlight int, retries int, launched int) {
	remaining := total - successful
	if remaining < 0 {
		remaining = 0
	}
	setConsoleTitle(fmt.Sprintf("Aged CSV gen: %d/%d ok | %d remaining | %d running | %d launched | %d retries", successful, total, remaining, inFlight, launched, retries))
}

func (e *Engine) createPreparedSignupSafely(ctx context.Context, persistRequests chan<- preparedSignupPersistRequest, index int, total int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = preparedSignupAttemptError{cause: fmt.Errorf("prepared signup panic recovered: %v", recovered)}
		}
	}()
	return e.createPreparedSignup(ctx, persistRequests, index, total)
}

func isPreparedSignupHardFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var persistErr preparedSignupPersistError
	return errors.As(err, &persistErr)
}

func (e *Engine) persistPreparedSignupViaWriter(ctx context.Context, requests chan<- preparedSignupPersistRequest, signup SignupResult, referral referralSelection, masterCounted bool, status string) (bool, error) {
	reply := make(chan preparedSignupPersistResponse, 1)
	request := preparedSignupPersistRequest{
		signup:        signup,
		referral:      referral,
		status:        status,
		masterCounted: masterCounted,
		reply:         reply,
	}
	select {
	case requests <- request:
	case <-ctx.Done():
		return masterCounted, ctx.Err()
	}
	select {
	case response := <-reply:
		return response.masterCounted, response.err
	case <-ctx.Done():
		return masterCounted, ctx.Err()
	}
}

func isPreparedSignupRetryable(err error) bool {
	if err == nil {
		return false
	}
	return !isPreparedSignupHardFailure(err)
}

func (e *Engine) RunSpecificAccountGeneration(ctx context.Context, email string, password string) (path string, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	setConsoleTitle("Create account: " + email)
	defer func() {
		if err != nil {
			setConsoleTitle("Create account failed: " + email)
			return
		}
		setConsoleTitle("Create account complete: " + email)
	}()
	password = strings.TrimSpace(password)
	if email == "" || !strings.Contains(email, "@") {
		return "", fmt.Errorf("valid email is required")
	}
	if password == "" {
		return "", fmt.Errorf("password is required")
	}
	path = filepath.Join("storage", safeCSVFilename(email)+".csv")

	masters, err := e.store.LoadMasters()
	if err != nil {
		return "", err
	}
	logger.Manager("Specific account generation starting for " + email + ".")
	masterCounted := false
	signup, referral, _, err := e.runSignupWithReferralRetry(ctx, masters, "Specific account generation", func(referral referralSelection) (SignupResult, error) {
		return e.flows.PrepareCheckoutAccount(ctx, SignupInput{
			FriendCode: referral.Username,
			Email:      email,
			Password:   password,
		})
	})
	if err != nil {
		return "", err
	}
	if err := e.persistPreparedSignupProgress(path, signup, referral, &masterCounted, "ready"); err != nil {
		return "", err
	}
	logger.Manager("Specific account generation stored CSV: " + path)
	return path, nil
}

func (e *Engine) createPreparedSignup(ctx context.Context, persistRequests chan<- preparedSignupPersistRequest, index int, total int) error {
	masters, err := e.store.LoadMasters()
	if err != nil {
		return err
	}

	logger.Manager(fmt.Sprintf("Prepared signup %d/%d starting.", index, total))
	masterCounted := false
	rewardCheckpointStored := false
	signup, referral, _, err := e.runSignupWithReferralRetry(ctx, masters, "Prepared signup", func(referral referralSelection) (SignupResult, error) {
		return e.flows.PrepareCheckoutAccount(ctx, SignupInput{
			FriendCode: referral.Username,
			RewardDetected: func(progress SignupResult) error {
				updatedMasterCounted, err := e.persistPreparedSignupViaWriter(ctx, persistRequests, progress, referral, masterCounted, "reward_detected")
				masterCounted = updatedMasterCounted
				if err != nil {
					return preparedSignupPersistError{cause: err}
				}
				rewardCheckpointStored = true
				logger.Manager("Prepared CSV updated after reward detection for " + progress.Account.Email + ".")
				return nil
			},
		})
	})
	if err != nil {
		if rewardCheckpointStored {
			logger.ManagerError(fmt.Sprintf("Prepared signup %d/%d hit an error after reward checkpoint; counting saved checkpoint and starting a fresh task: %s", index, total, err.Error()))
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		var persistErr preparedSignupPersistError
		if errors.As(err, &persistErr) {
			return err
		}
		return preparedSignupAttemptError{cause: err}
	}

	updatedMasterCounted, err := e.persistPreparedSignupViaWriter(ctx, persistRequests, signup, referral, masterCounted, "ready")
	masterCounted = updatedMasterCounted
	if err != nil {
		if rewardCheckpointStored {
			logger.ManagerError(fmt.Sprintf("Prepared signup %d/%d could not finalize ready row after reward checkpoint; keeping saved checkpoint: %s", index, total, err.Error()))
			return nil
		}
		return preparedSignupPersistError{cause: err}
	}
	logger.Manager(fmt.Sprintf("Prepared signup %d/%d stored for %s.", index, total, signup.Account.Email))
	return nil
}

func safeCSVFilename(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			builder.WriteRune(char)
		case char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '@' || char == '.' || char == '_' || char == '-':
			builder.WriteRune(char)
		default:
			builder.WriteByte('_')
		}
	}
	out := builder.String()
	if out == "" {
		return "specific_account"
	}
	return out
}

func (e *Engine) persistPreparedSignupProgress(path string, signup SignupResult, referral referralSelection, masterCounted *bool, status string) error {
	var account Account
	var session SessionRecord
	shouldSendWebhook := false

	e.preparedMu.Lock()
	if err := func() error {
		defer e.preparedMu.Unlock()

		accounts, err := e.store.LoadAccounts()
		if err != nil {
			return err
		}
		sessions, err := e.store.LoadNormalSessions()
		if err != nil {
			return err
		}
		masters, err := e.store.LoadMasters()
		if err != nil {
			return err
		}

		now := e.now()
		account = signup.Account
		session = signup.Session
		account.MasterUsernameUsed = referral.Username
		account.SessionID = session.ID
		account.State = AccountStateSignedUp
		account.IsMasterCandidate = false
		account.CreatedAt = ensureTime(account.CreatedAt, now)
		account.UpdatedAt = now
		normalizePreparedSignupSessionForExport(&account, &session, now)
		if status != "reward_detected" {
			if err := validatePreparedSignupSessionExport(account, session); err != nil {
				return err
			}
		}
		initializeRewardCreditBaseline(&session)

		existingSession, hasExistingSession := findSessionForAccount(sessions, account)
		if hasExistingSession {
			mergeExistingPreparedSessionMetadata(&session, existingSession)
		}
		shouldSendWebhook = status == "reward_detected" || !hasExistingSession

		accounts = upsertAccount(accounts, account)
		sessions = upsertSession(sessions, session)
		if referral.MasterID != "" && masterCounted != nil && !*masterCounted {
			if masterIndex := findMasterIndex(masters, referral.MasterID); masterIndex >= 0 {
				masters[masterIndex].SignupCount++
				masters[masterIndex].UpdatedAt = now
				if masters[masterIndex].SignupCount >= masters[masterIndex].SignupCap {
					masters[masterIndex].State = MasterStateAging
					masters[masterIndex].AgingStartedAt = &now
					logger.Manager(fmt.Sprintf("Master %s reached cap and moved to Aging.", masters[masterIndex].Username))
				}
				*masterCounted = true
			}
		}
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return err
		}
		if err := e.store.SaveAccounts(accounts); err != nil {
			return err
		}
		if err := e.store.SaveMasters(masters); err != nil {
			return err
		}
		if referral.MasterID != "" {
			if masterIndex := findMasterIndex(masters, referral.MasterID); masterIndex >= 0 {
				e.mirrorMasterFriendCode(masters[masterIndex])
			}
		}

		rewardAmount := rewardAmountSafeFromSession(session)
		reward := ""
		if rewardAmount > 0 {
			reward = strconv.Itoa(rewardAmount)
		}
		row := preparedRowFromAccount(account, session, reward, e.productsJSONForReward(checkoutRewardLabelForBalance(rewardAmount)), status, e.now())
		if err := upsertPreparedTaskRow(path, row); err != nil {
			return err
		}
		return nil
	}(); err != nil {
		return err
	}

	if shouldSendWebhook && e.sendHighRewardWebhookIfNeeded(context.Background(), account, &session) {
		e.preparedMu.Lock()
		defer e.preparedMu.Unlock()
		sessions, err := e.store.LoadNormalSessions()
		if err != nil {
			return err
		}
		sessions = upsertSession(sessions, session)
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return err
		}
	}
	return nil
}

func normalizePreparedSignupSessionForExport(account *Account, session *SessionRecord, now time.Time) {
	if account == nil || session == nil {
		return
	}
	account.Email = strings.TrimSpace(account.Email)
	account.Username = strings.TrimSpace(account.Username)
	account.ID = strings.TrimSpace(account.ID)
	session.ID = strings.TrimSpace(session.ID)
	session.AccountID = strings.TrimSpace(session.AccountID)
	if session.AccountID == "" {
		session.AccountID = account.ID
	}
	if account.SessionID == "" {
		account.SessionID = session.ID
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if metadataValue(session.Metadata, "email") == "" && account.Email != "" {
		session.Metadata["email"] = account.Email
	}
	if metadataValue(session.Metadata, "username") == "" && account.Username != "" {
		session.Metadata["username"] = account.Username
	}
	if metadataValue(session.Metadata, "first_name") == "" && account.FirstName != "" {
		session.Metadata["first_name"] = account.FirstName
	}
	if metadataValue(session.Metadata, "last_name") == "" && account.LastName != "" {
		session.Metadata["last_name"] = account.LastName
	}
	if metadataValue(session.Metadata, "proxy") == "" && account.Proxy != "" {
		session.Metadata["proxy"] = account.Proxy
	}
	if metadataValue(session.Metadata, "ip") == "" && account.IP != "" {
		session.Metadata["ip"] = account.IP
	}
	session.CreatedAt = ensureTime(session.CreatedAt, now)
	session.UpdatedAt = ensureTime(session.UpdatedAt, now)
}

func validatePreparedSignupSessionExport(account Account, session SessionRecord) error {
	if strings.TrimSpace(account.Email) == "" {
		return fmt.Errorf("prepared signup session export invalid: account email is empty")
	}
	if strings.TrimSpace(account.ID) == "" {
		return fmt.Errorf("prepared signup session export invalid for %s: account id is empty", account.Email)
	}
	if strings.TrimSpace(session.ID) == "" {
		return fmt.Errorf("prepared signup session export invalid for %s: session id is empty", account.Email)
	}
	if strings.TrimSpace(session.AccountID) == "" {
		return fmt.Errorf("prepared signup session export invalid for %s: session account id is empty", account.Email)
	}
	if metadataValue(session.Metadata, "email") == "" {
		return fmt.Errorf("prepared signup session export invalid for %s: session email metadata is empty", account.Email)
	}
	if sessionCookieValue(session.Cookies, "__Secure-access-token") == "" {
		return fmt.Errorf("prepared signup session export invalid for %s: access token cookie is empty", account.Email)
	}
	if sessionCookieValue(session.Cookies, "__Secure-refresh-token") == "" {
		return fmt.Errorf("prepared signup session export invalid for %s: refresh token cookie is empty", account.Email)
	}
	return nil
}

func mergeExistingPreparedSessionMetadata(session *SessionRecord, existing SessionRecord) {
	if session == nil || existing.Metadata == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	for key, value := range existing.Metadata {
		if _, ok := session.Metadata[key]; !ok {
			session.Metadata[key] = value
		}
	}
}

func (e *Engine) RunPreparedCheckoutCSV(ctx context.Context, path string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("prepared checkout recovered panic: %v", recovered)
		}
	}()
	path = preparedCSVPath(path)
	headers, rows, err := readPreparedTaskRows(path)
	if err != nil {
		return err
	}
	processed := 0
	eligible := 0
	failed := 0
	for _, row := range rows {
		status := strings.ToLower(strings.TrimSpace(row["status"]))
		if status == "banned" || status == "no_balance" {
			continue
		}
		if preparedCheckoutFollowSellerPending(row) {
			eligible++
			continue
		}
		if status == "checked_out" && preparedCheckoutRowOrderIDsCoverBasket(row) {
			continue
		}
		eligible++
	}
	updateAgedCSVCheckoutTitle(eligible, 0, 0, false)
	for i := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		status := strings.ToLower(strings.TrimSpace(rows[i]["status"]))
		if status == "banned" || status == "no_balance" {
			continue
		}
		if preparedCheckoutFollowSellerPending(rows[i]) {
			followed, err := e.completePreparedCheckoutFollowSeller(ctx, rows[i])
			if err != nil {
				logger.ManagerError(fmt.Sprintf("Prepared checkout row %d follow_seller failed: %s; continuing checkout.", i+1, err.Error()))
			} else {
				rows[i]["follow_seller"] = "completed"
				rows[i]["updated_at"] = e.now().Format(time.RFC3339)
				if writeErr := writePreparedTaskRows(path, headers, rows); writeErr != nil {
					return writeErr
				}
				logger.Manager(fmt.Sprintf("Prepared checkout row %d follow_seller completed (%d seller(s)).", i+1, followed))
				if err := e.sleepDelay(ctx, "after_follow_seller"); err != nil {
					return err
				}
			}
		}
		if status == "checked_out" {
			if preparedCheckoutRowOrderIDsCoverBasket(rows[i]) {
				if preparedCheckoutFollowSellerCompleted(rows[i]) {
					processed++
					updateAgedCSVCheckoutTitle(eligible, processed, failed, false)
				}
				continue
			}
			skip, changed, err := e.confirmPreparedCheckoutRowComplete(ctx, rows[i])
			if err != nil {
				logger.ManagerError(fmt.Sprintf("Prepared checkout row %d completion check failed: %s; resuming checkout.", i+1, err.Error()))
			}
			if changed {
				if writeErr := writePreparedTaskRows(path, headers, rows); writeErr != nil {
					return writeErr
				}
			}
			if skip {
				processed++
				updateAgedCSVCheckoutTitle(eligible, processed, failed, false)
				continue
			}
			logger.Manager(fmt.Sprintf("Prepared checkout row %d was marked checked_out but order_ids do not cover the basket; resuming checkout.", i+1))
		}
		status = strings.ToLower(strings.TrimSpace(rows[i]["status"]))
		if status == "banned" || status == "no_balance" {
			continue
		}
		processed++
		updateAgedCSVCheckoutTitle(eligible, processed, failed, false)
		var immediateWriteErr error
		if err := e.checkoutPreparedRowWithOptions(ctx, rows[i], preparedCheckoutOptions{
			OrderPlaced: func(progress CheckoutResult) {
				now := e.now()
				orderIDs := checkoutResultOrderIDs(progress)
				if len(orderIDs) > 0 {
					rows[i]["status"] = "checked_out"
					rows[i]["last_error"] = ""
					rows[i]["order_ids"] = strings.Join(orderIDs, "|")
					rows[i]["updated_at"] = now.Format(time.RFC3339)
					if writeErr := writePreparedTaskRows(path, headers, rows); writeErr != nil {
						immediateWriteErr = writeErr
						logger.ManagerError(fmt.Sprintf("Prepared checkout row %d immediate CSV update failed: %s", i+1, writeErr.Error()))
					} else {
						logger.Manager(fmt.Sprintf("Prepared checkout row %d updated before after_order delay.", i+1))
					}
				}
			},
		}); err != nil {
			failed++
			if errors.Is(err, ErrNoBalance) {
				rows[i]["status"] = "NO_BALANCE"
			} else {
				rows[i]["status"] = "error"
			}
			rows[i]["last_error"] = err.Error()
			rows[i]["updated_at"] = e.now().Format(time.RFC3339)
			logger.ManagerError(fmt.Sprintf("Prepared checkout row %d failed: %s", i+1, err.Error()))
			e.sendErrorWebhook(ctx, taskErrorWebhookTitle(err), err, map[string]string{
				"Mode":    "Aged CSV checkout",
				"Row":     strconv.Itoa(i + 1),
				"Account": firstNonEmptyString(rows[i]["email"], rows[i]["username"], rows[i]["account_id"]),
				"Status":  rows[i]["status"],
			})
			updateAgedCSVCheckoutTitle(eligible, processed, failed, false)
		}
		if immediateWriteErr != nil {
			return immediateWriteErr
		}
		if writeErr := writePreparedTaskRows(path, headers, rows); writeErr != nil {
			return writeErr
		}
		if i < len(rows)-1 {
			if err := e.sleepDelay(ctx, "bulk_checkout_between_rows"); err != nil {
				return err
			}
		}
	}
	logger.Manager(fmt.Sprintf("Prepared checkout CSV complete. Processed %d row(s) from %s.", processed, path))
	updateAgedCSVCheckoutTitle(eligible, processed, failed, true)
	return nil
}

func checkoutResultOrderIDs(result CheckoutResult) []string {
	orders := result.Orders
	if len(orders) == 0 && result.Order.ID != "" {
		orders = []Order{result.Order}
	}
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		if strings.TrimSpace(order.ID) != "" {
			ids = append(ids, strings.TrimSpace(order.ID))
		}
	}
	return ids
}

func preparedCheckoutFollowSellerPending(row map[string]string) bool {
	value := strings.ToLower(strings.TrimSpace(row["follow_seller"]))
	switch value {
	case "true", "yes", "y", "1":
		return true
	default:
		return false
	}
}

func preparedCheckoutFollowSellerCompleted(row map[string]string) bool {
	value := strings.ToLower(strings.TrimSpace(row["follow_seller"]))
	return value == "completed" || value == "done"
}

func (e *Engine) completePreparedCheckoutFollowSeller(ctx context.Context, row map[string]string) (int, error) {
	products, err := preparedCheckoutProductsForRow(row)
	if err != nil {
		return 0, err
	}
	listingIDs := make([]string, 0, len(products))
	seen := map[string]bool{}
	for _, product := range products {
		listingID := strings.TrimSpace(product.ProductListingID)
		if listingID == "" || seen[listingID] {
			continue
		}
		seen[listingID] = true
		listingIDs = append(listingIDs, listingID)
	}
	if len(listingIDs) == 0 {
		return 0, fmt.Errorf("products_json has no listing IDs to follow")
	}

	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return 0, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return 0, err
	}
	accounts, accountIndex, session, err := e.ensurePreparedCheckoutAccount(accounts, sessions, row)
	if err != nil {
		return 0, err
	}
	account := accounts[accountIndex]
	applyPreparedCheckoutRowIdentity(&account, &session, row)

	refreshed, healthy, err := e.flows.RefreshSessionWithoutCredits(ctx, session)
	if err != nil {
		return 0, fmt.Errorf("refresh session: %w", err)
	}
	if !healthy {
		return 0, fmt.Errorf("refresh session returned unhealthy")
	}
	session = refreshed
	session.AccountID = account.ID
	account.SessionID = session.ID
	applyPreparedCheckoutRowIdentity(&account, &session, row)
	accounts[accountIndex] = account
	sessions = upsertSession(sessions, session)
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return 0, err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return 0, err
	}

	return e.flows.FollowListingSellers(ctx, AccountOrdersInput{Account: account, Session: session}, listingIDs)
}

type preparedCheckoutCSVProduct struct {
	ProductID        string
	ProductListingID string
	ProductName      string
	ProductPrice     int
}

func preparedCheckoutRowOrderIDsCoverBasket(row map[string]string) bool {
	products, err := preparedCheckoutProductsForRow(row)
	if err != nil || len(products) == 0 {
		return false
	}
	return len(preparedRowOrderIDs(row)) >= len(products)
}

func (e *Engine) confirmPreparedCheckoutRowComplete(ctx context.Context, row map[string]string) (bool, bool, error) {
	products, err := preparedCheckoutProductsForRow(row)
	if err != nil {
		return false, false, err
	}
	expected := len(products)
	if expected == 0 {
		return false, false, fmt.Errorf("products_json has no products")
	}
	if len(preparedRowOrderIDs(row)) >= expected {
		return true, false, nil
	}

	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return false, false, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return false, false, err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return false, false, err
	}
	accounts, accountIndex, session, err := e.ensurePreparedCheckoutAccount(accounts, sessions, row)
	if err != nil {
		return false, false, err
	}
	account := accounts[accountIndex]
	applyPreparedCheckoutRowIdentity(&account, &session, row)
	accounts[accountIndex] = account

	refreshed, healthy, err := e.flows.RefreshSession(ctx, session)
	if err != nil {
		return false, false, fmt.Errorf("refresh session: %w", err)
	}
	if !healthy {
		return false, false, fmt.Errorf("refresh session returned unhealthy")
	}
	session = refreshed
	session.AccountID = account.ID
	account.SessionID = session.ID
	applyPreparedCheckoutRowIdentity(&account, &session, row)
	accounts[accountIndex] = account
	sessions = upsertSession(sessions, session)

	live, err := e.flows.ListOrders(ctx, AccountOrdersInput{Account: account, Session: session})
	if err != nil {
		return false, false, fmt.Errorf("list live orders: %w", err)
	}
	now := e.now()
	matchedIDs := preparedCheckoutMatchedOrderIDs(products, live)
	for _, result := range live {
		order := orderFromCheckResult(account.ID, result, now)
		if strings.TrimSpace(order.ID) != "" {
			orders = upsertOrder(orders, order)
		}
	}
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return false, false, err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return false, false, err
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return false, false, err
	}

	row["updated_at"] = now.Format(time.RFC3339)
	if len(matchedIDs) >= expected {
		row["status"] = "checked_out"
		row["last_error"] = ""
		row["order_ids"] = strings.Join(matchedIDs, "|")
		return true, true, nil
	}
	row["status"] = "ready"
	row["last_error"] = fmt.Sprintf("checked_out row had %d/%d order_ids; live check confirmed %d/%d matching order(s)", len(preparedRowOrderIDs(row)), expected, len(matchedIDs), expected)
	return false, true, nil
}

func preparedCheckoutProductsForRow(row map[string]string) ([]preparedCheckoutCSVProduct, error) {
	if row == nil {
		return nil, fmt.Errorf("missing checkout row")
	}
	return parsePreparedCheckoutCSVProducts(row["products_json"], row["reward_label"])
}

func parsePreparedCheckoutCSVProducts(productsJSON string, rewardLabel string) ([]preparedCheckoutCSVProduct, error) {
	trimmed := strings.TrimSpace(productsJSON)
	if trimmed == "" {
		return nil, fmt.Errorf("products_json is required")
	}
	var products []preparedCheckoutCSVProduct
	if err := json.Unmarshal([]byte(trimmed), &products); err == nil {
		return validatePreparedCheckoutCSVProducts(products)
	}
	var prizeBaskets map[string][][]preparedCheckoutCSVProduct
	if err := json.Unmarshal([]byte(trimmed), &prizeBaskets); err == nil && len(prizeBaskets) > 0 {
		key := firstNonEmptyString(rewardLabel, "£10")
		baskets := prizeBaskets[key]
		if len(baskets) == 0 {
			for basketKey, value := range prizeBaskets {
				if preparedCurrencyDigits(basketKey) == preparedCurrencyDigits(key) {
					baskets = value
					break
				}
			}
		}
		if len(baskets) == 0 {
			return nil, fmt.Errorf("products_json has no basket for reward %q", key)
		}
		return validatePreparedCheckoutCSVProducts(baskets[0])
	}
	var baskets [][]preparedCheckoutCSVProduct
	if err := json.Unmarshal([]byte(trimmed), &baskets); err != nil {
		return nil, fmt.Errorf("parse products_json: %w", err)
	}
	if len(baskets) == 0 {
		return nil, fmt.Errorf("products_json has no baskets")
	}
	return validatePreparedCheckoutCSVProducts(baskets[0])
}

func validatePreparedCheckoutCSVProducts(products []preparedCheckoutCSVProduct) ([]preparedCheckoutCSVProduct, error) {
	if len(products) == 0 {
		return nil, fmt.Errorf("products_json has no products")
	}
	for index := range products {
		products[index].ProductID = strings.TrimSpace(products[index].ProductID)
		products[index].ProductListingID = strings.TrimSpace(products[index].ProductListingID)
		products[index].ProductName = strings.TrimSpace(products[index].ProductName)
		if products[index].ProductName == "" {
			return nil, fmt.Errorf("products_json product %d missing ProductName", index+1)
		}
		if products[index].ProductListingID == "" {
			return nil, fmt.Errorf("products_json product %d missing ProductListingId", index+1)
		}
		if products[index].ProductID == "" {
			return nil, fmt.Errorf("products_json product %d missing ProductId", index+1)
		}
		if products[index].ProductPrice <= 0 {
			return nil, fmt.Errorf("products_json product %d missing ProductPrice", index+1)
		}
	}
	return products, nil
}

func preparedCheckoutMatchedOrderIDs(products []preparedCheckoutCSVProduct, orders []OrderCheckResult) []string {
	used := make([]bool, len(orders))
	ids := []string{}
	for _, product := range products {
		for index, order := range orders {
			if used[index] || !preparedCheckoutOrderMatchesProduct(product, order) {
				continue
			}
			id := preparedCheckoutOrderID(order)
			if id == "" {
				continue
			}
			used[index] = true
			ids = append(ids, id)
			break
		}
	}
	return ids
}

func preparedCheckoutOrderMatchesProduct(product preparedCheckoutCSVProduct, order OrderCheckResult) bool {
	listingID := strings.TrimSpace(product.ProductListingID)
	if listingID != "" {
		for _, key := range []string{"listing_id", "listingId", "product_listing_id"} {
			if strings.TrimSpace(metadataValue(order.Metadata, key)) == listingID {
				return true
			}
		}
		for _, existingID := range metadataStringList(order.Metadata, "listing_ids") {
			if strings.TrimSpace(existingID) == listingID {
				return true
			}
		}
	}
	productID := strings.TrimSpace(product.ProductID)
	if productID != "" {
		for _, key := range []string{"product_id", "productId", "ProductId"} {
			if strings.TrimSpace(metadataValue(order.Metadata, key)) == productID {
				return true
			}
		}
	}
	productName := normalizePreparedCheckoutProductName(product.ProductName)
	if productName == "" {
		return false
	}
	for _, item := range metadataStringList(order.Metadata, "items") {
		if normalizePreparedCheckoutProductName(item) == productName {
			return true
		}
	}
	for _, key := range []string{"listing_title", "item", "product_name", "ProductName"} {
		if normalizePreparedCheckoutProductName(metadataValue(order.Metadata, key)) == productName {
			return true
		}
	}
	return false
}

func preparedCheckoutOrderID(order OrderCheckResult) string {
	id := firstNonEmptyString(metadataValue(order.Metadata, "encoded_id"), order.ExternalOrderID, metadataValue(order.Metadata, "display_id"))
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "order_") {
		return id
	}
	return "order_" + id
}

func normalizePreparedCheckoutProductName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		} else {
			builder.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func preparedCurrencyDigits(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func updateAgedCSVCheckoutTitle(total int, processed int, failed int, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("Aged CSV checkout complete: %d/%d processed | %d failed", processed, total, failed))
		return
	}
	setConsoleTitle(fmt.Sprintf("Aged CSV checkout: %d/%d processed | %d failed", processed, total, failed))
}

type preparedCheckoutOptions struct {
	AllowInsufficientBalance bool
	ForceCheckout            bool
	OrderPlaced              func(CheckoutResult)
}

func (e *Engine) checkoutPreparedRow(ctx context.Context, row map[string]string) error {
	return e.checkoutPreparedRowWithOptions(ctx, row, preparedCheckoutOptions{})
}

func (e *Engine) checkoutPreparedRowWithOptions(ctx context.Context, row map[string]string, options preparedCheckoutOptions) error {
	email := strings.TrimSpace(row["email"])
	if email == "" {
		return fmt.Errorf("email is required")
	}
	if err := validatePreparedCheckoutRow(row); err != nil {
		return err
	}

	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return err
	}

	accounts, accountIndex, session, err := e.ensurePreparedCheckoutAccount(accounts, sessions, row)
	if err != nil {
		return err
	}
	account := accounts[accountIndex]
	applyPreparedCheckoutRowIdentity(&account, &session, row)
	accounts[accountIndex] = account

	refreshed, healthy, err := e.flows.RefreshSession(ctx, session)
	if err != nil {
		return fmt.Errorf("refresh session: %w", err)
	}
	if !healthy {
		return fmt.Errorf("refresh session: unhealthy")
	}
	session = refreshed
	session.AccountID = account.ID
	account.SessionID = session.ID
	applyPreparedCheckoutRowIdentity(&account, &session, row)
	accounts[accountIndex] = account
	sessions = upsertSession(sessions, session)
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}

	logger.Manager("Continuing prepared checkout for " + email + ".")
	checkout, err := e.flows.CheckoutPreparedProducts(ctx, PreparedCheckoutInput{
		Account:                  account,
		Session:                  session,
		AddressLine1:             row["address_line1"],
		AddressLine2:             row["address_line2"],
		Town:                     row["town"],
		Postcode:                 row["postcode"],
		AddressName:              row["address_last_name"],
		CardNumber:               row["card_number"],
		CardCVC:                  row["card_cvc"],
		CardExpMonth:             row["card_exp_month"],
		CardExpYear:              row["card_exp_year"],
		CardPostcode:             row["card_postcode"],
		PaymentMethodID:          row["payment_method_id"],
		ProductsJSON:             row["products_json"],
		RewardLabel:              row["reward_label"],
		SkipDelays:               strings.EqualFold(row["skip_delays"], "true"),
		ForceCheckout:            options.ForceCheckout,
		BalanceLimit:             provenCheckoutBalanceAmountSafeFromSession(session),
		AllowInsufficientBalance: options.AllowInsufficientBalance,
		OrderPlaced: func(progress CheckoutResult) {
			orders = e.recordImmediateCheckoutOrders(ctx, account, orders, progress)
			if options.OrderPlaced != nil {
				options.OrderPlaced(progress)
			}
		},
	})
	if err != nil {
		if errors.Is(err, ErrNoBalance) {
			recovered, recoverErr := e.recoverPreparedNoBalanceOrder(ctx, account, session, accounts, accountIndex, sessions, orders, row)
			if recoverErr != nil {
				logger.ManagerError(fmt.Sprintf("NO_BALANCE order recovery failed for %s: %s", email, recoverErr.Error()))
			}
			if recovered {
				return nil
			}
		}
		return err
	}

	now := e.now()
	if checkout.Session.ID != "" {
		session = checkout.Session
		session.ID = account.SessionID
		session.AccountID = account.ID
		session.CreatedAt = ensureTime(session.CreatedAt, now)
		session.UpdatedAt = now
		syncAccountIdentityFromSessionMetadata(&account, session)
		row["username"] = account.Username
		sessions = upsertSession(sessions, session)
	}

	checkoutOrders := checkout.Orders
	if len(checkoutOrders) == 0 {
		checkoutOrders = []Order{checkout.Order}
	}
	orderIDs := make([]string, 0, len(checkoutOrders))
	for index := range checkoutOrders {
		checkoutOrders[index].AccountID = account.ID
		checkoutOrders[index].State = OrderStateCreated
		checkoutOrders[index].CreatedAt = ensureTime(checkoutOrders[index].CreatedAt, now)
		checkoutOrders[index].UpdatedAt = now
		orders = upsertOrder(orders, checkoutOrders[index])
		orderIDs = append(orderIDs, checkoutOrders[index].ID)
	}
	account.State = AccountStateCheckedOut
	account.OrderID = checkoutOrders[0].ID
	account.LastError = ""
	account.UpdatedAt = now
	accounts[accountIndex] = account

	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	for _, checkoutOrder := range checkoutOrders {
		orderIndex := findOrderIndex(orders, checkoutOrder.ID)
		if orderIndex >= 0 {
			e.sendCheckoutWebhookIfNeeded(ctx, account, &orders[orderIndex])
		}
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	for _, checkoutOrder := range checkoutOrders {
		if err := appendCheckoutLabel(e.cfg.Engine.CheckoutLabelCSVPath, account, checkoutOrder, checkout.ShippingFirstName, checkout.ShippingLastName, e.now()); err != nil {
			return err
		}
		if err := appendOrderExport(account, checkoutOrder, checkout.ShippingFirstName, checkout.ShippingLastName, e.now()); err != nil {
			return err
		}
	}

	row["status"] = "checked_out"
	row["last_error"] = ""
	row["order_ids"] = strings.Join(orderIDs, "|")
	row["updated_at"] = now.Format(time.RFC3339)
	return nil
}

func (e *Engine) recoverPreparedNoBalanceOrder(ctx context.Context, account Account, session SessionRecord, accounts []Account, accountIndex int, sessions []SessionRecord, orders []Order, row map[string]string) (bool, error) {
	logger.Manager("NO_BALANCE returned for " + firstNonEmptyString(account.Email, account.Username, account.ID) + "; checking account orders before marking no balance.")
	discovered, err := e.discoverAccountOrdersWithoutRefresh(ctx, []Account{account}, orders, sessions)
	if len(discovered.currentOrders) == 0 {
		return false, err
	}
	now := e.now()
	orderIDs := make([]string, 0, len(discovered.currentOrders))
	for _, order := range discovered.currentOrders {
		if strings.TrimSpace(order.ID) == "" {
			continue
		}
		orderIDs = append(orderIDs, strings.TrimSpace(order.ID))
	}
	if len(orderIDs) == 0 {
		return false, err
	}
	account.State = AccountStateCheckedOut
	account.OrderID = orderIDs[0]
	account.LastError = ""
	account.UpdatedAt = now
	if accountIndex >= 0 && accountIndex < len(accounts) {
		accounts[accountIndex] = account
	}
	row["status"] = "checked_out"
	row["last_error"] = ""
	row["order_ids"] = strings.Join(orderIDs, "|")
	row["updated_at"] = now.Format(time.RFC3339)
	if saveErr := e.store.SaveAccounts(accounts); saveErr != nil {
		return true, saveErr
	}
	if saveErr := e.store.SaveNormalSessions(discovered.sessions); saveErr != nil {
		return true, saveErr
	}
	if saveErr := e.store.SaveOrders(discovered.orders); saveErr != nil {
		return true, saveErr
	}
	for _, recoveredOrder := range discovered.currentOrders {
		orderIndex := findOrderIndex(discovered.orders, recoveredOrder.ID)
		if orderIndex >= 0 {
			e.sendCheckoutWebhookIfNeeded(ctx, account, &discovered.orders[orderIndex])
		}
		if labelErr := appendCheckoutLabel(e.cfg.Engine.CheckoutLabelCSVPath, account, recoveredOrder, account.FirstName, account.LastName, now); labelErr != nil {
			return true, labelErr
		}
		if exportErr := appendOrderExport(account, recoveredOrder, account.FirstName, account.LastName, now); exportErr != nil {
			return true, exportErr
		}
	}
	if saveErr := e.store.SaveOrders(discovered.orders); saveErr != nil {
		return true, saveErr
	}
	logger.Manager(fmt.Sprintf("Recovered checkout context for %s after NO_BALANCE: %s.", firstNonEmptyString(account.Email, account.Username, account.ID), strings.Join(orderIDs, "|")))
	return true, err
}

func (e *Engine) ensurePreparedCheckoutAccount(accounts []Account, sessions []SessionRecord, row map[string]string) ([]Account, int, SessionRecord, error) {
	if index := findPreparedAccountIndex(accounts, row); index >= 0 {
		session, ok := findSessionForAccount(sessions, accounts[index])
		if !ok {
			for _, query := range []string{row["session_id"], row["email"], row["username"], row["account_id"]} {
				if recovered, recoveredOK := findSessionByQuery(sessions, query); recoveredOK {
					session = recovered
					ok = true
					break
				}
			}
		}
		if !ok {
			return accounts, -1, SessionRecord{}, fmt.Errorf("stored session not found for %s", firstNonEmptyString(row["email"], row["username"], accounts[index].Email, accounts[index].Username, accounts[index].ID))
		}
		return accounts, index, session, nil
	}

	query := firstNonEmptyString(row["email"], row["username"], row["session_id"], row["account_id"])
	session, ok := findSessionByQuery(sessions, query)
	if !ok {
		return accounts, -1, SessionRecord{}, fmt.Errorf("stored account/session not found for %s", firstNonEmptyString(row["email"], query))
	}
	account := accountFromSessionRecord(session, e.now())
	account.Email = firstNonEmptyString(account.Email, strings.TrimSpace(row["email"]))
	account.Username = firstNonEmptyString(account.Username, strings.TrimSpace(row["username"]))
	account.Password = firstNonEmptyString(account.Password, strings.TrimSpace(row["password"]))
	account.MasterUsernameUsed = firstNonEmptyString(account.MasterUsernameUsed, strings.TrimSpace(row["referrer_username"]))
	account.SessionID = session.ID
	session.AccountID = account.ID
	accounts = upsertAccount(accounts, account)
	if err := e.store.SaveAccounts(accounts); err != nil {
		return accounts, -1, SessionRecord{}, err
	}
	sessions = upsertSession(sessions, session)
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return accounts, -1, SessionRecord{}, err
	}
	logger.Manager("Recreated missing account record from stored session for " + firstNonEmptyString(account.Email, account.Username, account.ID) + ".")
	index := findPreparedAccountIndex(accounts, row)
	if index < 0 {
		index = findAccountIndex(accounts, account.ID)
	}
	if index < 0 {
		return accounts, -1, SessionRecord{}, fmt.Errorf("stored account not found after recreating from session for %s", firstNonEmptyString(row["email"], query))
	}
	return accounts, index, session, nil
}

func applyPreparedCheckoutRowIdentity(account *Account, session *SessionRecord, row map[string]string) {
	if account == nil || row == nil {
		return
	}
	if value := strings.TrimSpace(row["email"]); value != "" {
		account.Email = value
	}
	if value := strings.TrimSpace(row["username"]); value != "" {
		account.Username = value
	}
	if value := strings.TrimSpace(row["password"]); value != "" {
		account.Password = value
	}
	if value := strings.TrimSpace(row["first_name"]); value != "" {
		account.FirstName = value
	}
	if value := strings.TrimSpace(row["last_name"]); value != "" {
		account.LastName = value
	}
	if value := strings.TrimSpace(row["referrer_username"]); value != "" {
		account.MasterUsernameUsed = value
	}
	if strings.TrimSpace(row["address_last_name"]) == "" && strings.TrimSpace(account.LastName) != "" {
		row["address_last_name"] = strings.TrimSpace(account.LastName)
	}
	if session == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if account.Email != "" {
		session.Metadata["email"] = account.Email
	}
	if account.Username != "" {
		session.Metadata["username"] = account.Username
	}
	if account.FirstName != "" {
		session.Metadata["first_name"] = account.FirstName
	}
	if account.LastName != "" {
		session.Metadata["last_name"] = account.LastName
	}
	if account.Password != "" {
		session.Metadata["password"] = account.Password
	}
	if value := strings.TrimSpace(row["reward_amount"]); value != "" {
		session.Metadata["reward_balance_amount_safe"] = value
		session.Metadata["referral_amount"] = value
		session.Metadata["reward_balance_currency"] = firstNonEmptyString(metadataValue(session.Metadata, "reward_balance_currency"), "GBP")
		session.Metadata["reward_balance_last_seen_amount_safe"] = value
		session.Metadata["reward_balance_last_seen_currency"] = session.Metadata["reward_balance_currency"]
		session.Metadata["reward_balance_last_logged_amount_safe"] = value
		session.Metadata["reward_balance_last_logged_currency"] = session.Metadata["reward_balance_currency"]
	}
	if value := strings.TrimSpace(row["reward_label"]); value != "" {
		session.Metadata["reward_label"] = value
	}
}

func (e *Engine) RunSingleAccountCheckout(ctx context.Context, request SingleAccountCheckoutRequest) (err error) {
	email := strings.TrimSpace(request.Email)
	setConsoleTitle("Single checkout: " + email)
	defer func() {
		if err != nil {
			setConsoleTitle("Single checkout failed: " + email)
			return
		}
		setConsoleTitle("Single checkout complete: " + email)
	}()
	if email == "" {
		return fmt.Errorf("email is required")
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return err
	}
	accountIndex := findAccountByQuery(accounts, email)
	if accountIndex < 0 {
		session, ok := findSessionByQuery(sessions, email)
		if !ok {
			return fmt.Errorf("stored account/session not found for %s", email)
		}
		account := accountFromSessionRecord(session, e.now())
		accounts = upsertAccount(accounts, account)
		if err := e.store.SaveAccounts(accounts); err != nil {
			return err
		}
		logger.Manager("Recreated missing account record from stored session for " + firstNonEmptyString(account.Email, account.Username, account.ID) + ".")
		accountIndex = findAccountIndex(accounts, account.ID)
		if accountIndex < 0 {
			return fmt.Errorf("stored account not found after recreating from session for %s", email)
		}
	}
	account := accounts[accountIndex]

	session, ok := findSessionForAccount(sessions, account)
	if !ok {
		return fmt.Errorf("stored session not found for %s", email)
	}
	usePreparedCheckout := request.UseCustomDetails || request.UseCustomAddress || request.UseCustomCard || request.UseCustomProducts
	if !usePreparedCheckout {
		return e.checkoutStoredSingleAccount(ctx, account, session, request.ApproveNoBalance)
	}
	if request.UseCustomDetails {
		request.UseCustomAddress = true
		request.UseCustomCard = true
		request.UseCustomProducts = true
	}
	if usePreparedCheckout {
		refreshed, healthy, err := e.flows.RefreshSession(ctx, session)
		if err != nil {
			return fmt.Errorf("refresh session before checkout: %w", err)
		}
		if !healthy {
			return fmt.Errorf("refresh session before checkout: unhealthy")
		}
		session = refreshed
		sessions = upsertSession(sessions, session)
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return err
		}
	}

	rewardLabelValue := strings.TrimSpace(request.RewardLabel)
	if rewardLabelValue == "" {
		if amount := provenCheckoutBalanceAmountSafeFromSession(session); amount > 0 {
			rewardLabelValue = checkoutRewardLabelForBalance(amount)
		}
	}
	addressLine1 := request.AddressLine1
	addressLine2 := request.AddressLine2
	town := request.Town
	postcode := request.Postcode
	addressName := request.AddressName
	if !request.UseCustomAddress {
		if address, ok := storedCheckoutAddress(session.Metadata); ok {
			addressLine1 = address.AddressLine1
			addressLine2 = address.AddressLine2
			town = address.Town
			postcode = address.Postcode
			addressName = address.LastName
		} else {
			address, ok, err := e.defaultCheckoutAddress(ctx, account, session)
			if err != nil {
				return fmt.Errorf("lookup account addresses for %s: %w", email, err)
			}
			if !ok {
				return fmt.Errorf("stored/default address missing for %s; choose custom address", email)
			}
			addressLine1 = address.AddressLine1
			addressLine2 = address.AddressLine2
			town = address.Town
			postcode = address.Postcode
			addressName = address.LastName
			logger.Manager("Using account default shipping address for " + email + ".")
		}
	}
	cardNumber := request.CardNumber
	cardCVC := request.CardCVC
	cardExpMonth := request.CardExpMonth
	cardExpYear := request.CardExpYear
	cardPostcode := request.CardPostcode
	paymentMethodID := strings.TrimSpace(request.PaymentMethodID)
	if !request.UseCustomCard {
		if card, ok := storedCheckoutCard(session.Metadata); ok {
			cardNumber = card.Number
			cardCVC = card.CVC
			cardExpMonth = card.ExpMonth
			cardExpYear = card.ExpYear
			cardPostcode = card.Postcode
		} else {
			method, ok, err := e.defaultCheckoutPaymentMethod(ctx, account, session)
			if err != nil {
				return fmt.Errorf("lookup account payment methods for %s: %w", email, err)
			}
			if !ok {
				return fmt.Errorf("stored card/payment method missing for %s; choose custom card", email)
			}
			paymentMethodID = method.ID
			logger.Manager("Using account payment method " + firstNonEmptyString(method.CardDescription, method.ID) + " for " + email + ".")
		}
	}
	productsJSON := strings.TrimSpace(request.ProductsJSON)
	if productsJSON == "" && rewardLabelValue != "" {
		productsJSON = e.productsJSONForReward(rewardLabelValue)
	}
	if strings.TrimSpace(productsJSON) == "" || strings.TrimSpace(productsJSON) == "[]" {
		return fmt.Errorf("products_json is required because no basket could be inferred for %s", email)
	}

	row := map[string]string{
		"email":             account.Email,
		"account_id":        account.ID,
		"session_id":        session.ID,
		"username":          account.Username,
		"password":          account.Password,
		"first_name":        account.FirstName,
		"last_name":         account.LastName,
		"referrer_username": account.MasterUsernameUsed,
		"reward_amount":     strconv.Itoa(provenCheckoutBalanceAmountSafeFromSession(session)),
		"reward_label":      rewardLabelValue,
		"status":            "ready",
		"address_line1":     addressLine1,
		"address_line2":     addressLine2,
		"town":              town,
		"postcode":          postcode,
		"address_last_name": firstNonEmptyString(addressName, account.LastName),
		"card_number":       cardNumber,
		"card_cvc":          cardCVC,
		"card_exp_month":    cardExpMonth,
		"card_exp_year":     cardExpYear,
		"card_postcode":     cardPostcode,
		"payment_method_id": paymentMethodID,
		"products_json":     productsJSON,
		"skip_delays":       "true",
		"updated_at":        e.now().Format(time.RFC3339),
	}
	return e.checkoutPreparedRowWithOptions(ctx, row, preparedCheckoutOptions{
		AllowInsufficientBalance: request.ApproveNoBalance,
		ForceCheckout:            true,
	})
}

func (e *Engine) RunSingleAccountFullCheckout(ctx context.Context, query string, approveNoBalance bool) (err error) {
	query = strings.TrimSpace(query)
	setConsoleTitle("Account full checkout: " + query)
	defer func() {
		if err != nil {
			setConsoleTitle("Account full checkout failed: " + query)
			return
		}
		setConsoleTitle("Account full checkout complete: " + query)
	}()
	if query == "" {
		return fmt.Errorf("email or username is required")
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return err
	}
	accounts, index, session, err := e.ensureAccountFromQuery(accounts, sessions, query)
	if err != nil {
		return err
	}
	return e.checkoutStoredAccount(ctx, accounts[index], session, false, false, false, approveNoBalance)
}

func findSessionByQuery(sessions []SessionRecord, query string) (SessionRecord, bool) {
	query = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	if query == "" {
		return SessionRecord{}, false
	}
	for _, session := range sessions {
		if strings.ToLower(strings.TrimSpace(session.ID)) == query ||
			strings.ToLower(strings.TrimSpace(session.AccountID)) == query ||
			strings.ToLower(strings.TrimSpace(metadataValue(session.Metadata, "email"))) == query ||
			strings.ToLower(strings.TrimSpace(metadataValue(session.Metadata, "username"))) == query {
			return session, true
		}
	}
	for _, session := range sessions {
		if strings.Contains(strings.ToLower(metadataValue(session.Metadata, "email")), query) ||
			strings.Contains(strings.ToLower(metadataValue(session.Metadata, "username")), query) ||
			(strings.TrimSpace(session.AccountID) != "" && strings.Contains(strings.ToLower(session.AccountID), query)) {
			return session, true
		}
	}
	return SessionRecord{}, false
}

func accountFromSessionRecord(session SessionRecord, now time.Time) Account {
	accountID := strings.TrimSpace(session.AccountID)
	if accountID == "" {
		accountID = "acct_" + slugForID(firstNonEmptyString(metadataValue(session.Metadata, "email"), session.ID))
	}
	createdAt := session.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	updatedAt := session.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
	}
	return Account{
		ID:        accountID,
		Username:  metadataValue(session.Metadata, "username"),
		Email:     metadataValue(session.Metadata, "email"),
		FirstName: metadataValue(session.Metadata, "first_name"),
		LastName:  metadataValue(session.Metadata, "last_name"),
		IP:        metadataValue(session.Metadata, "ip"),
		Proxy:     metadataValue(session.Metadata, "proxy"),
		State:     AccountStateSignedUp,
		SessionID: session.ID,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		LastError: "Created from session CSV for checkout.",
	}
}

type storedAddressFields struct {
	AddressLine1 string
	AddressLine2 string
	Town         string
	Postcode     string
	LastName     string
}

type storedCardFields struct {
	Number   string
	CVC      string
	ExpMonth string
	ExpYear  string
	Postcode string
}

func storedCheckoutAddress(metadata map[string]any) (storedAddressFields, bool) {
	var address storedAddressFields
	if !decodeStoredMetadata(metadata, "address", &address) {
		return address, false
	}
	return address, strings.TrimSpace(address.AddressLine1) != "" &&
		strings.TrimSpace(address.Town) != "" &&
		strings.TrimSpace(address.Postcode) != ""
}

func storedCheckoutCard(metadata map[string]any) (storedCardFields, bool) {
	var card storedCardFields
	if !decodeStoredMetadata(metadata, "card", &card) {
		return card, false
	}
	return card, strings.TrimSpace(card.Number) != "" &&
		strings.TrimSpace(card.CVC) != "" &&
		strings.TrimSpace(card.ExpMonth) != "" &&
		strings.TrimSpace(card.ExpYear) != ""
}

func (e *Engine) defaultCheckoutAddress(ctx context.Context, account Account, session SessionRecord) (storedAddressFields, bool, error) {
	addresses, err := e.flows.ListAddresses(ctx, AccountOrdersInput{Account: account, Session: session})
	if err != nil {
		return storedAddressFields{}, false, err
	}
	if len(addresses) == 0 {
		return storedAddressFields{}, false, nil
	}
	selected := addresses[0]
	for _, address := range addresses {
		if address.DefaultShipping {
			selected = address
			break
		}
	}
	out := storedAddressFields{
		AddressLine1: selected.Line1,
		AddressLine2: selected.Line2,
		Town:         selected.City,
		Postcode:     selected.PostalCode,
		LastName:     firstNonEmptyString(account.LastName, lastNameFromFullName(selected.FullName)),
	}
	return out, strings.TrimSpace(out.AddressLine1) != "" &&
		strings.TrimSpace(out.Town) != "" &&
		strings.TrimSpace(out.Postcode) != "", nil
}

func (e *Engine) defaultCheckoutPaymentMethod(ctx context.Context, account Account, session SessionRecord) (AccountPaymentMethod, bool, error) {
	methods, err := e.flows.ListPaymentMethods(ctx, AccountOrdersInput{Account: account, Session: session})
	if err != nil {
		return AccountPaymentMethod{}, false, err
	}
	if len(methods) == 0 {
		return AccountPaymentMethod{}, false, nil
	}
	selected := methods[0]
	for _, method := range methods {
		if method.Default {
			selected = method
			break
		}
	}
	if strings.TrimSpace(selected.ID) == "" {
		return AccountPaymentMethod{}, false, nil
	}
	return selected, true, nil
}

func lastNameFromFullName(fullName string) string {
	parts := strings.Fields(strings.TrimSpace(fullName))
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func decodeStoredMetadata(metadata map[string]any, key string, out any) bool {
	if metadata == nil {
		return false
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return false
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	return json.Unmarshal(encoded, out) == nil
}

func syncAccountIdentityFromSessionMetadata(account *Account, session SessionRecord) {
	if account == nil {
		return
	}
	if value := strings.TrimSpace(metadataValue(session.Metadata, "username")); value != "" {
		account.Username = value
	}
	if value := strings.TrimSpace(metadataValue(session.Metadata, "email")); value != "" {
		account.Email = value
	}
	if value := strings.TrimSpace(metadataValue(session.Metadata, "first_name")); value != "" {
		account.FirstName = value
	}
	if value := strings.TrimSpace(metadataValue(session.Metadata, "last_name")); value != "" {
		account.LastName = value
	}
}

func (e *Engine) checkoutStoredSingleAccount(ctx context.Context, account Account, session SessionRecord, allowInsufficientBalance bool) error {
	return e.checkoutStoredAccount(ctx, account, session, true, true, true, allowInsufficientBalance)
}

func (e *Engine) checkoutStoredAccount(ctx context.Context, account Account, session SessionRecord, skipDelays bool, forceCheckout bool, returnSkippedError bool, allowInsufficientBalance bool) error {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return err
	}
	accountIndex := findAccountIndex(accounts, account.ID)
	if accountIndex < 0 {
		return fmt.Errorf("stored account not found for %s", firstNonEmptyString(account.Email, account.ID))
	}

	logger.Manager("Continuing stored checkout for " + firstNonEmptyString(account.Email, account.Username, account.ID) + ".")
	checkout, err := e.flows.Checkout(ctx, CheckoutInput{
		Account:                  account,
		Session:                  session,
		SkipDelays:               skipDelays,
		ForceCheckout:            forceCheckout,
		AllowInsufficientBalance: allowInsufficientBalance,
		OrderPlaced: func(progress CheckoutResult) {
			orders = e.recordImmediateCheckoutOrders(ctx, account, orders, progress)
		},
	})
	now := e.now()
	if err != nil {
		if errors.Is(err, ErrCheckoutSkipped) {
			if checkout.Session.ID != "" {
				session = checkout.Session
				session.ID = account.SessionID
				session.AccountID = account.ID
				session.CreatedAt = ensureTime(session.CreatedAt, now)
				session.UpdatedAt = now
				syncAccountIdentityFromSessionMetadata(&account, session)
				sessions = upsertSession(sessions, session)
			}
			account.State = AccountStateSignedUp
			account.LastError = ""
			account.UpdatedAt = now
			accounts[accountIndex] = account
			if saveErr := e.store.SaveNormalSessions(sessions); saveErr != nil {
				return saveErr
			}
			if saveErr := e.store.SaveAccounts(accounts); saveErr != nil {
				return saveErr
			}
			if !returnSkippedError {
				return nil
			}
			return err
		}
		if errors.Is(err, ErrNoBalance) {
			if checkout.Session.ID != "" {
				session = checkout.Session
				session.ID = account.SessionID
				session.AccountID = account.ID
				session.CreatedAt = ensureTime(session.CreatedAt, now)
				session.UpdatedAt = now
				syncAccountIdentityFromSessionMetadata(&account, session)
				sessions = upsertSession(sessions, session)
			}
			account.State = AccountStateSignedUp
			account.LastError = err.Error()
			account.UpdatedAt = now
			accounts[accountIndex] = account
			if saveErr := e.store.SaveNormalSessions(sessions); saveErr != nil {
				return saveErr
			}
			if saveErr := e.store.SaveAccounts(accounts); saveErr != nil {
				return saveErr
			}
			return err
		}
		account.State = AccountStateFailed
		account.LastError = err.Error()
		account.UpdatedAt = now
		accounts[accountIndex] = account
		_ = e.store.SaveAccounts(accounts)
		return err
	}

	if checkout.Session.ID != "" {
		session = checkout.Session
		session.ID = account.SessionID
		session.AccountID = account.ID
		session.CreatedAt = ensureTime(session.CreatedAt, now)
		session.UpdatedAt = now
		syncAccountIdentityFromSessionMetadata(&account, session)
		sessions = upsertSession(sessions, session)
	}
	checkoutOrders := checkout.Orders
	if len(checkoutOrders) == 0 {
		checkoutOrders = []Order{checkout.Order}
	}
	if len(checkoutOrders) == 0 || strings.TrimSpace(checkoutOrders[0].ID) == "" {
		return fmt.Errorf("checkout completed but did not expose an order id")
	}
	for index := range checkoutOrders {
		checkoutOrders[index].AccountID = account.ID
		checkoutOrders[index].State = OrderStateCreated
		checkoutOrders[index].CreatedAt = ensureTime(checkoutOrders[index].CreatedAt, now)
		checkoutOrders[index].UpdatedAt = now
		orders = upsertOrder(orders, checkoutOrders[index])
	}
	account.State = AccountStateCheckedOut
	account.OrderID = checkoutOrders[0].ID
	account.LastError = ""
	account.UpdatedAt = now
	accounts[accountIndex] = account

	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	for _, checkoutOrder := range checkoutOrders {
		orderIndex := findOrderIndex(orders, checkoutOrder.ID)
		if orderIndex >= 0 {
			e.sendCheckoutWebhookIfNeeded(ctx, account, &orders[orderIndex])
		}
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	for _, checkoutOrder := range checkoutOrders {
		if err := appendCheckoutLabel(e.cfg.Engine.CheckoutLabelCSVPath, account, checkoutOrder, checkout.ShippingFirstName, checkout.ShippingLastName, e.now()); err != nil {
			return err
		}
		if err := appendOrderExport(account, checkoutOrder, checkout.ShippingFirstName, checkout.ShippingLastName, e.now()); err != nil {
			return err
		}
	}
	return nil
}

func validatePreparedCheckoutRow(row map[string]string) error {
	required := []string{"address_line1", "town", "postcode", "products_json"}
	for _, key := range required {
		if strings.TrimSpace(row[key]) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	if strings.TrimSpace(row["payment_method_id"]) == "" {
		for _, key := range []string{"card_number", "card_cvc", "card_exp_month", "card_exp_year"} {
			if strings.TrimSpace(row[key]) == "" {
				return fmt.Errorf("%s is required", key)
			}
		}
	}
	return nil
}

func preparedCSVPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultPreparedTasksCSVPath
	}
	return filepath.Clean(path)
}

func preparedRowFromAccount(account Account, session SessionRecord, reward string, productsJSON string, status string, now time.Time) map[string]string {
	if strings.TrimSpace(productsJSON) == "" {
		productsJSON = "[]"
	}
	if strings.TrimSpace(status) == "" {
		status = "ready"
	}
	return map[string]string{
		"email":             account.Email,
		"account_id":        account.ID,
		"session_id":        session.ID,
		"username":          account.Username,
		"password":          account.Password,
		"first_name":        account.FirstName,
		"last_name":         account.LastName,
		"referrer_username": account.MasterUsernameUsed,
		"reward_amount":     reward,
		"reward_label":      rewardLabel(reward),
		"status":            status,
		"last_error":        "",
		"address_last_name": account.LastName,
		"products_json":     productsJSON,
		"updated_at":        now.Format(time.RFC3339),
	}
}

func (e *Engine) productsJSONForReward(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "[]"
	}
	path := strings.TrimSpace(e.cfg.Engine.BasketsJSONPath)
	if path == "" {
		path = filepath.Join("setup", "baskets.json")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "[]"
	}
	var baskets map[string][][]map[string]any
	if err := json.Unmarshal(raw, &baskets); err != nil {
		return "[]"
	}
	matchingBaskets := baskets[label]
	if len(matchingBaskets) == 0 {
		for key, value := range baskets {
			if currencyDigits(key) == currencyDigits(label) {
				matchingBaskets = value
				break
			}
		}
	}
	for _, basket := range matchingBaskets {
		if !preparedBasketInStock(basket) {
			continue
		}
		encoded, err := json.Marshal(basket)
		if err == nil {
			return string(encoded)
		}
	}
	return "[]"
}

func currencyDigits(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func preparedBasketInStock(basket []map[string]any) bool {
	if len(basket) == 0 {
		return false
	}
	for _, product := range basket {
		if inStock, exists := product["inStock"]; exists {
			if value, ok := inStock.(bool); ok && !value {
				return false
			}
		}
	}
	return true
}

func rewardLabel(amount string) string {
	amount = strings.TrimSpace(amount)
	amountSafe, err := strconv.Atoi(amount)
	if err != nil || amountSafe <= 0 {
		return ""
	}
	if amountSafe%100 == 0 {
		return fmt.Sprintf("\u00a3%d", amountSafe/100)
	}
	return fmt.Sprintf("\u00a3%.2f", float64(amountSafe)/100)
}

func appendPreparedTaskRow(path string, row map[string]string) error {
	headers, rows, err := readPreparedTaskRows(path)
	if err != nil {
		return err
	}
	rows = append(rows, row)
	return writePreparedTaskRows(path, headers, rows)
}

func upsertPreparedTaskRow(path string, row map[string]string) error {
	headers, rows, err := readPreparedTaskRows(path)
	if err != nil {
		return err
	}
	if index := findPreparedRowIndex(rows, row); index >= 0 {
		rows[index] = mergePreparedRow(rows[index], row)
	} else {
		rows = append(rows, row)
	}
	return writePreparedTaskRows(path, headers, rows)
}

func findPreparedRowIndex(rows []map[string]string, row map[string]string) int {
	accountID := strings.TrimSpace(row["account_id"])
	email := strings.TrimSpace(row["email"])
	for index, existing := range rows {
		if accountID != "" && strings.EqualFold(strings.TrimSpace(existing["account_id"]), accountID) {
			return index
		}
		if email != "" && strings.EqualFold(strings.TrimSpace(existing["email"]), email) {
			return index
		}
	}
	return -1
}

func mergePreparedRow(existing map[string]string, incoming map[string]string) map[string]string {
	out := make(map[string]string, len(existing)+len(incoming))
	for key, value := range existing {
		out[key] = value
	}
	for key, value := range incoming {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out[key] = value
	}
	return out
}

func readPreparedTaskRows(path string) ([]string, []map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, err
		}
		return append([]string(nil), preparedTaskHeaders...), nil, nil
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return append([]string(nil), preparedTaskHeaders...), nil, nil
		}
		return nil, nil, err
	}
	header = normalizePreparedHeaders(header)
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

func writePreparedTaskRows(path string, headers []string, rows []map[string]string) error {
	headers = normalizePreparedHeaders(headers)
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

func normalizePreparedHeaders(headers []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(headers)+len(preparedTaskHeaders))
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
	for _, key := range preparedTaskHeaders {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}

func replacePreparedFile(tempName string, path string) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(100+attempt*50) * time.Millisecond)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			lastErr = err
			continue
		}
		if err := os.Rename(tempName, path); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	_ = os.Remove(tempName)
	return lastErr
}

func findPreparedAccountIndex(accounts []Account, row map[string]string) int {
	accountID := strings.TrimSpace(row["account_id"])
	if accountID != "" {
		if index := findAccountIndex(accounts, accountID); index >= 0 {
			return index
		}
	}
	email := strings.TrimSpace(row["email"])
	for index, account := range accounts {
		if strings.EqualFold(account.Email, email) {
			return index
		}
	}
	return -1
}
