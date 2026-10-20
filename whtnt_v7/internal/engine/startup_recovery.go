package engine

import (
	"context"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

func (e *Engine) recoverStartupState(ctx context.Context) error {
	abandoned, err := e.countAbandonedRunningAccountTasks()
	if err != nil {
		return err
	}
	if abandoned > 0 && !e.hasContinueUnfinishedApproval() && !e.hasStartFreshApproval() {
		return UnfinishedSessionsError{Count: abandoned}
	}
	if e.hasStartFreshApproval() {
		return nil
	}
	e.startupRecoveryOnce.Do(func() {
		e.startupRecoveryErr = e.recoverStartupStateOnce(ctx)
	})
	return e.startupRecoveryErr
}

func (e *Engine) recoverStartupStateOnce(ctx context.Context) error {
	reconcileAccountIDs, err := e.requeueRunningTasksOnStartup()
	if err != nil {
		return err
	}
	if err := e.reconcileLiveOrdersOnStartup(ctx, reconcileAccountIDs); err != nil {
		return err
	}
	return nil
}

func (e *Engine) requeueRunningTasksOnStartup() ([]string, error) {
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return nil, err
	}
	now := e.now()
	changed := false
	reconcileAccountIDs := []string{}
	for i := range tasks {
		if tasks[i].State != TaskStateRunning {
			continue
		}
		if tasks[i].AccountID != "" && tasks[i].OrderID == "" {
			reconcileAccountIDs = appendUniqueString(reconcileAccountIDs, tasks[i].AccountID)
		}
		tasks[i].State = TaskStatePending
		tasks[i].StartedAt = nil
		tasks[i].UpdatedAt = now
		tasks[i].LastError = "requeued after startup recovery"
		changed = true
	}
	if !changed {
		return nil, nil
	}
	logger.Manager("Startup recovery requeued abandoned running tasks.")
	return reconcileAccountIDs, e.store.SaveTasks(tasks)
}

func (e *Engine) reconcileLiveOrdersOnStartup(ctx context.Context, reconcileAccountIDs []string) error {
	if len(reconcileAccountIDs) == 0 {
		return nil
	}
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

	reconcileSet := make(map[string]bool, len(reconcileAccountIDs))
	for _, accountID := range reconcileAccountIDs {
		reconcileSet[accountID] = true
	}

	targetAccounts := make([]Account, 0, len(reconcileAccountIDs))
	for _, account := range accounts {
		if !reconcileSet[account.ID] || account.State == AccountStateComplete {
			continue
		}
		if accountHasStoredOrder(orders, account.ID) {
			continue
		}
		if _, ok := findSessionForAccount(sessions, account); ok {
			targetAccounts = append(targetAccounts, account)
		}
	}
	if len(targetAccounts) == 0 {
		return nil
	}

	logger.Manager("Startup recovery checking live orders only for abandoned checkout tasks...")
	discovered, err := e.discoverAccountOrders(ctx, targetAccounts, orders, sessions)
	if err != nil {
		logger.ManagerError("Startup order reconciliation had errors: " + err.Error())
	}
	orders = discovered.orders
	sessions = discovered.sessions
	accounts = markAccountsCheckedOutFromOrders(accounts, orders, e.now())
	e.sendPendingCheckoutWebhooks(ctx, accounts, orders)

	if saveErr := e.store.SaveNormalSessions(sessions); saveErr != nil {
		return saveErr
	}
	if saveErr := e.store.SaveOrders(orders); saveErr != nil {
		return saveErr
	}
	if saveErr := e.store.SaveAccounts(accounts); saveErr != nil {
		return saveErr
	}
	return err
}

func accountHasStoredOrder(orders []Order, accountID string) bool {
	for _, order := range orders {
		if order.AccountID == accountID {
			return true
		}
	}
	return false
}

func (e *Engine) sendPendingCheckoutWebhooks(ctx context.Context, accounts []Account, orders []Order) {
	for i := range orders {
		if orders[i].CheckoutWebhookSent {
			continue
		}
		accountIndex := findAccountIndex(accounts, orders[i].AccountID)
		if accountIndex < 0 {
			continue
		}
		e.sendCheckoutWebhookIfNeeded(ctx, accounts[accountIndex], &orders[i])
	}
}

func markAccountsCheckedOutFromOrders(accounts []Account, orders []Order, now time.Time) []Account {
	for i := range accounts {
		if accounts[i].State == AccountStateComplete {
			continue
		}
		for _, order := range orders {
			if order.AccountID != accounts[i].ID {
				continue
			}
			if accounts[i].State == AccountStateSignedUp || accounts[i].State == AccountStateNew || accounts[i].State == AccountStateFailed {
				accounts[i].State = AccountStateCheckedOut
			}
			if accounts[i].OrderID == "" {
				accounts[i].OrderID = order.ID
			}
			accounts[i].UpdatedAt = now
			break
		}
	}
	return accounts
}
