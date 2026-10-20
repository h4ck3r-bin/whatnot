package engine

import "fmt"

type UnfinishedSessionsError struct {
	Count int
}

func (e UnfinishedSessionsError) Error() string {
	return fmt.Sprintf("%d sessions unfinished - continue from where they left off?", e.Count)
}

func (e *Engine) ApproveContinueUnfinishedSessions() {
	e.unfinishedMu.Lock()
	defer e.unfinishedMu.Unlock()
	e.continueUnfinishedApproved = true
	e.startFreshApproved = false
}

func (e *Engine) ApproveStartFreshWithoutUnfinishedSessions() {
	e.unfinishedMu.Lock()
	defer e.unfinishedMu.Unlock()
	e.startFreshApproved = true
	e.continueUnfinishedApproved = false
}

func (e *Engine) hasContinueUnfinishedApproval() bool {
	e.unfinishedMu.Lock()
	defer e.unfinishedMu.Unlock()
	return e.continueUnfinishedApproved
}

func (e *Engine) hasStartFreshApproval() bool {
	e.unfinishedMu.Lock()
	defer e.unfinishedMu.Unlock()
	return e.startFreshApproved
}

func (e *Engine) clearUnfinishedRunApprovals() {
	e.unfinishedMu.Lock()
	defer e.unfinishedMu.Unlock()
	e.continueUnfinishedApproved = false
	e.startFreshApproved = false
}

func (e *Engine) CountUnfinishedSessions() (int, error) {
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return 0, err
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return 0, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return 0, err
	}
	orders, err := e.store.LoadOrders()
	if err != nil {
		return 0, err
	}

	seen := map[string]bool{}
	anonymous := 0
	for _, task := range tasks {
		if task.Type != TaskTypeAccountPipeline {
			continue
		}
		if task.State != TaskStatePending && task.State != TaskStateRunning {
			continue
		}
		if task.AccountID == "" {
			if task.State == TaskStateRunning {
				anonymous++
			}
			continue
		}
		seen[task.AccountID] = true
	}

	for _, account := range accounts {
		switch account.State {
		case AccountStateSignedUp:
		case AccountStateCheckedOut:
			if accountHasStoredOrder(orders, account.ID) {
				continue
			}
		default:
			continue
		}
		if _, ok := findSessionForAccount(sessions, account); !ok {
			continue
		}
		key := account.ID
		if key == "" {
			key = account.SessionID
		}
		if key == "" {
			key = account.Email
		}
		if key != "" {
			seen[key] = true
		}
	}

	return len(seen) + anonymous, nil
}

func (e *Engine) countAbandonedRunningAccountTasks() (int, error) {
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	anonymous := 0
	for _, task := range tasks {
		if task.Type != TaskTypeAccountPipeline || task.State != TaskStateRunning {
			continue
		}
		if task.AccountID == "" {
			anonymous++
			continue
		}
		seen[task.AccountID] = true
	}
	return len(seen) + anonymous, nil
}
