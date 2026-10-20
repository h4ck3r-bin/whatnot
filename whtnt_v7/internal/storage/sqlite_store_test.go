package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/engine"
)

func testSQLiteStorageConfig(t *testing.T) appconfig.StorageConfig {
	t.Helper()
	dir := t.TempDir()
	return appconfig.StorageConfig{
		DatabasePath:       dir + "/state.sqlite",
		AccountsPath:       dir + "/accounts.json",
		OrdersPath:         dir + "/orders.json",
		NormalSessionsPath: dir + "/normal_sessions.json",
		MasterSessionsPath: dir + "/master_sessions.json",
		MastersPath:        dir + "/masters.json",
		RuntimePath:        dir + "/runtime.json",
		TaskQueuePath:      dir + "/task_queue.json",
		PendingReviewsPath: dir + "/pending_reviews.json",
		DiscoveryCachePath: dir + "/account_discovery_snapshots.json",
		ErrorDedupePath:    dir + "/error_webhook_dedupe.json",
	}
}

func TestSQLiteStoreImportsExistingFiles(t *testing.T) {
	paths := testSQLiteStorageConfig(t)
	account := engine.Account{ID: "acct_1", Email: "one@example.com", Username: "one", SessionID: "sess_1", State: engine.AccountStateSignedUp}
	session := engine.SessionRecord{ID: "sess_1", AccountID: "acct_1", Metadata: map[string]any{"email": "one@example.com"}, UpdatedAt: time.Now()}
	order := engine.Order{ID: "order_1", AccountID: "acct_1", State: engine.OrderStateCreated}
	task := engine.Task{ID: "task_1", Type: engine.TaskTypeAccountPipeline, State: engine.TaskStatePending}
	pendingReview := engine.PendingReviewItem{ID: "review_1", AccountID: "acct_1", OrderID: "order_1", Status: "pending"}
	if err := saveSlice(paths.AccountsPath, []engine.Account{account}); err != nil {
		t.Fatal(err)
	}
	if err := saveSlice(paths.OrdersPath, []engine.Order{order}); err != nil {
		t.Fatal(err)
	}
	if err := saveSessions(paths.NormalSessionsPath, []engine.SessionRecord{session}); err != nil {
		t.Fatal(err)
	}
	if err := saveSessions(paths.MasterSessionsPath, nil); err != nil {
		t.Fatal(err)
	}
	if err := saveSlice(paths.MastersPath, []engine.MasterAccount{}); err != nil {
		t.Fatal(err)
	}
	if err := saveSlice(paths.TaskQueuePath, []engine.Task{task}); err != nil {
		t.Fatal(err)
	}
	if err := saveSlice(paths.PendingReviewsPath, []engine.PendingReviewItem{pendingReview}); err != nil {
		t.Fatal(err)
	}
	if err := saveObject(paths.DiscoveryCachePath, map[string]any{"acct_1": map[string]any{"orders": map[string]any{"order_1": "seen"}}}); err != nil {
		t.Fatal(err)
	}
	if err := saveObject(paths.RuntimePath, engine.RuntimeState{LastOrderCheckDay: "2026-07-07"}); err != nil {
		t.Fatal(err)
	}

	store, err := NewSQLiteStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	accounts, err := store.LoadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Email != account.Email {
		t.Fatalf("imported accounts = %#v", accounts)
	}
	sessions, err := store.LoadNormalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("imported sessions = %#v", sessions)
	}
	orders, err := store.LoadOrders()
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].ID != order.ID {
		t.Fatalf("imported orders = %#v", orders)
	}
	reviews, err := store.LoadPendingReviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].ID != pendingReview.ID {
		t.Fatalf("imported reviews = %#v", reviews)
	}
	var backend map[string]any
	if err := store.LoadBackendJSON("account_discovery_snapshots", &backend); err != nil {
		t.Fatal(err)
	}
	if len(backend) == 0 {
		t.Fatal("backend snapshot import was empty")
	}
}

func TestSQLiteStoreClaimsTaskAtomically(t *testing.T) {
	paths := testSQLiteStorageConfig(t)
	store, err := NewSQLiteStore(paths)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveTasks([]engine.Task{
		{ID: "task_1", Type: engine.TaskTypeAccountPipeline, State: engine.TaskStatePending},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	task, ok, err := store.ClaimNextAccountPipelineTask(false, now)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || task.ID != "task_1" || task.State != engine.TaskStateRunning {
		t.Fatalf("claimed task = %#v ok=%v", task, ok)
	}
	_, ok, err = store.ClaimNextAccountPipelineTask(false, now)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("second claim unexpectedly succeeded")
	}
}

func TestSQLiteTransactionDoneErrorsAreRetryable(t *testing.T) {
	if !isSQLiteBusyError(sql.ErrTxDone) {
		t.Fatal("sql.ErrTxDone should be retried")
	}
	err := fmt.Errorf("save failed: %w", errors.New("sql: transaction has already been committed or rolled back"))
	if !isSQLiteBusyError(err) {
		t.Fatal("transaction committed/rolled back text should be retried")
	}
}
