package engine

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

type atomicTaskStore interface {
	ClaimNextAccountPipelineTask(startFresh bool, now time.Time) (Task, bool, error)
	UpdateTask(taskID string, update func(*Task), now time.Time) error
}

func (e *Engine) requeueStaleTasks() error {
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return err
	}
	now := e.now()
	changed := false
	for i := range tasks {
		if tasks[i].State != TaskStateRunning || tasks[i].StartedAt == nil {
			continue
		}
		if now.Sub(*tasks[i].StartedAt) < e.cfg.Engine.StaleTaskAfter.Duration {
			continue
		}
		tasks[i].State = TaskStatePending
		tasks[i].StartedAt = nil
		tasks[i].UpdatedAt = now
		changed = true
	}
	if !changed {
		return nil
	}
	return e.store.SaveTasks(tasks)
}

func (e *Engine) claimNextTask() (Task, bool, error) {
	if taskStore, ok := e.store.(atomicTaskStore); ok {
		return taskStore.ClaimNextAccountPipelineTask(e.hasStartFreshApproval(), e.now())
	}
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return Task{}, false, err
	}
	now := e.now()
	for i := range tasks {
		if tasks[i].Type != TaskTypeAccountPipeline || tasks[i].State != TaskStatePending {
			continue
		}
		if e.hasStartFreshApproval() && tasks[i].AccountID != "" {
			continue
		}
		tasks[i].State = TaskStateRunning
		tasks[i].Attempt++
		tasks[i].StartedAt = &now
		tasks[i].UpdatedAt = now
		claimed := tasks[i]
		if err := e.store.SaveTasks(tasks); err != nil {
			return Task{}, false, err
		}
		return claimed, true, nil
	}
	return Task{}, false, nil
}

func (e *Engine) updateTask(taskID string, update func(*Task)) error {
	if taskStore, ok := e.store.(atomicTaskStore); ok {
		return taskStore.UpdateTask(taskID, update, e.now())
	}
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return err
	}
	for i := range tasks {
		if tasks[i].ID != taskID {
			continue
		}
		update(&tasks[i])
		if tasks[i].UpdatedAt.IsZero() {
			tasks[i].UpdatedAt = e.now()
		}
		return e.store.SaveTasks(tasks)
	}
	return fmt.Errorf("task %s not found", taskID)
}

func (e *Engine) failTask(taskID string, cause error) error {
	updateErr := e.updateTask(taskID, func(t *Task) {
		now := e.now()
		if isTerminalTaskError(cause) || (t.MaxAttempts > 0 && t.Attempt >= t.MaxAttempts) {
			t.State = TaskStateFailed
		} else {
			t.State = TaskStatePending
		}
		t.StartedAt = nil
		t.UpdatedAt = now
		t.LastError = cause.Error()
	})
	if updateErr != nil {
		return updateErr
	}
	return cause
}

func (e *Engine) completeTask(taskID string) error {
	return e.updateTask(taskID, func(t *Task) {
		now := e.now()
		t.State = TaskStateCompleted
		t.CompletedAt = &now
		t.StartedAt = nil
		t.UpdatedAt = now
		t.LastError = ""
	})
}

func (e *Engine) waitForCurrentRunAccountTasksIdle(ctxDone <-chan struct{}, runStartedAt time.Time) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	logged := false
	for {
		count, err := e.countRunningAccountTasksStartedSince(runStartedAt)
		if err != nil {
			logger.ManagerError("Task idle check failed: " + err.Error())
			return
		}
		if count == 0 {
			return
		}
		if !logged {
			logger.Manager(fmt.Sprintf("Waiting for %d active task(s) to finish before returning to the menu.", count))
			logged = true
		}
		select {
		case <-ctxDone:
			return
		case <-ticker.C:
		}
	}
}

func (e *Engine) countRunningAccountTasksStartedSince(runStartedAt time.Time) (int, error) {
	tasks, err := e.store.LoadTasks()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, task := range tasks {
		if task.Type != TaskTypeAccountPipeline || task.State != TaskStateRunning || task.StartedAt == nil {
			continue
		}
		if task.StartedAt.Before(runStartedAt) {
			continue
		}
		count++
	}
	return count, nil
}

func (e *Engine) savePipelineState(accounts []Account, orders []Order, normalSessions []SessionRecord, masterSessions []SessionRecord, masters []MasterAccount) error {
	if err := e.store.SaveNormalSessions(normalSessions); err != nil {
		return err
	}
	if err := e.store.SaveMasterSessions(masterSessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	return e.store.SaveMasters(masters)
}

func newTaskID() string {
	var random [6]byte
	if _, err := rand.Read(random[:]); err == nil {
		return fmt.Sprintf("task_%d_%s", time.Now().UnixNano(), hex.EncodeToString(random[:]))
	}
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

func nextWindowStart(window TimeWindow, now time.Time) time.Time {
	local := now.In(window.Location)
	start := time.Date(local.Year(), local.Month(), local.Day(), window.StartHour, window.StartMinute, 0, 0, window.Location)
	if !local.Before(start) {
		start = start.Add(24 * time.Hour)
	}
	return start
}
