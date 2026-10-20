package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/logger"
)

type Dependencies struct {
	Config *appconfig.Config
	Store  Store
	Flows  Flows
}

type Engine struct {
	cfg          *appconfig.Config
	store        Store
	flows        Flows
	webhook      WebhookClient
	errorWebhook WebhookClient
	reviews      ReviewPool
	location     *time.Location

	startupRecoveryOnce sync.Once
	startupRecoveryErr  error

	unfinishedMu               sync.Mutex
	continueUnfinishedApproved bool
	startFreshApproved         bool
	preparedMu                 sync.Mutex
}

type sessionRefreshCheckError struct {
	cause error
}

func (e sessionRefreshCheckError) Error() string {
	if e.cause == nil {
		return "refresh was not successful"
	}
	return e.cause.Error()
}

func (e sessionRefreshCheckError) Unwrap() error {
	return e.cause
}

func New(deps Dependencies) *Engine {
	location, err := time.LoadLocation(deps.Config.Engine.Timezone)
	if err != nil {
		location = time.Local
	}
	return &Engine{
		cfg:          deps.Config,
		store:        deps.Store,
		flows:        deps.Flows,
		webhook:      WebhookClient{URL: deps.Config.Engine.DiscordWebhookURL},
		errorWebhook: WebhookClient{URL: deps.Config.ErrorWebhook},
		reviews:      ReviewPool{Path: deps.Config.Engine.ReviewTextJSONPath},
		location:     location,
	}
}

func (e *Engine) now() time.Time {
	return time.Now().In(e.location)
}

func (e *Engine) RunTasks(ctx context.Context) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("run recovered panic: %v", recovered)
		}
	}()
	setConsoleTitle("Full checkout: starting")
	runStartedAt := e.now()
	defer setConsoleTitle("Full checkout mode idle")
	defer e.waitForCurrentRunAccountTasksIdle(ctx.Done(), runStartedAt)

	unfinished, err := e.CountUnfinishedSessions()
	if err != nil {
		return err
	}
	if unfinished > 0 && !e.hasContinueUnfinishedApproval() && !e.hasStartFreshApproval() {
		return UnfinishedSessionsError{Count: unfinished}
	}
	defer e.clearUnfinishedRunApprovals()

	if !e.hasStartFreshApproval() {
		setConsoleTitle("Full checkout: recovery")
		if err := e.recoverStartupState(ctx); err != nil {
			logger.ManagerError("Startup recovery had errors: " + err.Error())
		}
	}
	setConsoleTitle("Full checkout: account tasks")
	if err := e.runQueuedAccountTasks(ctx); err != nil {
		return err
	}
	setConsoleTitle("Full checkout: order checks")
	if err := e.processOrdersIfDue(ctx); err != nil {
		return err
	}
	setConsoleTitle("Full checkout: master tasks")
	return e.processMasters(ctx)
}

func (e *Engine) RunSessionChecks(ctx context.Context, iterations int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("session check recovered panic: %v", recovered)
		}
	}()

	if iterations < 1 {
		return fmt.Errorf("session check iterations must be greater than zero")
	}

	logger.Manager(fmt.Sprintf("SEON tester started. Target: %d successful refresh check(s).", iterations))
	updateSEONTesterTitle(iterations, 0, 0, false)
	successCount := 0
	for successCount < iterations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.runFreshSessionCheck(ctx); err != nil {
			var refreshErr sessionRefreshCheckError
			if errors.As(err, &refreshErr) {
				logger.ManagerError(fmt.Sprintf("SEON tester stopped: 1 failed, %d successful. First unsuccessful refresh: %s", successCount, err.Error()))
				if shouldSendTaskErrorWebhook(err) {
					e.sendErrorWebhook(ctx, "Session refresh check failed", err, nil)
				}
				updateSEONTesterTitle(iterations, successCount, 1, true)
				return fmt.Errorf("1 failed, %d successful: %w", successCount, err)
			}
			logger.ManagerError(fmt.Sprintf("SEON tester stopped before refresh verdict: 0 failed, %d successful. Error: %s", successCount, err.Error()))
			if shouldSendTaskErrorWebhook(err) {
				e.sendErrorWebhook(ctx, "Session check stopped before refresh verdict", err, nil)
			}
			updateSEONTesterTitle(iterations, successCount, 1, true)
			return fmt.Errorf("0 failed, %d successful, stopped before refresh verdict: %w", successCount, err)
		}
		successCount++
		updateSEONTesterTitle(iterations, successCount, 0, false)
		logger.Manager(fmt.Sprintf("Successful refresh checks: %d", successCount))
	}
	logger.Manager(fmt.Sprintf("SEON tester complete: 0 failed, %d successful.", successCount))
	updateSEONTesterTitle(iterations, successCount, 0, true)
	return nil
}

func updateSEONTesterTitle(total int, successful int, failed int, complete bool) {
	if complete {
		setConsoleTitle(fmt.Sprintf("SEON tester complete: %d/%d ok | %d failed", successful, total, failed))
		return
	}
	setConsoleTitle(fmt.Sprintf("SEON tester: %d/%d ok | %d failed", successful, total, failed))
}

func (e *Engine) ListMasters() ([]MasterAccount, error) {
	return e.store.LoadMasters()
}

