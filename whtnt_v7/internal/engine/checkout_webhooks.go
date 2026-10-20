package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

func (e *Engine) sendCheckoutWebhookIfNeeded(ctx context.Context, account Account, order *Order) {
	if order == nil || order.CheckoutWebhookSent {
		return
	}
	if !e.webhook.Enabled() {
		logger.ManagerError("Checkout webhook not sent: success webhook is not configured.")
		return
	}
	if err := e.webhook.SendCheckout(ctx, account, *order); err != nil {
		logger.ManagerError("Checkout webhook failed: " + err.Error())
		e.sendErrorWebhook(ctx, "Checkout webhook failed", err, map[string]string{
			"Account": account.Email,
			"Order":   orderLabel(*order),
		})
		return
	}
	order.CheckoutWebhookSent = true
	order.UpdatedAt = e.now()
	logger.Manager(fmt.Sprintf("Checkout webhook sent for %s.", orderLabel(*order)))
}

func (e *Engine) recordImmediateCheckoutOrders(ctx context.Context, account Account, orders []Order, checkout CheckoutResult) []Order {
	e.sendCheckoutPriceUpdateWebhooks(ctx, account, checkout.PriceUpdates)
	e.sendCheckoutProductIssueWebhooks(ctx, account, checkout.ProductIssues)
	now := e.now()
	checkoutOrders := checkout.Orders
	if len(checkoutOrders) == 0 && checkout.Order.ID != "" {
		checkoutOrders = []Order{checkout.Order}
	}
	for index := range checkoutOrders {
		checkoutOrders[index].AccountID = account.ID
		checkoutOrders[index].State = OrderStateCreated
		checkoutOrders[index].CreatedAt = ensureTime(checkoutOrders[index].CreatedAt, now)
		checkoutOrders[index].UpdatedAt = now
		e.enrichCheckoutOrderForWebhook(ctx, account, checkout.Session, &checkoutOrders[index])
		orders = upsertOrder(orders, checkoutOrders[index])
		orderIndex := findOrderIndex(orders, checkoutOrders[index].ID)
		if orderIndex < 0 {
			continue
		}
		webhookAccount := account
		if username := metadataValue(orders[orderIndex].Metadata, "account_username"); username != "" {
			webhookAccount.Username = username
		}
		e.sendCheckoutWebhookIfNeeded(ctx, webhookAccount, &orders[orderIndex])
	}
	if err := e.store.SaveOrders(orders); err != nil {
		logger.ManagerError("Immediate checkout order save failed: " + err.Error())
	}
	return orders
}

