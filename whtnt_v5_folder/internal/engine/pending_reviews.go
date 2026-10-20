package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

var errReviewNoLongerPending = errors.New("review is no longer pending")

func (e *Engine) queuePendingReview(ctx context.Context, account Account, order *Order) error {
	if order == nil || order.State != OrderStateReviewReady || order.ReviewCompleted {
		return nil
	}
	if strings.TrimSpace(order.ReviewReferenceID) == "" {
		return nil
	}
	items, err := e.store.LoadPendingReviews()
	if err != nil {
		return err
	}
	now := e.now()
	record := pendingReviewFromOrder(account, *order, now)
	found := false
	for i := range items {
		if items[i].ID == record.ID {
			record.DetectedAt = items[i].DetectedAt
			if record.DetectedAt.IsZero() {
				record.DetectedAt = now
			}
			if strings.EqualFold(items[i].Status, "submitted") {
				record.Status = items[i].Status
			}
			items[i] = record
			found = true
			break
		}
	}
	if !found {
		items = append(items, record)
	}
	if err := e.store.SavePendingReviews(items); err != nil {
		return err
	}
	if !orderMetadataBool(order.Metadata, "pending_review_webhook_sent") {
		if err := e.webhook.SendPendingReview(ctx, account, *order); err != nil {
			e.sendErrorWebhook(ctx, "Pending review webhook failed", err, map[string]string{
				"Account": account.Email,
				"Order":   orderLabel(*order),
			})
			logger.ManagerError(fmt.Sprintf("Pending review webhook failed for %s; continuing: %s", orderLabel(*order), err.Error()))
			return nil
		}
		if order.Metadata == nil {
			order.Metadata = map[string]any{}
		}
		order.Metadata["pending_review_webhook_sent"] = true
		order.UpdatedAt = now
		logger.Manager(fmt.Sprintf("Pending review webhook sent for %s.", orderLabel(*order)))
	}
	return nil
}

func pendingReviewFromOrder(account Account, order Order, now time.Time) PendingReviewItem {
	return PendingReviewItem{
		ID:                pendingReviewID(account.ID, order),
		AccountID:         account.ID,
		AccountEmail:      account.Email,
		AccountUsername:   account.Username,
		OrderID:           order.ID,
		ExternalOrderID:   order.ExternalOrderID,
		ReviewReferenceID: order.ReviewReferenceID,
		Items:             metadataStringList(order.Metadata, "items"),
		DisplayID:         firstNonEmptyString(metadataValue(order.Metadata, "display_id"), order.ExternalOrderID, order.ID),
		Status:            "pending",
		DetectedAt:        now,
		UpdatedAt:         now,
		Metadata:          cloneMetadata(order.Metadata),
	}
}

func pendingReviewID(accountID string, order Order) string {
	return firstNonEmptyString(order.ReviewReferenceID, order.ExternalOrderID, order.ID, accountID)
}

func orderMetadataBool(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	switch value := metadata[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func metadataStringList(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []string{strings.TrimSpace(value)}
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return nil
		}
		return []string{text}
	}
}

func cloneMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	out := make(map[string]any, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func (e *Engine) ListPendingReviews() ([]PendingReviewItem, error) {
	items, err := e.store.LoadPendingReviews()
	if err != nil {
		return nil, err
	}
	sessions, _ := e.store.LoadNormalSessions()
	out := make([]PendingReviewItem, 0, len(items))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Status), "pending") {
			if item.AccountCredits == "" {
				item.AccountCredits = pendingReviewCreditLabel(item, sessions)
			}
			out = append(out, item)
		}
	}
	return out, nil
}

func pendingReviewCreditLabel(item PendingReviewItem, sessions []SessionRecord) string {
	for _, session := range sessions {
		if item.AccountID != "" && session.AccountID == item.AccountID {
			return sessionCreditLabel(session)
		}
		if item.AccountEmail != "" && strings.EqualFold(metadataValue(session.Metadata, "email"), item.AccountEmail) {
			return sessionCreditLabel(session)
		}
	}
	return metadataCreditLabel(item.Metadata)
}