func (e *Engine) RunMasterCheckout(ctx context.Context, masterID string, request MasterCheckoutRequest) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("master checkout recovered panic: %v", recovered)
		}
	}()

	masters, err := e.store.LoadMasters()
	if err != nil {
		return err
	}
	masterIndex := findMasterIndex(masters, masterID)
	if masterIndex < 0 {
		return fmt.Errorf("master %s not found", masterID)
	}
	master := masters[masterIndex]
	if master.State != MasterStateReady {
		return fmt.Errorf("master %s is %s, not Ready", master.Username, master.State)
	}
	setConsoleTitle("Master checkout: " + firstNonEmptyString(master.Username, master.Email, master.ID))

	sessions, err := e.store.LoadMasterSessions()
	if err != nil {
		return err
	}
	session, ok := findSessionForMaster(sessions, master)
	if !ok {
		return fmt.Errorf("stored session %s for master %s was not found", master.SessionID, master.Username)
	}

	result, err := e.flows.MasterCheckout(ctx, master, session, request)
	if err != nil {
		master.LastError = err.Error()
		master.UpdatedAt = e.now()
		masters[masterIndex] = master
		_ = e.store.SaveMasters(masters)
		return err
	}

	now := e.now()
	master.State = MasterStateDormant
	master.UpdatedAt = now
	master.LastError = ""
	masters[masterIndex] = master
	if err := e.store.SaveMasters(masters); err != nil {
		return err
	}
	e.mirrorMasterFriendCode(master)

	logger.Manager(fmt.Sprintf("Master %s checked out order %s and is now Dormant.", master.Username, result.Order.ID))
	setConsoleTitle("Master checkout complete: " + firstNonEmptyString(master.Username, master.Email, master.ID))
	return nil
}

func (e *Engine) runQueuedAccountTasks(ctx context.Context) error {
	window, err := NewTimeWindow(e.cfg.Engine.Timezone, e.cfg.Engine.TaskWindowStart, e.cfg.Engine.TaskWindowEnd)
	if err != nil {
		return err
	}
	now := e.now()
	if !window.Contains(now) {
		logger.Manager(fmt.Sprintf("Account task window is paused. It opens again at %s.", nextWindowStart(window, now).Format(time.RFC1123)))
		return nil
	}

	if err := e.requeueStaleTasks(); err != nil {
		return err
	}
	if err := e.enqueueAccountTasks(e.cfg.Engine.AccountsPerRun); err != nil {
		return err
	}
	return e.processTaskQueue(ctx, e.cfg.Engine.TaskConcurrency)
}

func (e *Engine) runFreshSessionCheck(ctx context.Context) error {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	normalSessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return err
	}
	masterSessions, err := e.store.LoadMasterSessions()
	if err != nil {
		return err
	}
	masters, err := e.store.LoadMasters()
	if err != nil {
		return err
	}

	now := e.now()
	logger.Manager("Session check cycle starting fresh signup.")
	signupEmail := randomSessionCheckEmail()
	logger.Manager("Session check using generated email: " + signupEmail)
	signup, referral, masterIndex, err := e.runSignupWithReferralRetry(ctx, masters, "Session check", func(referral referralSelection) (SignupResult, error) {
		return e.flows.SignupForSEONTester(ctx, SignupInput{
			FriendCode: referral.Username,
			Email:      signupEmail,
		})
	})
	if err != nil {
		return err
	}

	account := signup.Account
	session := signup.Session
	account.MasterUsernameUsed = referral.Username
	account.SessionID = session.ID
	account.State = AccountStateSignedUp
	account.IsMasterCandidate = false
	account.CreatedAt = ensureTime(account.CreatedAt, now)
	account.UpdatedAt = now
	accounts = upsertAccount(accounts, account)
	normalSessions = upsertSession(normalSessions, session)

	if masterIndex >= 0 {
		masters[masterIndex].SignupCount++
		masters[masterIndex].UpdatedAt = now
		if masters[masterIndex].SignupCount >= masters[masterIndex].SignupCap {
			masters[masterIndex].State = MasterStateAging
			masters[masterIndex].AgingStartedAt = &now
			logger.Manager(fmt.Sprintf("Master %s reached cap and moved to Aging.", masters[masterIndex].Username))
		}
	}

	if err := e.store.SaveNormalSessions(normalSessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	if err := e.store.SaveMasters(masters); err != nil {
		return err
	}
	if masterIndex >= 0 {
		e.mirrorMasterFriendCode(masters[masterIndex])
	}

	account.LastError = ""
	account.UpdatedAt = e.now()
	accounts = upsertAccount(accounts, account)
	normalSessions = upsertSession(normalSessions, session)
	if err := e.store.SaveNormalSessions(normalSessions); err != nil {
		return err
	}
	if err := e.store.SaveMasterSessions(masterSessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}

	logger.Manager(fmt.Sprintf("Session check account %s refreshed successfully.", account.Email))
	return nil
}

func (e *Engine) markSessionCheckAccountFailed(account Account, accounts []Account, cause error) error {
	now := e.now()
	account.State = AccountStateFailed
	account.LastError = cause.Error()
	account.UpdatedAt = now
	accounts = upsertAccount(accounts, account)
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	return cause
}

func randomSessionCheckEmail() string {
	firstName := lowerAlpha(gofakeit.FirstName())
	lastName := lowerAlpha(gofakeit.LastName())
	if firstName == "" {
		firstName = "test"
	}
	if lastName == "" {
		lastName = "account"
	}
	return fmt.Sprintf("%s%s%04d@gmail.com", firstName, lastName, rand.Intn(10000))
}

