package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

type discoveredOrders struct {
	orders        []Order
	currentOrders []Order
	sessions      []SessionRecord
	results       map[string]OrderCheckResult
}

func (e *Engine) discoverAccountOrders(ctx context.Context, accounts []Account, orders []Order, sessions []SessionRecord) (discoveredOrders, error) {
	return e.discoverAccountOrdersWithRefresh(ctx, accounts, orders, sessions, true)
}

func (e *Engine) discoverAccountOrdersWithoutRefresh(ctx context.Context, accounts []Account, orders []Order, sessions []SessionRecord) (discoveredOrders, error) {
	return e.discoverAccountOrdersWithRefresh(ctx, accounts, orders, sessions, false)
}

func (e *Engine) discoverAccountOrdersWithRefresh(ctx context.Context, accounts []Account, orders []Order, sessions []SessionRecord, refreshSessions bool) (discoveredOrders, error) {
	out := discoveredOrders{
		orders:   orders,
		sessions: sessions,
		results:  map[string]OrderCheckResult{},
	}

	var combinedErr error
	now := e.now()
	for _, account := range accounts {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if storedAccountIsKnownBanned(account) || strings.TrimSpace(account.SessionID) == "" {
			continue
		}
		session, ok := findSessionForAccount(sessions, account)
		if !ok {
			combinedErr = joinErr(combinedErr, fmt.Errorf("session %s missing for account %s", account.SessionID, account.ID))
			continue
		}
		if refreshSessions {
			refreshed, healthy, err := e.flows.RefreshSessionWithoutCredits(ctx, session)
			if err != nil {
				combinedErr = joinErr(combinedErr, fmt.Errorf("refresh session for %s: %w", account.Email, err))
				continue
			}
			if !healthy {
				combinedErr = joinErr(combinedErr, fmt.Errorf("session for %s became unhealthy", account.Email))
				continue
			}
			if err := validateRefreshedSession(session, refreshed); err != nil {
				combinedErr = joinErr(combinedErr, fmt.Errorf("refresh session for %s returned unusable data: %w", account.Email, err))
				continue
			}
			session = refreshed
			out.sessions = upsertSession(out.sessions, refreshed)
			if err := e.store.SaveNormalSessions(out.sessions); err != nil {
				return out, fmt.Errorf("save refreshed session for %s: %w", account.Email, err)
			}
		}

		results, err := e.flows.ListOrders(ctx, AccountOrdersInput{Account: account, Session: session})
		if err != nil {
			fallbackResults, fallbackErr := e.checkStoredAccountOrders(ctx, account, session, out.orders)
			if fallbackErr != nil && len(fallbackResults) == 0 {
				combinedErr = joinErr(combinedErr, fmt.Errorf("list orders for %s: %w; stored order fallback: %w", account.Email, err, fallbackErr))
				continue
			}
			if len(fallbackResults) == 0 {
				combinedErr = joinErr(combinedErr, fmt.Errorf("list orders for %s: %w", account.Email, err))
				continue
			}
			if fallbackErr != nil {
				combinedErr = joinErr(combinedErr, fmt.Errorf("stored order fallback partial errors for %s: %w", account.Email, fallbackErr))
			}
			logger.Manager(fmt.Sprintf("List orders failed for %s; checked %d stored order id(s) instead.", firstNonEmptyString(account.Email, account.Username, account.ID), len(fallbackResults)))
			results = fallbackResults
		}
		if len(results) == 0 {
			fallbackResults, fallbackErr := e.checkStoredAccountOrders(ctx, account, session, out.orders)
			if fallbackErr != nil {
				combinedErr = joinErr(combinedErr, fmt.Errorf("stored order fallback for %s: %w", account.Email, fallbackErr))
			}
			if len(fallbackResults) > 0 {
				logger.Manager(fmt.Sprintf("No listed orders for %s; checked %d stored order id(s) instead.", firstNonEmptyString(account.Email, account.Username, account.ID), len(fallbackResults)))
				results = fallbackResults
			}
		}

		for _, result := range results {
			order := orderFromCheckResult(account.ID, result, now)
			if order.ID == "" {
				continue
			}
			addSessionCreditMetadata(order.Metadata, session)
			out.currentOrders = upsertOrder(out.currentOrders, order)
			previous := Order{}
			hadPrevious := false
			if existingIndex := findOrderIndex(out.orders, order.ID); existingIndex >= 0 {
				previous = out.orders[existingIndex]
				hadPrevious = true
			}
			out.orders = upsertOrder(out.orders, order)
			orderIndex := findOrderIndex(out.orders, order.ID)
			if orderIndex >= 0 {
				if shouldLogDiscoveredOrder(previous, out.orders[orderIndex], hadPrevious) {
					e.logDetectedOrder(account, out.orders[orderIndex])
				}
				out.results[orderStatusKey(account.ID, out.orders[orderIndex].ExternalOrderID, out.orders[orderIndex].ID)] = result
				if hadPrevious && orderStatusLabel(previous) != orderStatusLabel(out.orders[orderIndex]) {
					if err := e.webhook.SendOrderStatusChange(ctx, account, previous, out.orders[orderIndex]); err != nil {
						combinedErr = joinErr(combinedErr, fmt.Errorf("order status webhook for %s: %w", out.orders[orderIndex].ID, err))
					}
				}
				if err := e.sendTrackingWebhookImmediately(ctx, account, &out.orders[orderIndex]); err != nil {
					combinedErr = joinErr(combinedErr, err)
				}
				if out.orders[orderIndex].State == OrderStateReviewReady {
					if err := e.queuePendingReview(ctx, account, &out.orders[orderIndex]); err != nil {
						combinedErr = joinErr(combinedErr, err)
					}
				}
				if err := e.store.SaveOrders(out.orders); err != nil {
					return out, fmt.Errorf("save discovered order for %s: %w", account.Email, err)
				}
			} else {
				out.results[orderStatusKey(account.ID, order.ExternalOrderID, order.ID)] = result
			}
		}
	}

	return out, combinedErr
}