func (e *Engine) enrichCheckoutOrderForWebhook(ctx context.Context, account Account, session SessionRecord, order *Order) {
	if order == nil || strings.TrimSpace(metadataValue(order.Metadata, "seller_username")) != "" || strings.TrimSpace(session.ID) == "" {
		return
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result, err := e.flows.CheckOrder(lookupCtx, OrderCheckInput{Account: account, Session: session, Order: *order})
	if err != nil || strings.TrimSpace(metadataValue(result.Metadata, "seller_username")) == "" {
		results, listErr := e.flows.ListOrders(lookupCtx, AccountOrdersInput{Account: account, Session: session})
		if listErr != nil {
			if err != nil {
				logger.ManagerError("Checkout webhook seller lookup failed: " + err.Error())
			} else {
				logger.ManagerError("Checkout webhook seller lookup failed: " + listErr.Error())
			}
			return
		}
		match, ok := matchingCheckoutOrderResult(*order, results)
		if !ok {
			return
		}
		result = match
	}
	mergeOrderCheckMetadata(order, result)
}

func matchingCheckoutOrderResult(order Order, results []OrderCheckResult) (OrderCheckResult, bool) {
	for _, result := range results {
		if orderCheckResultMatchesOrder(order, result) {
			return result, true
		}
	}
	return OrderCheckResult{}, false
}

func orderCheckResultMatchesOrder(order Order, result OrderCheckResult) bool {
	for _, candidate := range []string{
		order.ExternalOrderID,
		order.ID,
		metadataValue(order.Metadata, "encoded_id"),
		metadataValue(order.Metadata, "display_id"),
	} {
		if candidate == "" {
			continue
		}
		if strings.EqualFold(candidate, result.ExternalOrderID) ||
			strings.EqualFold(candidate, metadataValue(result.Metadata, "encoded_id")) ||
			strings.EqualFold(candidate, metadataValue(result.Metadata, "display_id")) {
			return true
		}
	}
	listingID := metadataValue(order.Metadata, "listing_id")
	if listingID != "" && strings.EqualFold(listingID, metadataValue(result.Metadata, "listing_id")) {
		return true
	}
	return false
}

func mergeOrderCheckMetadata(order *Order, result OrderCheckResult) {
	if order == nil {
		return
	}
	if order.Metadata == nil {
		order.Metadata = map[string]any{}
	}
	for key, value := range result.Metadata {
		if value == nil {
			continue
		}
		if existing := metadataValue(order.Metadata, key); existing != "" && key != "seller_username" {
			continue
		}
		order.Metadata[key] = value
	}
	if strings.TrimSpace(order.ExternalOrderID) == "" {
		order.ExternalOrderID = strings.TrimSpace(result.ExternalOrderID)
	}
	if strings.TrimSpace(order.TrackingNumber) == "" {
		order.TrackingNumber = strings.TrimSpace(result.TrackingNumber)
	}
	if result.ReadyForReview {
		order.State = OrderStateReviewReady
	}
}

func (e *Engine) sendCheckoutProductIssueWebhooks(ctx context.Context, account Account, issues []ProductIssue) {
	for _, issue := range issues {
		err := fmt.Errorf("product unavailable for %s: %s", firstNonEmptyString(issue.ProductName, issue.ProductListingID), firstNonEmptyString(issue.Reason, "unavailable"))
		e.sendErrorWebhook(ctx, "Product unavailable", err, map[string]string{
			"Account": account.Email,
			"Product": firstNonEmptyString(issue.ProductName, issue.ProductListingID),
			"Listing": issue.ProductListingID,
			"Reason":  firstNonEmptyString(issue.Reason, "unavailable"),
		})
	}
}

func (e *Engine) sendCheckoutPriceUpdateWebhooks(ctx context.Context, account Account, updates []PriceUpdate) {
	for _, update := range updates {
		err := fmt.Errorf("product price changed for %s: %s -> %s (maxPrice %s)", firstNonEmptyString(update.ProductName, update.ProductListingID), rewardCreditLine(update.OldPrice, "GBP"), rewardCreditLine(update.NewPrice, "GBP"), rewardCreditLine(update.MaxPrice, "GBP"))
		e.sendErrorWebhook(ctx, "Product price changed", err, map[string]string{
			"Account": account.Email,
			"Product": firstNonEmptyString(update.ProductName, update.ProductListingID),
			"Listing": update.ProductListingID,
			"Old":     rewardCreditLine(update.OldPrice, "GBP"),
			"New":     rewardCreditLine(update.NewPrice, "GBP"),
			"Max":     rewardCreditLine(update.MaxPrice, "GBP"),
		})
	}
}

func taskOrderIDs(task Task) []string {
	ids := []string{}
	if task.OrderID != "" {
		ids = append(ids, task.OrderID)
	}
	if task.Metadata == nil {
		return ids
	}
	raw, ok := task.Metadata["order_ids"]
	if !ok {
		return ids
	}
	switch value := raw.(type) {
	case []string:
		for _, id := range value {
			ids = appendUniqueString(ids, id)
		}
	case []any:
		for _, item := range value {
			ids = appendUniqueString(ids, fmt.Sprint(item))
		}
	}
	return ids
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