func lowerAlpha(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func (e *Engine) enqueueAccountTasks(count int) error {
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return err
	}
	now := e.now()
	for i := 0; i < count; i++ {
		tasks = append(tasks, Task{
			ID:          newTaskID(),
			Type:        TaskTypeAccountPipeline,
			State:       TaskStatePending,
			MaxAttempts: 3,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return e.store.SaveTasks(tasks)
}

type taskProcessor func(context.Context, Task) error

func (e *Engine) processTaskQueue(ctx context.Context, concurrency int) error {
	return e.processTaskQueueWith(ctx, concurrency, e.processAccountPipelineTaskSafely)
}

func (e *Engine) processTaskQueueWith(ctx context.Context, concurrency int, processor taskProcessor) error {
	if concurrency < 1 {
		concurrency = 1
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan Task)
	var wg sync.WaitGroup
	var terminalMu sync.Mutex
	var terminalErr error

	setTerminalErr := func(err error) {
		if !isRunStoppingError(err) {
			return
		}
		terminalMu.Lock()
		if terminalErr == nil {
			terminalErr = err
		}
		terminalMu.Unlock()
		cancel()
	}
	getTerminalErr := func() error {
		terminalMu.Lock()
		defer terminalMu.Unlock()
		return terminalErr
	}

	for i := 0; i < concurrency; i++ {
		workerIndex := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.staggerWorkerStart(workCtx, workerIndex, concurrency); err != nil {
				return
			}
			for task := range jobs {
				if workCtx.Err() != nil {
					return
				}
				if err := processor(workCtx, task); err != nil {
					setTerminalErr(err)
				}
			}
		}()
	}

	for {
		if workCtx.Err() != nil {
			close(jobs)
			wg.Wait()
			if err := getTerminalErr(); err != nil {
				return err
			}
			return ctx.Err()
		}
		task, ok, err := e.claimNextTask()
		if err != nil {
			close(jobs)
			wg.Wait()
			return err
		}
		if !ok {
			close(jobs)
			wg.Wait()
			return nil
		}
		select {
		case jobs <- task:
		case <-workCtx.Done():
			close(jobs)
			wg.Wait()
			if err := getTerminalErr(); err != nil {
				return err
			}
			return ctx.Err()
		}
	}
}

func (e *Engine) processAccountPipelineTaskSafely(ctx context.Context, task Task) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic recovered: %v", recovered)
			logger.ManagerError(fmt.Sprintf("Task %s failed: %s", task.ID, err.Error()))
			_ = e.failTask(task.ID, err)
			e.sendErrorWebhook(context.Background(), "Task panic recovered", err, e.taskErrorWebhookFields(task))
		}
	}()

	if err := e.processAccountPipelineTask(ctx, task); err != nil {
		logger.ManagerError(fmt.Sprintf("Task %s failed: %s", task.ID, err.Error()))
		if shouldSendTaskErrorWebhook(err) {
			e.sendErrorWebhook(ctx, taskErrorWebhookTitle(err), err, e.taskErrorWebhookFields(task))
		}
		return err
	}
	return nil
}

func (e *Engine) taskErrorWebhookFields(task Task) map[string]string {
	fields := map[string]string{"Task": task.ID}
	if strings.TrimSpace(task.AccountID) != "" {
		fields["Account ID"] = task.AccountID
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return fields
	}
	if index := findAccountIndex(accounts, task.AccountID); index >= 0 {
		account := accounts[index]
		fields["Email"] = account.Email
		if strings.TrimSpace(account.Username) != "" {
			fields["Username"] = account.Username
		}
	}
	return fields
}