func shouldLogDiscoveredOrder(previous Order, current Order, hadPrevious bool) bool {
	if !hadPrevious {
		return true
	}
	if orderStatusLabel(previous) != orderStatusLabel(current) {
		return true
	}
	if strings.TrimSpace(previous.TrackingNumber) == "" && strings.TrimSpace(current.TrackingNumber) != "" {
		return true
	}
	return false
}

func (e *Engine) checkStoredAccountOrders(ctx context.Context, account Account, session SessionRecord, orders []Order) ([]OrderCheckResult, error) {
	accountOrders := ordersForAccount(orders, account.ID)
	if len(accountOrders) == 0 {
		return nil, nil
	}
	results := []OrderCheckResult{}
	var combinedErr error
	for _, order := range accountOrders {
		refs := storedOrderCheckRefs(order)
		if len(refs) == 0 {
			continue
		}
		var lastErr error
		for _, ref := range refs {
			checkOrder := order
			checkOrder.ExternalOrderID = ref
			result, err := e.flows.CheckOrder(ctx, OrderCheckInput{
				Account: account,
				Session: session,
				Order:   checkOrder,
			})
			if err != nil {
				lastErr = err
				continue
			}
			if result.ExternalOrderID == "" {
				result.ExternalOrderID = firstNonEmptyString(order.ExternalOrderID, ref)
			}
			if result.Metadata == nil {
				result.Metadata = map[string]any{}
			}
			if metadataValue(result.Metadata, "encoded_id") == "" {
				result.Metadata["encoded_id"] = strings.TrimPrefix(order.ID, "order_")
			}
			results = append(results, result)
			lastErr = nil
			break
		}
		if lastErr != nil {
			combinedErr = joinErr(combinedErr, fmt.Errorf("%s: %w", orderLabel(order), lastErr))
		}
	}
	return results, combinedErr
}