func (e *Engine) SubmitPendingReview(ctx context.Context, reviewID string, ratings []int, text string) error {
	reviewID = strings.TrimSpace(reviewID)
	if reviewID == "" {
		return fmt.Errorf("review id is required")
	}
	items, err := e.store.LoadPendingReviews()
	if err != nil {
		return err
	}
	itemIndex := -1
	var item PendingReviewItem
	for i := range items {
		if items[i].ID == reviewID {
			itemIndex = i
			item = items[i]
			break
		}
	}
	if itemIndex < 0 {
		return fmt.Errorf("pending review %s not found", reviewID)
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
	accountIndex := findAccountIndex(accounts, item.AccountID)
	if accountIndex < 0 {
		return fmt.Errorf("account %s not found for review", item.AccountID)
	}
	orderIndex := findOrderIndex(orders, item.OrderID)
	if orderIndex < 0 {
		return fmt.Errorf("order %s not found for review", item.OrderID)
	}
	session, ok := findSessionForAccount(sessions, accounts[accountIndex])
	if !ok {
		return fmt.Errorf("session not found for review account %s", accounts[accountIndex].Email)
	}
	refreshed, healthy, err := e.flows.RefreshSessionWithoutCredits(ctx, session)
	if err != nil || !healthy {
		items[itemIndex].LastError = sessionAuditError(err, healthy)
		items[itemIndex].UpdatedAt = e.now()
		_ = e.store.SavePendingReviews(items)
		if err != nil {
			return fmt.Errorf("refresh session before review: %w", err)
		}
		return fmt.Errorf("refresh session before review returned unhealthy")
	}
	if err := validateRefreshedSession(session, refreshed); err != nil {
		items[itemIndex].LastError = err.Error()
		items[itemIndex].UpdatedAt = e.now()
		_ = e.store.SavePendingReviews(items)
		return err
	}
	session = refreshed
	sessions = upsertSession(sessions, refreshed)
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	if item.ReviewReferenceID != "" {
		orders[orderIndex].ReviewReferenceID = item.ReviewReferenceID
	}
	if orders[orderIndex].Metadata == nil {
		orders[orderIndex].Metadata = map[string]any{}
	}
	if item.ReviewReferenceID != "" {
		orders[orderIndex].Metadata["review_shipment_id"] = item.ReviewReferenceID
	}
	if current, checkErr := e.flows.CheckOrder(ctx, OrderCheckInput{
		Account: accounts[accountIndex],
		Order:   orders[orderIndex],
		Session: session,
	}); checkErr == nil {
		currentOrder := orderFromCheckResult(accounts[accountIndex].ID, current, e.now())
		if currentOrder.ID != "" {
			orders = upsertOrder(orders, currentOrder)
			orderIndex = findOrderIndex(orders, currentOrder.ID)
			if orderIndex < 0 {
				orderIndex = findOrderIndex(orders, item.OrderID)
			}
		}
		if !current.ReadyForReview {
			reason := reviewNotPendingReason(current, orders, orderIndex)
			items[itemIndex].Status = "skipped"
			items[itemIndex].LastError = reason
			items[itemIndex].UpdatedAt = e.now()
			_ = e.store.SaveOrders(orders)
			_ = e.store.SavePendingReviews(items)
			return fmt.Errorf("%w: %s", errReviewNoLongerPending, reason)
		}
	} else {
		logger.ManagerError(fmt.Sprintf("Review pre-check failed for %s; trying submit anyway: %s", firstNonEmptyString(item.DisplayID, item.ExternalOrderID, item.OrderID), checkErr.Error()))
	}
	reviewResult, err := e.flows.LeaveReview(ctx, ReviewInput{
		Account: accounts[accountIndex],
		Order:   orders[orderIndex],
		Session: session,
		Stars:   firstRating(ratings),
		Ratings: ratings,
		Text:    text,
	})
	if err != nil {
		items[itemIndex].LastError = err.Error()
		items[itemIndex].UpdatedAt = e.now()
		_ = e.store.SavePendingReviews(items)
		return err
	}

	now := e.now()
	orders[orderIndex].State = OrderStateReviewed
	orders[orderIndex].ReviewCompleted = true
	orders[orderIndex].LastError = ""
	orders[orderIndex].UpdatedAt = now
	if orders[orderIndex].Metadata == nil {
		orders[orderIndex].Metadata = map[string]any{}
	}
	if reviewResult.Session.ID != "" {
		session = reviewResult.Session
		session.ID = firstNonEmptyString(accounts[accountIndex].SessionID, reviewResult.Session.ID, session.ID)
		session.AccountID = accounts[accountIndex].ID
		accounts[accountIndex].SessionID = session.ID
		session.CreatedAt = ensureTime(session.CreatedAt, now)
		session.UpdatedAt = now
		syncAccountIdentityFromSessionMetadata(&accounts[accountIndex], session)
		sessions = upsertSession(sessions, session)
	}
	if strings.TrimSpace(reviewResult.NewUsername) != "" {
		newUsername := strings.TrimSpace(reviewResult.NewUsername)
		orders[orderIndex].Metadata["review_new_username"] = newUsername
		accounts[accountIndex].Username = newUsername
	}
	if reviewResult.AccountsFollowed > 0 {
		orders[orderIndex].Metadata["review_accounts_followed"] = reviewResult.AccountsFollowed
	}
	if !orders[orderIndex].ReviewWebhookSent {
		if err := e.webhook.SendReview(ctx, accounts[accountIndex], orders[orderIndex], text); err != nil {
			orders[orderIndex].LastError = err.Error()
			e.sendErrorWebhook(ctx, "Review webhook failed", err, map[string]string{
				"Account": accounts[accountIndex].Email,
				"Order":   orderLabel(orders[orderIndex]),
			})
		} else {
			orders[orderIndex].ReviewWebhookSent = true
		}
	}
	if allAccountOrdersReviewed(orders, accounts[accountIndex].ID) {
		accounts[accountIndex].State = AccountStateComplete
		accounts[accountIndex].LastError = ""
		completedAt := now
		accounts[accountIndex].CompletedAt = &completedAt
	}
	accounts[accountIndex].UpdatedAt = now
	if err := e.store.SaveOrders(orders); err != nil {
		return err
	}
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return err
	}
	if err := e.store.SaveAccounts(accounts); err != nil {
		return err
	}
	items[itemIndex].Status = "submitted"
	items[itemIndex].LastError = ""
	items[itemIndex].UpdatedAt = now
	return e.store.SavePendingReviews(items)
}

func reviewNotPendingReason(result OrderCheckResult, orders []Order, orderIndex int) string {
	status := ""
	if orderIndex >= 0 && orderIndex < len(orders) {
		status = orderStatusLabel(orders[orderIndex])
	}
	status = firstNonEmptyString(
		metadataValue(result.Metadata, "pretty_status"),
		metadataValue(result.Metadata, "overview_status"),
		metadataValue(result.Metadata, "status"),
		status,
		"not review pending",
	)
	return "order is not pending review; current status: " + status
}

func isReviewNoLongerPendingError(err error) bool {
	return errors.Is(err, errReviewNoLongerPending)
}

func firstRating(ratings []int) int {
	if len(ratings) == 0 {
		return 5
	}
	if ratings[0] < 1 {
		return 1
	}
	if ratings[0] > 5 {
		return 5
	}
	return ratings[0]
}