func (e *Engine) persistTaskRewardDiscovery(ctx context.Context, progress SignupResult, referral referralSelection) error {
	account := progress.Account
	session := progress.Session
	now := e.now()
	account.MasterUsernameUsed = referral.Username
	account.SessionID = session.ID
	account.State = AccountStateSignedUp
	account.CreatedAt = ensureTime(account.CreatedAt, now)
	account.UpdatedAt = now
	initializeRewardCreditBaseline(&session)

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
		if existingSession, ok := findSessionForAccount(sessions, account); ok {
			mergeExistingPreparedSessionMetadata(&session, existingSession)
		}
		accounts = upsertAccount(accounts, account)
		sessions = upsertSession(sessions, session)
		if err := e.store.SaveAccounts(accounts); err != nil {
			return err
		}
		if err := e.store.SaveNormalSessions(sessions); err != nil {
			return err
		}
		return nil
	}(); err != nil {
		return err
	}

	if e.sendHighRewardWebhookIfNeeded(ctx, account, &session) {
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

func (e *Engine) processAccountPipelineTask(ctx context.Context, task Task) error {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return err
	}
	normalSessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return err
	}
	masterSessions, err := e.store.LoadMasterSessions()
	if err != nil {
		return err
	}
	masters, err := e.store.LoadMasters()
	if err != nil {
		return err
	}

	now := e.now()
	var account Account
	var session SessionRecord

	if task.AccountID == "" {
		signup, referral, masterIndex, err := e.runSignupWithReferralRetry(ctx, masters, "Task signup", func(referral referralSelection) (SignupResult, error) {
			return e.flows.Signup(ctx, SignupInput{
				FriendCode: referral.Username,
				RewardDetected: func(progress SignupResult) error {
					return e.persistTaskRewardDiscovery(ctx, progress, referral)
				},
			})
		})
		if err != nil {
			return e.failTask(task.ID, err)
		}

		account = signup.Account
		session = signup.Session
		account.MasterUsernameUsed = referral.Username
		account.SessionID = session.ID
		account.State = AccountStateSignedUp
		account.CreatedAt = ensureTime(account.CreatedAt, now)
		account.UpdatedAt = now
		initializeRewardCreditBaseline(&session)
		if refreshedSessions, refreshErr := e.store.LoadNormalSessions(); refreshErr == nil {
			normalSessions = refreshedSessions
			if existingSession, ok := findSessionForAccount(normalSessions, account); ok {
				mergeExistingPreparedSessionMetadata(&session, existingSession)
			}
		}

		isCandidate := rand.Float64()*100 < e.cfg.Engine.MasterCandidatePercentage
		account.IsMasterCandidate = isCandidate

		accounts = upsertAccount(accounts, account)
		normalSessions = upsertSession(normalSessions, session)

		if isCandidate {
			masterAccount := e.newMasterCandidate(account, session.ID, now)
			masters = upsertMaster(masters, masterAccount)
			masterSessions = upsertSession(masterSessions, session)
			logger.Manager(fmt.Sprintf("Account %s stored as Master candidate.", account.Username))
		}

		if masterIndex >= 0 {
			masters[masterIndex].SignupCount++
			masters[masterIndex].UpdatedAt = now
			if masters[masterIndex].SignupCount >= masters[masterIndex].SignupCap {
				masters[masterIndex].State = MasterStateAging
				masters[masterIndex].AgingStartedAt = &now
				logger.Manager(fmt.Sprintf("Master %s reached cap and moved to Aging.", masters[masterIndex].Username))
			}
		}

		if err := e.savePipelineState(accounts, orders, normalSessions, masterSessions, masters); err != nil {
			return err
		}
		if masterIndex >= 0 {
			e.mirrorMasterFriendCode(masters[masterIndex])
		}
		if err := e.updateTask(task.ID, func(t *Task) {
			t.AccountID = account.ID
			t.MasterID = referral.MasterID
		}); err != nil {
			return err
		}
	} else {
		accountIndex := findAccountIndex(accounts, task.AccountID)
		if accountIndex < 0 {
			return e.failTask(task.ID, fmt.Errorf("account %s not found for queued task", task.AccountID))
		}
		account = accounts[accountIndex]
		var ok bool
		session, ok = findSessionForAccount(normalSessions, account)
		if !ok {
			return e.failTask(task.ID, fmt.Errorf("session %s not found for queued task", account.SessionID))
		}
		if session.ID != "" && account.SessionID != session.ID {
			account.SessionID = session.ID
			account.UpdatedAt = now
			accounts[accountIndex] = account
			if err := e.store.SaveAccounts(accounts); err != nil {
				return err
			}
		}
	}

	if task.OrderID != "" {
		orderIDs := taskOrderIDs(task)
		if len(orderIDs) == 0 {
			orderIDs = []string{task.OrderID}
		}
		orderIndex := findOrderIndex(orders, orderIDs[0])
		if orderIndex < 0 {
			return e.failTask(task.ID, fmt.Errorf("order %s not found for queued task", task.OrderID))
		}
		if !task.LabelWritten {
			if err := appendCheckoutLabel(e.cfg.Engine.CheckoutLabelCSVPath, account, orders[orderIndex], account.FirstName, account.LastName, e.now()); err != nil {
				return err
			}
			if err := appendOrderExport(account, orders[orderIndex], account.FirstName, account.LastName, e.now()); err != nil {
				return err
			}
			if err := e.updateTask(task.ID, func(t *Task) {
				t.LabelWritten = true
			}); err != nil {
				return err
			}
		}
		for _, orderID := range orderIDs {
			index := findOrderIndex(orders, orderID)
			if index < 0 {
				continue
			}
			e.sendCheckoutWebhookIfNeeded(ctx, account, &orders[index])
		}
		if err := e.store.SaveOrders(orders); err != nil {
			return err
		}
		return e.completeTask(task.ID)
	}

	rewardAmount := rewardAmountSafeFromSession(session)
	logger.Manager(fmt.Sprintf("Detected reward for %s: %s.", account.Email, rewardAmountLabel(rewardAmount)))
	if e.sendHighRewardWebhookIfNeeded(ctx, account, &session) {
		normalSessions = upsertSession(normalSessions, session)
		if account.IsMasterCandidate {
			masterSessions = upsertSession(masterSessions, session)
		}
		if err := e.store.SaveNormalSessions(normalSessions); err != nil {
			return err
		}
		if account.IsMasterCandidate {
			if err := e.store.SaveMasterSessions(masterSessions); err != nil {
				return err
			}
		}
	}
	forceWithoutBalance := false
	switch {
	case rewardAmount >= 20000:
		logger.Manager("Checkout decision: reward is £200; continuing checkout.")
	case rewardAmount >= 2000:
		logger.Manager("Checkout decision: reward is £20; continuing checkout.")
	case rewardAmount >= 1000:
		if e.cfg.CheckoutChance <= 0 {
			logger.Manager("Checkout decision: reward is £10 and checkout chance is 0%; skipping checkout.")
			now = e.now()
			account.State = AccountStateSignedUp
			account.LastError = ""
			account.UpdatedAt = now
			accounts = upsertAccount(accounts, account)
			normalSessions = upsertSession(normalSessions, session)
			if account.IsMasterCandidate {
				masterSessions = upsertSession(masterSessions, session)
			}
			if err := e.savePipelineState(accounts, orders, normalSessions, masterSessions, masters); err != nil {
				return err
			}
			return e.completeTask(task.ID)
		}
		logger.Manager(fmt.Sprintf("Checkout decision: reward is £10; checkout flow will apply configured %d%% chance.", e.cfg.CheckoutChance))
	default:
		logger.Manager("Checkout decision: no reward balance detected; forcing checkout anyway.")
		forceWithoutBalance = true
	}

	checkout, err := e.flows.Checkout(ctx, CheckoutInput{
		Account:                  account,
		Session:                  session,
		ForceCheckout:            forceWithoutBalance,
		AllowInsufficientBalance: forceWithoutBalance,
		OrderPlaced: func(progress CheckoutResult) {
			orders = e.recordImmediateCheckoutOrders(ctx, account, orders, progress)
		},
	})
	if err != nil {
		if errors.Is(err, ErrCheckoutSkipped) {
			now = e.now()
			if checkout.Session.ID != "" {
				session = checkout.Session
				session.ID = account.SessionID
				session.AccountID = account.ID
				session.CreatedAt = ensureTime(session.CreatedAt, now)
				session.UpdatedAt = now
				normalSessions = upsertSession(normalSessions, session)
				if account.IsMasterCandidate {
					masterSessions = upsertSession(masterSessions, session)
				}
			}
			account.State = AccountStateSignedUp
			account.LastError = ""
			account.UpdatedAt = now
			accounts = upsertAccount(accounts, account)
			if err := e.savePipelineState(accounts, orders, normalSessions, masterSessions, masters); err != nil {
				return err
			}
			return e.completeTask(task.ID)
		}
		if errors.Is(err, ErrNoBalance) {
			if checkout.Session.ID != "" {
				session = checkout.Session
				session.ID = account.SessionID
				session.AccountID = account.ID
				session.CreatedAt = ensureTime(session.CreatedAt, now)
				session.UpdatedAt = now
				normalSessions = upsertSession(normalSessions, session)
				if account.IsMasterCandidate {
					masterSessions = upsertSession(masterSessions, session)
				}
			}
			account.State = AccountStateSignedUp
			account.LastError = err.Error()
			account.UpdatedAt = now
			accounts = upsertAccount(accounts, account)
			if saveErr := e.savePipelineState(accounts, orders, normalSessions, masterSessions, masters); saveErr != nil {
				return saveErr
			}
			return e.failTask(task.ID, err)
		}
		account.State = AccountStateFailed
		account.LastError = err.Error()
		account.UpdatedAt = now
		accounts = upsertAccount(accounts, account)
		_ = e.store.SaveAccounts(accounts)
		return e.failTask(task.ID, err)
	}

	if checkout.Session.ID != "" {
		session = checkout.Session
		session.ID = account.SessionID
		session.AccountID = account.ID
		session.CreatedAt = ensureTime(session.CreatedAt, now)
		session.UpdatedAt = now
		normalSessions = upsertSession(normalSessions, session)
		if account.IsMasterCandidate {
			masterSessions = upsertSession(masterSessions, session)
		}
	}

	checkoutOrders := checkout.Orders
	if len(checkoutOrders) == 0 {
		checkoutOrders = []Order{checkout.Order}
	}
	for index := range checkoutOrders {
		checkoutOrders[index].AccountID = account.ID
		checkoutOrders[index].State = OrderStateCreated
		checkoutOrders[index].CreatedAt = ensureTime(checkoutOrders[index].CreatedAt, now)
		checkoutOrders[index].UpdatedAt = now
		orders = upsertOrder(orders, checkoutOrders[index])
	}
	order := checkoutOrders[0]
	account.State = AccountStateCheckedOut
	account.OrderID = order.ID
	account.UpdatedAt = now

	accounts = upsertAccount(accounts, account)

	if err := e.savePipelineState(accounts, orders, normalSessions, masterSessions, masters); err != nil {
		return err
	}
	if err := e.updateTask(task.ID, func(t *Task) {
		t.OrderID = order.ID
		orderIDs := make([]string, 0, len(checkoutOrders))
		for _, checkoutOrder := range checkoutOrders {
			orderIDs = append(orderIDs, checkoutOrder.ID)
		}
		if t.Metadata == nil {
			t.Metadata = map[string]any{}
		}
		t.Metadata["order_ids"] = orderIDs
	}); err != nil {
		return err
	}
	for _, checkoutOrder := range checkoutOrders {
		orderIndex := findOrderIndex(orders, checkoutOrder.ID)
		if orderIndex < 0 {
			continue
		}
		e.sendCheckoutWebhookIfNeeded(ctx, account, &orders[orderIndex])
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	if err := appendCheckoutLabel(e.cfg.Engine.CheckoutLabelCSVPath, account, order, checkout.ShippingFirstName, checkout.ShippingLastName, e.now()); err != nil {
		return err
	}
	for _, checkoutOrder := range checkoutOrders {
		if err := appendOrderExport(account, checkoutOrder, checkout.ShippingFirstName, checkout.ShippingLastName, e.now()); err != nil {
			return err
		}
	}
	if err := e.sleepDelay(ctx, "after_order"); err != nil {
		return err
	}

	return e.updateTask(task.ID, func(t *Task) {
		t.LabelWritten = true
		t.State = TaskStateCompleted
		completed := e.now()
		t.CompletedAt = &completed
		t.UpdatedAt = completed
		t.LastError = ""
	})
}