func storedOrderCheckRefs(order Order) []string {
	refs := []string{}
	refs = appendUniqueString(refs, strings.TrimSpace(order.ExternalOrderID))
	refs = appendUniqueString(refs, metadataValue(order.Metadata, "encoded_id"))
	refs = appendUniqueString(refs, metadataValue(order.Metadata, "uuid"))
	refs = appendUniqueString(refs, strings.TrimPrefix(strings.TrimSpace(order.ID), "order_"))
	refs = appendUniqueString(refs, strings.TrimSpace(order.ID))
	out := refs[:0]
	for _, ref := range refs {
		if strings.TrimSpace(ref) != "" {
			out = append(out, strings.TrimSpace(ref))
		}
	}
	return out
}

func (e *Engine) logDetectedOrder(account Account, order Order) {
	credits := metadataCreditLabel(order.Metadata)
	logger.Manager(fmt.Sprintf(
		"Detected order for %s: %s | status=%s | item=%s",
		accountMentionLabel(account, credits),
		firstNonEmptyString(metadataValue(order.Metadata, "display_id"), order.ExternalOrderID, order.ID),
		firstNonEmptyString(metadataValue(order.Metadata, "pretty_status"), metadataValue(order.Metadata, "overview_status"), string(order.State)),
		firstNonEmptyString(metadataValue(order.Metadata, "items"), metadataValue(order.Metadata, "listing_title"), "-"),
	))
}

func (e *Engine) sendTrackingWebhookImmediately(ctx context.Context, account Account, order *Order) error {
	if order == nil || order.TrackingWebhookSent || strings.TrimSpace(order.TrackingNumber) == "" {
		return nil
	}
	if !orderEligibleForTrackingWebhook(*order) {
		return nil
	}
	if err := e.webhook.SendTracking(ctx, account, *order, order.Metadata); err != nil {
		order.LastError = err.Error()
		order.UpdatedAt = e.now()
		e.sendErrorWebhook(ctx, "Tracking webhook failed", err, map[string]string{
			"Account": account.Email,
			"Order":   orderLabel(*order),
		})
		logger.ManagerError(fmt.Sprintf("Tracking webhook failed for %s; continuing: %s", orderLabel(*order), err.Error()))
		return nil
	}
	order.TrackingWebhookSent = true
	order.LastError = ""
	order.UpdatedAt = e.now()
	if err := appendOrderExport(account, *order, account.FirstName, account.LastName, e.now()); err != nil {
		return fmt.Errorf("orders.csv export for %s: %w", order.ID, err)
	}
	return nil
}

func orderEligibleForTrackingWebhook(order Order) bool {
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
		strings.Contains(status, "reviewed") {
		return false
	}
	return true
}

func orderFromCheckResult(accountID string, result OrderCheckResult, now time.Time) Order {
	externalID := strings.TrimSpace(result.ExternalOrderID)
	encodedID := metadataValue(result.Metadata, "encoded_id")
	idSeed := firstMetadataString(result.Metadata, "encoded_id", externalID)
	if idSeed == "" {
		return Order{}
	}
	order := Order{
		ID:                "order_" + idSeed,
		AccountID:         accountID,
		ExternalOrderID:   externalID,
		State:             OrderStateInProgress,
		TrackingNumber:    strings.TrimSpace(result.TrackingNumber),
		ReviewReferenceID: strings.TrimSpace(result.ReviewReferenceID),
		CreatedAt:         now,
		UpdatedAt:         now,
		Metadata:          map[string]any{},
	}
	if externalID == "" {
		order.ExternalOrderID = encodedID
	}
	if result.ReadyForReview {
		order.State = OrderStateReviewReady
	}
	for key, value := range result.Metadata {
		order.Metadata[key] = value
	}
	return order
}

func orderStatusKey(accountID string, values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return accountID + "::" + strings.TrimSpace(value)
		}
	}
	return accountID + "::"
}

func allAccountOrdersReviewed(orders []Order, accountID string) bool {
	found := false
	for _, order := range orders {
		if order.AccountID != accountID {
			continue
		}
		found = true
		if !order.ReviewCompleted && order.State != OrderStateReviewed {
			return false
		}
	}
	return found
}

func joinErr(existing error, next error) error {
	if existing == nil {
		return next
	}
	if next == nil {
		return existing
	}
	return fmt.Errorf("%v; %w", existing, next)
}