func (e *Engine) processOrdersIfDue(ctx context.Context) error {
	window, err := NewTimeWindow(e.cfg.Engine.Timezone, e.cfg.Engine.OrderCheckWindowStart, e.cfg.Engine.OrderCheckWindowEnd)
	if err != nil {
		return err
	}
	now := e.now()
	runtimeState, err := e.store.LoadRuntimeState()
	if err != nil {
		return err
	}

	if runtimeState.NextOrderCheckAt == nil || now.After(runtimeState.NextOrderCheckAt.Add(24*time.Hour)) {
		next := window.NextRandomRun(now)
		runtimeState.NextOrderCheckAt = &next
		runtimeState.UpdatedAt = e.now()
		if err := e.store.SaveRuntimeState(runtimeState); err != nil {
			return err
		}
	}

	nextLocal := runtimeState.NextOrderCheckAt.In(window.Location)
	today := now.In(window.Location).Format("2006-01-02")
	if runtimeState.LastOrderCheckDay == today {
		logger.Manager(fmt.Sprintf("Order checking already ran for %s. Next check is %s.", today, nextLocal.Format(time.RFC1123)))
		return nil
	}
	if now.Before(*runtimeState.NextOrderCheckAt) {
		logger.Manager(fmt.Sprintf("Next randomized order check is scheduled for %s.", nextLocal.Format(time.RFC1123)))
		return nil
	}
	if !window.Contains(now) {
		next := window.NextRandomRun(now)
		runtimeState.NextOrderCheckAt = &next
		runtimeState.UpdatedAt = e.now()
		if err := e.store.SaveRuntimeState(runtimeState); err != nil {
			return err
		}
		logger.Manager(fmt.Sprintf("Order checking is outside the configured window. Next randomized check is %s.", next.In(window.Location).Format(time.RFC1123)))
		return nil
	}

	if err := e.processOrders(ctx); err != nil {
		return err
	}
	runtimeState.LastOrderCheckDay = today
	nextSeed := time.Date(now.In(window.Location).Year(), now.In(window.Location).Month(), now.In(window.Location).Day()+1, 0, 0, 0, 0, window.Location)
	next := window.NextRandomRun(nextSeed)
	runtimeState.NextOrderCheckAt = &next
	runtimeState.UpdatedAt = e.now()
	return e.store.SaveRuntimeState(runtimeState)
}

func (e *Engine) processOrders(ctx context.Context) error {
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return err
	}
	orders, err := e.store.LoadOrders()
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

	discovered, err := e.discoverAccountOrders(ctx, accounts, orders, sessions)
	if err != nil {
		logger.ManagerError("Order discovery had errors: " + err.Error())
		e.sendErrorWebhook(ctx, "Order discovery error", err, nil)
	} else {
		orders = discovered.orders
	}
	sessions = discovered.sessions

	now := e.now()
	for i := range orders {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		order := orders[i]
		accountIndex := findAccountIndex(accounts, order.AccountID)
		if accountIndex < 0 || accounts[accountIndex].State == AccountStateComplete {
			continue
		}
		if !orders[i].CheckoutWebhookSent {
			e.sendCheckoutWebhookIfNeeded(ctx, accounts[accountIndex], &orders[i])
		}
		if order.ReviewCompleted || order.State == OrderStateReviewed {
			continue
		}
		session, ok := findSessionForAccount(sessions, accounts[accountIndex])
		if !ok {
			orders[i].LastError = "normal account session not found"
			orders[i].UpdatedAt = now
			continue
		}
		if session.ID != "" && accounts[accountIndex].SessionID != session.ID {
			accounts[accountIndex].SessionID = session.ID
			accounts[accountIndex].UpdatedAt = now
		}

		check, ok := discovered.results[orderStatusKey(order.AccountID, order.ExternalOrderID, order.ID)]
		if !ok {
			check, err = e.flows.CheckOrder(ctx, OrderCheckInput{
				Account: accounts[accountIndex],
				Order:   order,
				Session: session,
			})
		}
		orders[i].LastCheckedAt = &now
		orders[i].UpdatedAt = now
		if err != nil {
			orders[i].LastError = err.Error()
			continue
		}
		orders[i].LastError = ""

		if strings.TrimSpace(check.ExternalOrderID) != "" {
			orders[i].ExternalOrderID = strings.TrimSpace(check.ExternalOrderID)
		}
		if strings.TrimSpace(check.ReviewReferenceID) != "" {
			orders[i].ReviewReferenceID = strings.TrimSpace(check.ReviewReferenceID)
		}
		if len(check.Metadata) > 0 {
			if orders[i].Metadata == nil {
				orders[i].Metadata = map[string]any{}
			}
			for key, value := range check.Metadata {
				orders[i].Metadata[key] = value
			}
		}
		if strings.TrimSpace(check.TrackingNumber) != "" && !orders[i].TrackingWebhookSent {
			orders[i].TrackingNumber = strings.TrimSpace(check.TrackingNumber)
			if err := e.sendTrackingWebhookImmediately(ctx, accounts[accountIndex], &orders[i]); err != nil {
				logger.ManagerError("tracking webhook handling failed: " + err.Error())
			}
		}

		if !check.ReadyForReview {
			orders[i].State = OrderStateInProgress
			if err := appendOrderExport(accounts[accountIndex], orders[i], accounts[accountIndex].FirstName, accounts[accountIndex].LastName, e.now()); err != nil {
				logger.ManagerError("orders.csv export failed: " + err.Error())
			}
			continue
		}

		orders[i].State = OrderStateReviewReady
		orders[i].UpdatedAt = now
		if err := e.queuePendingReview(ctx, accounts[accountIndex], &orders[i]); err != nil {
			orders[i].LastError = err.Error()
			logger.ManagerError("Pending review queue failed: " + err.Error())
			continue
		}
		if err := appendOrderExport(accounts[accountIndex], orders[i], accounts[accountIndex].FirstName, accounts[accountIndex].LastName, e.now()); err != nil {
			logger.ManagerError("orders.csv export failed: " + err.Error())
		}
	}

	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	if err := e.store.SaveMasters(masters); err != nil {
		return err
	}
	e.mirrorMasterFriendCodes(masters)
	return nil
}

func (e *Engine) processMasters(ctx context.Context) error {
	masters, err := e.store.LoadMasters()
	if err != nil {
		return err
	}
	sessions, err := e.store.LoadMasterSessions()
	if err != nil {
		return err
	}

	now := e.now()
	for i := range masters {
		if masters[i].State == MasterStateActive && masters[i].SignupCount >= masters[i].SignupCap {
			masters[i].State = MasterStateAging
			started := now
			masters[i].AgingStartedAt = &started
			masters[i].UpdatedAt = now
		}

		switch masters[i].State {
		case MasterStateHealthyPending:
			if masters[i].CandidateCompletedAt == nil {
				continue
			}
			if now.Sub(*masters[i].CandidateCompletedAt) < 24*time.Hour {
				continue
			}
			if masters[i].LastHealthCheckAt != nil && now.Sub(*masters[i].LastHealthCheckAt) < e.cfg.Engine.MasterHealthCheckInterval.Duration {
				continue
			}
			session, ok := findSessionForMaster(sessions, masters[i])
			if !ok {
				masters[i].State = MasterStateFailed
				masters[i].LastError = "master session not found"
				masters[i].UpdatedAt = now
				continue
			}
			refreshed, healthy, err := e.flows.CheckMasterHealth(ctx, masters[i], session)
			masters[i].LastHealthCheckAt = &now
			masters[i].UpdatedAt = now
			if err != nil {
				masters[i].LastError = err.Error()
				continue
			}
			if !healthy {
				masters[i].State = MasterStateFailed
				masters[i].LastError = "master health check returned unhealthy"
				continue
			}
			masters[i].State = MasterStateActive
			masters[i].HealthySince = &now
			masters[i].LastError = ""
			sessions = upsertSession(sessions, refreshed)
			logger.Manager(fmt.Sprintf("Master %s is Active.", masters[i].Username))

		case MasterStateActive:
			if masters[i].LastHealthCheckAt != nil && now.Sub(*masters[i].LastHealthCheckAt) < e.cfg.Engine.MasterHealthCheckInterval.Duration {
				continue
			}
			session, ok := findSessionForMaster(sessions, masters[i])
			if !ok {
				masters[i].State = MasterStateFailed
				masters[i].LastError = "master session not found"
				masters[i].UpdatedAt = now
				continue
			}
			refreshed, healthy, err := e.flows.CheckMasterHealth(ctx, masters[i], session)
			masters[i].LastHealthCheckAt = &now
			masters[i].UpdatedAt = now
			if err != nil {
				masters[i].LastError = err.Error()
				continue
			}
			if !healthy {
				masters[i].State = MasterStateFailed
				masters[i].LastError = "master health check returned unhealthy"
				continue
			}
			masters[i].LastError = ""
			sessions = upsertSession(sessions, refreshed)

		case MasterStateAging:
			if masters[i].AgingStartedAt == nil {
				started := now
				masters[i].AgingStartedAt = &started
				masters[i].UpdatedAt = now
				continue
			}
			if now.Sub(*masters[i].AgingStartedAt) >= time.Duration(e.cfg.Engine.MasterAgingDurationHours)*time.Hour {
				masters[i].State = MasterStateReady
				masters[i].UpdatedAt = now
				if !masters[i].ReadyWebhookSent {
					if err := e.webhook.SendMasterReady(ctx, masters[i]); err == nil {
						masters[i].ReadyWebhookSent = true
					} else {
						e.sendErrorWebhook(ctx, "Master ready webhook failed", err, map[string]string{
							"Master": masters[i].Username,
						})
					}
				}
				logger.Manager(fmt.Sprintf("Master %s moved to Ready.", masters[i].Username))
			}
		}
	}

	if err := e.store.SaveMasterSessions(sessions); err != nil {
		return err
	}
	if err := e.store.SaveMasters(masters); err != nil {
		return err
	}
	e.mirrorMasterFriendCodes(masters)
	return nil
}

type referralSelection struct {
	Username string
	MasterID string
	Source   string
}

func (e *Engine) selectReferralCode(masters []MasterAccount) (referralSelection, int, error) {
	return e.selectReferralCodeExcluding(masters, nil)
}

func (e *Engine) selectReferralCodeExcluding(masters []MasterAccount, excluded map[string]bool) (referralSelection, int, error) {
	for i, master := range masters {
		username := strings.TrimSpace(master.Username)
		if excluded != nil && excluded[strings.ToLower(username)] {
			continue
		}
		if master.State == MasterStateActive && master.SignupCount < master.SignupCap && !friendCodeExplicitlyUnusable(e.cfg.Engine.FriendCodesPath, username) {
			return referralSelection{Username: master.Username, MasterID: master.ID}, i, nil
		}
	}

	code, err := claimFriendCodeExcluding(e.cfg.Engine.FriendCodesPath, excluded)
	if err != nil {
		return referralSelection{}, -1, err
	}
	logger.ManagerNotice("Using setup friend-code fallback because no Active master is available yet.")
	return referralSelection{Username: code}, -1, nil
}

func (e *Engine) newMasterCandidate(account Account, sessionID string, now time.Time) MasterAccount {
	return MasterAccount{
		ID:          "master_" + account.ID,
		AccountID:   account.ID,
		Username:    account.Username,
		Email:       account.Email,
		State:       MasterStateCandidate,
		SignupCap:   randomizedCap(e.cfg.Engine.MasterBaseSignupCap, e.cfg.Engine.MasterCapRandomizationPct),
		SessionID:   sessionID,
		CreatedAt:   now,
		UpdatedAt:   now,
		SignupCount: 0,
	}
}

func (e *Engine) mirrorMasterFriendCodes(masters []MasterAccount) {
	for _, master := range masters {
		e.mirrorMasterFriendCode(master)
	}
}

func (e *Engine) mirrorMasterFriendCode(master MasterAccount) {
	if err := syncMasterFriendCode(e.cfg.Engine.FriendCodesPath, master); err != nil {
		logger.ManagerError(fmt.Sprintf("Failed to update friend_codes.csv for master %s: %s", master.Username, err.Error()))
	}
}

func randomizedCap(base int, movementPercent float64) int {
	if base <= 0 {
		return 1
	}
	movement := int(math.Round(float64(base) * movementPercent / 100))
	minCap := base - movement
	maxCap := base + movement
	if minCap < 1 {
		minCap = 1
	}
	if maxCap < minCap {
		maxCap = minCap
	}
	return minCap + rand.Intn(maxCap-minCap+1)
}

func ensureTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func trackingWebhookMessage(account Account, order Order, metadata map[string]any) string {
	parts := []string{
		"Tracking available",
		"Account: " + account.Email,
		"Order: " + firstMetadataString(metadata, "display_id", order.ExternalOrderID, order.ID),
		"Tracking: " + order.TrackingNumber,
	}
	if value := metadataValue(metadata, "tracking_url"); value != "" {
		parts = append(parts, "URL: "+value)
	}
	if value := metadataValue(metadata, "shipping_service"); value != "" {
		parts = append(parts, "Service: "+value)
	}
	if value := metadataValue(metadata, "items"); value != "" {
		parts = append(parts, "Items: "+value)
	}
	if value := metadataValue(metadata, "total"); value != "" {
		parts = append(parts, "Total: "+formatMoneyLine(value))
	}
	return strings.Join(parts, "\n")
}

func metadataValue(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, exists := metadata[key]
	if !exists || value == nil {
		return ""
	}
	switch value := metadata[key].(type) {
	case string:
		return cleanMetadataString(value)
	case []string:
		return strings.Join(value, " | ")
	case []any:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if item == nil {
				continue
			}
			if text := cleanMetadataString(fmt.Sprint(item)); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " | ")
	default:
		return cleanMetadataString(fmt.Sprint(value))
	}
}

func cleanMetadataString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "<nil>") || strings.EqualFold(value, "null") {
		return ""
	}
	return value
}

func firstMetadataString(metadata map[string]any, key string, fallback ...string) string {
	if value := metadataValue(metadata, key); value != "" {
		return value
	}
	for _, value := range fallback {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatMoneyLine(value string) string {
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return value
	}
	amount, err := strconv.Atoi(fields[0])
	if err != nil {
		return value
	}
	return fmt.Sprintf("%.2f %s", float64(amount)/100, fields[1])
}

func rewardAmountSafeFromSession(session SessionRecord) int {
	if amount, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe"); ok && amount > 0 {
		return amount
	}
	if amount, ok := metadataIntValue(session.Metadata, "referral_amount"); ok && amount > 0 {
		return amount
	}
	return 0
}

func checkoutBalanceAmountSafeFromSession(session SessionRecord) int {
	if amount, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe"); ok && amount > 0 {
		return amount
	}
	return rewardAmountSafeFromSession(session)
}

func provenCheckoutBalanceAmountSafeFromSession(session SessionRecord) int {
	amount, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe")
	if !ok || amount <= 0 {
		return 0
	}
	return amount
}

func checkoutRewardLabelForBalance(amount int) string {
	if amount < 1000 {
		return ""
	}
	return rewardLabel(strconv.Itoa(amount))
}

func rewardAmountLabel(amount int) string {
	if amount <= 0 {
		return "none"
	}
	if amount%100 == 0 {
		return fmt.Sprintf("\u00a3%d (%d GBP)", amount/100, amount)
	}
	return fmt.Sprintf("\u00a3%.2f (%d GBP)", float64(amount)/100, amount)
}
