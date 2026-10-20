package flows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"rnldo.dev/wnshell/internal/engine"
	"rnldo.dev/wnshell/internal/whtnt"
)

type Whatnot struct{}

func NewWhatnot() Whatnot {
	return Whatnot{}
}

func (Whatnot) Signup(ctx context.Context, input engine.SignupInput) (engine.SignupResult, error) {
	return signupWithMode(ctx, input, "account")
}

func (Whatnot) SignupForSEONTester(ctx context.Context, input engine.SignupInput) (engine.SignupResult, error) {
	return signupWithMode(ctx, input, "seon")
}

func (Whatnot) PrepareCheckoutAccount(ctx context.Context, input engine.SignupInput) (engine.SignupResult, error) {
	return signupWithMode(ctx, input, "prepared")
}

func signupWithMode(ctx context.Context, input engine.SignupInput, mode string) (engine.SignupResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.SignupResult{}, err
	}
	var module *whtnt.Module
	var err error
	if strings.TrimSpace(input.Email) != "" {
		module, err = whtnt.NewModuleWithCredentials(input.Email, input.Password)
	} else {
		module, err = whtnt.NewModule()
	}
	if err != nil {
		return engine.SignupResult{}, err
	}
	defer module.Close()

	module.SetReferralCode(input.FriendCode)
	if input.RewardDetected != nil {
		module.SetRewardDetectedHook(func(accountSnapshot whtnt.AccountSnapshot, sessionSnapshot whtnt.SessionSnapshot) error {
			result := signupResultFromSnapshots(accountSnapshot, sessionSnapshot)
			return input.RewardDetected(result)
		})
	}
	var registered bool
	switch mode {
	case "seon":
		registered, err = module.RegisterSEONTester()
	case "prepared":
		registered, err = module.RegisterPreparedCheckout()
	default:
		registered, err = module.RegisterAccountOnly()
	}
	if err != nil {
		return engine.SignupResult{}, err
	}
	if !registered {
		return engine.SignupResult{}, fmt.Errorf("registration did not complete")
	}

	accountSnapshot := module.AccountSnapshot()
	sessionSnapshot, err := module.SessionSnapshot()
	if err != nil {
		return engine.SignupResult{}, err
	}

	return signupResultFromSnapshots(accountSnapshot, sessionSnapshot), nil
}

func signupResultFromSnapshots(accountSnapshot whtnt.AccountSnapshot, sessionSnapshot whtnt.SessionSnapshot) engine.SignupResult {
	now := flowNow()
	accountID := accountSnapshot.ID
	if accountID == "" {
		accountID = "acct_" + strings.NewReplacer("@", "_", ".", "_").Replace(accountSnapshot.Email)
	}
	sessionID := sessionSnapshot.ID
	if sessionID == "" {
		sessionID = "sess_" + accountID
	}

	session := sessionFromSnapshot(sessionSnapshot)
	session.ID = sessionID
	session.AccountID = accountID
	session.CreatedAt = now
	session.UpdatedAt = now
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}

	return engine.SignupResult{
		Account: engine.Account{
			ID:        accountID,
			Username:  accountSnapshot.Username,
			Email:     accountSnapshot.Email,
			Password:  accountSnapshot.Password,
			FirstName: accountSnapshot.FirstName,
			LastName:  accountSnapshot.LastName,
			IP:        accountSnapshot.IP,
			Proxy:     accountSnapshot.Proxy,
			SessionID: session.ID,
			State:     engine.AccountStateSignedUp,
			CreatedAt: now,
			UpdatedAt: now,
		},
		Session: session,
	}
}

func (Whatnot) Checkout(ctx context.Context, input engine.CheckoutInput) (engine.CheckoutResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.CheckoutResult{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	defer module.Close()
	module.SetSkipDelays(input.SkipDelays)
	module.SetForceCheckout(input.ForceCheckout)
	module.SetAllowInsufficientBalance(input.AllowInsufficientBalance)
	if input.OrderPlaced != nil {
		module.SetCheckoutOrderHook(func(snapshot whtnt.CheckoutSnapshot) {
			result, err := checkoutResultFromSnapshot(snapshot, input.Account, input.Session, nil)
			if err != nil {
				return
			}
			input.OrderPlaced(result)
		})
	}

	preflightSnapshot, healthy, err := module.RefreshSessionSnapshot()
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	if !healthy {
		return engine.CheckoutResult{}, fmt.Errorf("session became unhealthy before checkout")
	}
	checkoutSnapshot, refreshedSnapshot, err := module.CheckoutGeneratedAccount()
	if err != nil {
		result := engine.CheckoutResult{}
		if preflightSnapshot.ID != "" {
			refreshedSession := sessionFromSnapshot(preflightSnapshot)
			refreshedSession.ID = input.Session.ID
			refreshedSession.AccountID = input.Session.AccountID
			refreshedSession.CreatedAt = input.Session.CreatedAt
			refreshedSession.UpdatedAt = flowNow()
			result.Session = refreshedSession
		}
		if errors.Is(err, whtnt.ErrNoBalance) {
			return result, fmt.Errorf("%w: %v", engine.ErrNoBalance, err)
		}
		return result, err
	}

	refreshedSession := sessionFromSnapshot(refreshedSnapshot)
	refreshedSession.ID = input.Session.ID
	refreshedSession.AccountID = input.Session.AccountID
	refreshedSession.CreatedAt = input.Session.CreatedAt
	refreshedSession.UpdatedAt = flowNow()

	result, err := checkoutResultFromSnapshot(checkoutSnapshot, input.Account, refreshedSession, &refreshedSession)
	if err != nil {
		if !checkoutSnapshot.CheckoutAttempted {
			return engine.CheckoutResult{Session: refreshedSession}, fmt.Errorf("%w: no checkout selected for reward/chance rules", engine.ErrCheckoutSkipped)
		}
		return engine.CheckoutResult{}, err
	}
	return result, nil
}

func (Whatnot) CheckoutPreparedProducts(ctx context.Context, input engine.PreparedCheckoutInput) (engine.CheckoutResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.CheckoutResult{}, err
	}
	products, err := parsePreparedProducts(input.ProductsJSON, input.RewardLabel)
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	if input.BalanceLimit > 0 {
		total := preparedProductsTotal(products)
		if total > input.BalanceLimit && !input.AllowInsufficientBalance {
			return engine.CheckoutResult{}, fmt.Errorf("%w: products total %s exceeds available reward balance %s", engine.ErrNoBalance, formatMinorGBP(total), formatMinorGBP(input.BalanceLimit))
		}
	} else if !input.AllowInsufficientBalance {
		total := preparedProductsTotal(products)
		return engine.CheckoutResult{}, fmt.Errorf("%w: products total %s has no proven available reward balance", engine.ErrNoBalance, formatMinorGBP(total))
	}
	if checkoutFullName(input.Account.FirstName, input.AddressName, input.Account.LastName) == "" {
		return engine.CheckoutResult{}, fmt.Errorf("prepared checkout row requires first_name and last_name/address_last_name for shipping")
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	defer module.Close()
	if strings.TrimSpace(input.PaymentMethodID) != "" {
		module.SetPaymentMethodID(input.PaymentMethodID)
	}
	module.SetSkipDelays(input.SkipDelays)
	module.SetForceCheckout(input.ForceCheckout)
	if input.OrderPlaced != nil {
		module.SetCheckoutOrderHook(func(snapshot whtnt.CheckoutSnapshot) {
			result, err := checkoutResultFromSnapshot(snapshot, input.Account, input.Session, nil)
			if err != nil {
				return
			}
			input.OrderPlaced(result)
		})
	}

	checkoutSnapshot, err := module.CheckoutPreparedProducts(products, whtnt.AddressFields{
		AddressLine1: input.AddressLine1,
		AddressLine2: input.AddressLine2,
		Town:         input.Town,
		Postcode:     input.Postcode,
		FirstName:    input.Account.FirstName,
		LastName:     firstNonEmpty(input.AddressName, input.Account.LastName),
	}, whtnt.CardDetails{
		Number:   input.CardNumber,
		CVC:      input.CardCVC,
		ExpMonth: input.CardExpMonth,
		ExpYear:  input.CardExpYear,
		Postcode: input.CardPostcode,
	})
	if err != nil {
		return engine.CheckoutResult{}, err
	}

	refreshedSnapshot, healthy, err := module.RefreshSessionSnapshot()
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	if !healthy {
		return engine.CheckoutResult{}, fmt.Errorf("session became unhealthy after prepared checkout")
	}
	refreshedSession := sessionFromSnapshot(refreshedSnapshot)
	refreshedSession.ID = input.Session.ID
	refreshedSession.AccountID = input.Session.AccountID
	refreshedSession.CreatedAt = input.Session.CreatedAt
	refreshedSession.UpdatedAt = flowNow()

	return checkoutResultFromSnapshot(checkoutSnapshot, input.Account, refreshedSession, &refreshedSession)
}

func checkoutResultFromSnapshot(checkoutSnapshot whtnt.CheckoutSnapshot, account engine.Account, metadataSession engine.SessionRecord, resultSession *engine.SessionRecord) (engine.CheckoutResult, error) {
	orderIDs := append([]string(nil), checkoutSnapshot.OrderIDs...)
	if len(orderIDs) == 0 {
		if orderID := strings.TrimSpace(checkoutSnapshot.OrderID); orderID != "" {
			orderIDs = []string{orderID}
		}
	}
	if len(orderIDs) == 0 {
		return engine.CheckoutResult{}, fmt.Errorf("checkout completed but did not expose an order id")
	}
	now := flowNow()
	orders := make([]engine.Order, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		order := engine.Order{
			ID:              "order_" + orderID,
			AccountID:       account.ID,
			ExternalOrderID: orderID,
			State:           engine.OrderStateCreated,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if product, ok := checkoutProductForOrder(checkoutSnapshot.Products, len(orders)); ok {
			order.Metadata = checkoutProductMetadata(product)
		}
		if order.Metadata == nil {
			order.Metadata = map[string]any{}
		}
		if username := firstNonEmpty(checkoutSnapshot.Username, metadataString(metadataSession.Metadata, "username"), account.Username); username != "" {
			order.Metadata["account_username"] = username
		}
		mergeCheckoutMetadata(order.Metadata, checkoutSnapshot.Address, checkoutSnapshot.Card)
		mergeSessionCreditMetadata(order.Metadata, metadataSession)
		orders = append(orders, order)
	}
	result := engine.CheckoutResult{
		Order:             orders[0],
		Orders:            orders,
		ShippingFirstName: firstNonEmpty(checkoutSnapshot.ShippingFirstName, account.FirstName),
		ShippingLastName:  firstNonEmpty(checkoutSnapshot.ShippingLastName, account.LastName),
		PriceUpdates:      priceUpdatesFromSnapshot(checkoutSnapshot.PriceUpdates),
		ProductIssues:     productIssuesFromSnapshot(checkoutSnapshot.ProductIssues),
	}
	if resultSession != nil {
		result.Session = *resultSession
	} else if metadataSession.ID != "" {
		result.Session = metadataSession
	}
	return result, nil
}

func productIssuesFromSnapshot(issues []whtnt.ProductIssue) []engine.ProductIssue {
	if len(issues) == 0 {
		return nil
	}
	out := make([]engine.ProductIssue, 0, len(issues))
	for _, issue := range issues {
		out = append(out, engine.ProductIssue{
			ProductName:      issue.ProductName,
			ProductListingID: issue.ProductListingID,
			Reason:           issue.Reason,
		})
	}
	return out
}

func priceUpdatesFromSnapshot(updates []whtnt.PriceUpdate) []engine.PriceUpdate {
	if len(updates) == 0 {
		return nil
	}
	out := make([]engine.PriceUpdate, 0, len(updates))
	for _, update := range updates {
		out = append(out, engine.PriceUpdate{
			ProductName:      update.ProductName,
			ProductListingID: update.ProductListingID,
			OldPrice:         update.OldPrice,
			NewPrice:         update.NewPrice,
			MaxPrice:         update.MaxPrice,
		})
	}
	return out
}

func checkoutFullName(firstName string, addressName string, accountLastName string) string {
	firstName = strings.TrimSpace(firstName)
	lastName := firstNonEmpty(addressName, accountLastName)
	if strings.TrimSpace(lastName) == "" {
		return ""
	}
	if strings.Contains(strings.TrimSpace(lastName), " ") {
		return strings.TrimSpace(lastName)
	}
	if firstName == "" {
		return ""
	}
	return strings.TrimSpace(firstName + " " + lastName)
}

func parsePreparedProducts(productsJSON string, rewardLabel string) ([]whtnt.Product, error) {
	trimmed := strings.TrimSpace(productsJSON)
	if trimmed == "" {
		return nil, fmt.Errorf("products_json is required")
	}
	var products []whtnt.Product
	if err := json.Unmarshal([]byte(trimmed), &products); err == nil {
		return validatePreparedProducts(products)
	}
	var prizeBaskets map[string][][]whtnt.Product
	if err := json.Unmarshal([]byte(trimmed), &prizeBaskets); err == nil && len(prizeBaskets) > 0 {
		key := firstNonEmpty(rewardLabel, "£10")
		baskets := prizeBaskets[key]
		if len(baskets) == 0 {
			for basketKey, value := range prizeBaskets {
				if currencyDigits(basketKey) == currencyDigits(key) {
					baskets = value
					break
				}
			}
		}
		if len(baskets) == 0 {
			return nil, fmt.Errorf("products_json has no basket for reward %q", key)
		}
		return validatePreparedProducts(baskets[0])
	}
	var baskets [][]whtnt.Product
	if err := json.Unmarshal([]byte(trimmed), &baskets); err != nil {
		return nil, fmt.Errorf("parse products_json: %w", err)
	}
	if len(baskets) == 0 {
		return nil, fmt.Errorf("products_json has no baskets")
	}
	return validatePreparedProducts(baskets[0])
}

func validatePreparedProducts(products []whtnt.Product) ([]whtnt.Product, error) {
	if len(products) == 0 {
		return nil, fmt.Errorf("products_json has no products")
	}
	for index := range products {
		products[index].ProductName = strings.TrimSpace(products[index].ProductName)
		products[index].ProductListingID = strings.TrimSpace(products[index].ProductListingID)
		products[index].ProductID = strings.TrimSpace(products[index].ProductID)
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
		products[index].InStock = true
	}
	return products, nil
}

func preparedProductsTotal(products []whtnt.Product) int {
	total := 0
	for _, product := range products {
		total += product.ProductPrice
	}
	return total
}

func formatMinorGBP(amount int) string {
	return fmt.Sprintf("GBP %.2f", float64(amount)/100)
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

func checkoutProductForOrder(products []whtnt.Product, index int) (whtnt.Product, bool) {
	if index < 0 || index >= len(products) {
		return whtnt.Product{}, false
	}
	return products[index], true
}

func checkoutProductMetadata(product whtnt.Product) map[string]any {
	return map[string]any{
		"items":      []string{product.ProductName},
		"listing_id": product.ProductListingID,
		"product_id": product.ProductID,
		"subtotal":   fmt.Sprintf("%d GBP", product.ProductPrice),
		"total":      fmt.Sprintf("%d GBP", product.ProductPrice),
	}
}

func (Whatnot) ListOrders(ctx context.Context, input engine.AccountOrdersInput) ([]engine.OrderCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return nil, err
	}
	defer module.Close()

	statuses, err := module.ListOrderSnapshots()
	if err != nil {
		return nil, err
	}
	out := make([]engine.OrderCheckResult, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, orderCheckResultFromStatus(status))
	}
	return out, nil
}

func (Whatnot) ListReviewOrders(ctx context.Context, input engine.AccountOrdersInput) ([]engine.OrderCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return nil, err
	}
	defer module.Close()

	statuses, err := module.ListReviewOrderSnapshots()
	if err != nil {
		return nil, err
	}
	out := make([]engine.OrderCheckResult, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, orderCheckResultFromStatus(status))
	}
	return out, nil
}

func (Whatnot) CheckOrder(ctx context.Context, input engine.OrderCheckInput) (engine.OrderCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.OrderCheckResult{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.OrderCheckResult{}, err
	}
	defer module.Close()

	status, err := module.CheckOrderSnapshot(firstNonEmpty(input.Order.ExternalOrderID, input.Order.ID))
	if err != nil {
		return engine.OrderCheckResult{}, err
	}
	return orderCheckResultFromStatus(status), nil
}

func (Whatnot) ListConversations(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountMessageSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return nil, err
	}
	defer module.Close()

	snapshots, err := module.ListConversationSnapshots()
	if err != nil {
		return nil, err
	}
	out := make([]engine.AccountMessageSummary, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, engine.AccountMessageSummary{
			ConversationID: snapshot.ID,
			Username:       snapshot.Username,
			DisplayName:    snapshot.DisplayName,
			Unread:         snapshot.Unread,
			UnreadCount:    boolInt(snapshot.Unread),
			Preview:        snapshot.Preview,
			LinkedOrder:    snapshot.LinkedOrder,
			LinkedItem:     snapshot.LinkedItem,
			LastMessageAt:  snapshot.LastMessageAt,
		})
	}
	return out, nil
}

func (Whatnot) GetConversation(ctx context.Context, input engine.AccountOrdersInput, conversationID string) (engine.AccountConversation, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountConversation{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.AccountConversation{}, err
	}
	defer module.Close()

	snapshot, err := module.GetConversationSnapshot(conversationID)
	if err != nil {
		return engine.AccountConversation{}, err
	}
	out := engine.AccountConversation{
		ConversationID: snapshot.ID,
		Username:       snapshot.Username,
		LinkedOrder:    snapshot.LinkedOrder,
		LinkedItem:     snapshot.LinkedItem,
		Messages:       make([]engine.AccountMessage, 0, len(snapshot.Messages)),
	}
	for _, message := range snapshot.Messages {
		out.Messages = append(out.Messages, engine.AccountMessage{
			From:       message.From,
			Body:       message.Body,
			SentAt:     message.SentAt,
			OwnMessage: message.OwnMessage,
			MessageID:  message.ID,
		})
	}
	return out, nil
}

func (Whatnot) ListNotifications(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountNotification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return nil, err
	}
	defer module.Close()

	snapshots, err := module.ListNotificationSnapshots()
	if err != nil {
		return nil, err
	}
	out := make([]engine.AccountNotification, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, engine.AccountNotification{
			ID:             snapshot.ID,
			ActivityID:     snapshot.ActivityID,
			ActivityType:   snapshot.ActivityType,
			Message:        snapshot.Message,
			CreatedAt:      snapshot.CreatedAt,
			Unread:         snapshot.Unread,
			DeepLink:       snapshot.DeepLink,
			PrimaryActor:   snapshot.PrimaryActor,
			RelatedListing: snapshot.RelatedListing,
		})
	}
	return out, nil
}

func (Whatnot) MarkNotificationsViewed(ctx context.Context, input engine.AccountOrdersInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return err
	}
	defer module.Close()
	return module.MarkNotificationsHubViewed()
}

func (Whatnot) AcknowledgeNotification(ctx context.Context, input engine.AccountOrdersInput, notificationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return err
	}
	defer module.Close()
	return module.AcknowledgeNotificationFeedNode(notificationID)
}

func (Whatnot) SendMessage(ctx context.Context, input engine.AccountOrdersInput, request engine.AccountMessageRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return err
	}
	defer module.Close()

	participantID := ""
	if strings.TrimSpace(request.ConversationID) == "" {
		if username := strings.TrimSpace(request.Username); username != "" {
			participantID, err = module.UserIDForUsername(username)
			if err != nil {
				return err
			}
		}
	}
	return module.SendMessageSnapshot(request.ConversationID, participantID, request.Body)
}

func (Whatnot) FollowUser(ctx context.Context, input engine.AccountOrdersInput, username string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return err
	}
	defer module.Close()
	return module.FollowUserByUsername(username)
}

func (Whatnot) FollowListingSellers(ctx context.Context, input engine.AccountOrdersInput, listingIDs []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return 0, err
	}
	defer module.Close()
	return module.FollowListingSellers(listingIDs)
}

func (Whatnot) ListAddresses(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountAddress, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return nil, err
	}
	defer module.Close()
	snapshots, err := module.ListAddressSnapshots()
	if err != nil {
		return nil, err
	}
	out := make([]engine.AccountAddress, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, addressFromSnapshot(snapshot))
	}
	return out, nil
}

func (Whatnot) CreateAddress(ctx context.Context, input engine.AccountOrdersInput, address engine.AccountAddressInput) (engine.AccountAddress, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountAddress{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.AccountAddress{}, err
	}
	defer module.Close()
	snapshot, err := module.CreateAddressSnapshot(addressSnapshotFromInput(address))
	if err != nil {
		return engine.AccountAddress{}, err
	}
	return addressFromSnapshot(snapshot), nil
}

func (Whatnot) UpdateAddress(ctx context.Context, input engine.AccountOrdersInput, address engine.AccountAddressInput) (engine.AccountAddress, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountAddress{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.AccountAddress{}, err
	}
	defer module.Close()
	snapshot, err := module.UpdateAddressSnapshot(addressSnapshotFromInput(address))
	if err != nil {
		return engine.AccountAddress{}, err
	}
	return addressFromSnapshot(snapshot), nil
}

func (Whatnot) DeleteAddress(ctx context.Context, input engine.AccountOrdersInput, addressID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return err
	}
	defer module.Close()
	return module.DeleteAddressSnapshot(addressID)
}

func (Whatnot) ListPaymentMethods(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountPaymentMethod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return nil, err
	}
	defer module.Close()
	snapshots, err := module.ListPaymentMethodSnapshots()
	if err != nil {
		return nil, err
	}
	out := make([]engine.AccountPaymentMethod, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, paymentFromSnapshot(snapshot))
	}
	return out, nil
}

func (Whatnot) AddPaymentMethod(ctx context.Context, input engine.AccountOrdersInput, payment engine.AccountPaymentInput) (engine.AccountPaymentMethod, engine.SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountPaymentMethod{}, engine.SessionRecord{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.AccountPaymentMethod{}, engine.SessionRecord{}, err
	}
	defer module.Close()
	snapshot, sessionSnapshot, err := module.AddPaymentMethodSnapshot(whtnt.CardDetails{
		Number:   payment.CardNumber,
		CVC:      payment.CardCVC,
		ExpMonth: payment.ExpMonth,
		ExpYear:  payment.ExpYear,
		Postcode: payment.Postcode,
	})
	if err != nil {
		return engine.AccountPaymentMethod{}, engine.SessionRecord{}, err
	}
	session := sessionFromSnapshot(sessionSnapshot)
	session.ID = input.Session.ID
	session.AccountID = input.Session.AccountID
	session.CreatedAt = input.Session.CreatedAt
	session.UpdatedAt = flowNow()
	return paymentFromSnapshot(snapshot), session, nil
}

func (Whatnot) DeletePaymentMethod(ctx context.Context, input engine.AccountOrdersInput, paymentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return err
	}
	defer module.Close()
	return module.DeletePaymentMethodSnapshot(paymentID)
}

func addressSnapshotFromInput(input engine.AccountAddressInput) whtnt.AddressSnapshot {
	return whtnt.AddressSnapshot{
		ID:              input.ID,
		FullName:        input.FullName,
		Line1:           input.Line1,
		Line2:           input.Line2,
		City:            input.City,
		State:           input.State,
		PostalCode:      input.PostalCode,
		CountryCode:     input.CountryCode,
		DefaultShipping: input.DefaultShipping,
		ReturnAddress:   input.ReturnAddress,
	}
}

func addressFromSnapshot(snapshot whtnt.AddressSnapshot) engine.AccountAddress {
	return engine.AccountAddress{
		ID:              snapshot.ID,
		FullName:        snapshot.FullName,
		Line1:           snapshot.Line1,
		Line2:           snapshot.Line2,
		City:            snapshot.City,
		State:           snapshot.State,
		PostalCode:      snapshot.PostalCode,
		CountryCode:     snapshot.CountryCode,
		DefaultShipping: snapshot.DefaultShipping,
		ReturnAddress:   snapshot.ReturnAddress,
	}
}

func paymentFromSnapshot(snapshot whtnt.PaymentMethodSnapshot) engine.AccountPaymentMethod {
	return engine.AccountPaymentMethod{
		ID:              snapshot.ID,
		CardDescription: snapshot.CardDescription,
		CardType:        snapshot.CardType,
		Gateway:         snapshot.Gateway,
		Default:         snapshot.Default,
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func orderCheckResultFromStatus(status whtnt.OrderStatusSnapshot) engine.OrderCheckResult {
	metadata := map[string]any{}
	for key, value := range status.Metadata {
		metadata[key] = value
	}
	if status.ReviewShipmentID != "" {
		metadata["review_shipment_id"] = status.ReviewShipmentID
	}
	if status.TrackingURL != "" {
		metadata["tracking_url"] = status.TrackingURL
	}
	if status.ShippingService != "" {
		metadata["shipping_service"] = status.ShippingService
	}
	if len(status.ItemNames) > 0 {
		metadata["items"] = status.ItemNames
	}
	if status.Subtotal != "" {
		metadata["subtotal"] = status.Subtotal
	}
	if status.Shipping != "" {
		metadata["shipping"] = status.Shipping
	}
	if status.Total != "" {
		metadata["total"] = status.Total
	}
	if status.DisplayID != "" {
		metadata["display_id"] = status.DisplayID
	}
	if status.PrettyStatus != "" {
		metadata["pretty_status"] = status.PrettyStatus
	}
	return engine.OrderCheckResult{
		TrackingNumber:    status.TrackingNumber,
		ReadyForReview:    status.ReadyForReview,
		ReviewReferenceID: status.ReviewShipmentID,
		ExternalOrderID:   status.ExternalOrderID,
		Metadata:          metadata,
	}
}

func (Whatnot) LeaveReview(ctx context.Context, input engine.ReviewInput) (engine.ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.ReviewResult{}, err
	}
	shipmentID := firstNonEmpty(input.Order.ReviewReferenceID, metadataString(input.Order.Metadata, "review_shipment_id"))
	if shipmentID == "" {
		return engine.ReviewResult{}, fmt.Errorf("review is ready but no shipment id has been stored; check order flow must provide it")
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(input.Session))
	if err != nil {
		return engine.ReviewResult{}, err
	}
	defer module.Close()

	result, err := module.SubmitReviewWithRatingsSnapshot(shipmentID, input.Text, input.Ratings)
	if err != nil {
		return engine.ReviewResult{}, err
	}
	session := engine.SessionRecord{}
	if result.Session.ID != "" || len(result.Session.Cookies) > 0 {
		session = sessionFromSnapshot(result.Session)
		session.ID = input.Session.ID
		session.AccountID = input.Session.AccountID
		session.CreatedAt = input.Session.CreatedAt
		session.UpdatedAt = flowNow()
	}
	return engine.ReviewResult{
		NewUsername:      result.NewUsername,
		AccountsFollowed: result.AccountsFollowed,
		Session:          session,
	}, nil
}

func (Whatnot) RefreshSession(ctx context.Context, session engine.SessionRecord) (engine.SessionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return engine.SessionRecord{}, false, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(session))
	if err != nil {
		return engine.SessionRecord{}, false, err
	}
	defer module.Close()

	refreshed, healthy, err := module.RefreshSessionSnapshot()
	if err != nil || !healthy {
		return session, healthy, err
	}
	out := sessionFromSnapshot(refreshed)
	out.ID = session.ID
	out.AccountID = session.AccountID
	out.CreatedAt = session.CreatedAt
	out.UpdatedAt = flowNow()
	return out, true, nil
}

func (Whatnot) RefreshSessionWithoutCredits(ctx context.Context, session engine.SessionRecord) (engine.SessionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return engine.SessionRecord{}, false, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(session))
	if err != nil {
		return engine.SessionRecord{}, false, err
	}
	defer module.Close()

	refreshed, healthy, err := module.RefreshSessionSnapshotWithoutCredits()
	if err != nil || !healthy {
		return session, healthy, err
	}
	out := sessionFromSnapshot(refreshed)
	out.ID = session.ID
	out.AccountID = session.AccountID
	out.CreatedAt = session.CreatedAt
	out.UpdatedAt = flowNow()
	return out, true, nil
}

func (Whatnot) CheckMasterHealth(ctx context.Context, master engine.MasterAccount, session engine.SessionRecord) (engine.SessionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return engine.SessionRecord{}, false, err
	}
	return Whatnot{}.RefreshSessionWithoutCredits(ctx, session)
}

func (Whatnot) MasterCheckout(ctx context.Context, master engine.MasterAccount, session engine.SessionRecord, request engine.MasterCheckoutRequest) (engine.CheckoutResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.CheckoutResult{}, err
	}
	module, err := whtnt.NewSessionModule(snapshotFromSession(session))
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	defer module.Close()

	price, err := parseMinorUnits(request.Price)
	if err != nil {
		return engine.CheckoutResult{}, err
	}
	checkout, err := module.CheckoutSpecificProduct(whtnt.Product{
		ProductName:      firstNonEmpty(request.ProductIdentifier, request.ListingID, request.ProductID),
		ProductListingID: request.ListingID,
		ProductID:        request.ProductID,
		ProductPrice:     price,
		InStock:          true,
	}, whtnt.CardDetails{
		Number:   request.CardNumber,
		CVC:      request.CardCVC,
		ExpMonth: request.CardExpMonth,
		ExpYear:  request.CardExpYear,
		Postcode: request.CardPostcode,
	})
	if err != nil {
		return engine.CheckoutResult{}, err
	}

	now := flowNow()
	orderID := checkout.OrderID
	if orderID == "" {
		return engine.CheckoutResult{}, fmt.Errorf("master checkout succeeded but no order id was exposed")
	}
	return engine.CheckoutResult{
		Order: engine.Order{
			ID:              "master_order_" + orderID,
			AccountID:       master.AccountID,
			ExternalOrderID: orderID,
			State:           engine.OrderStateCreated,
			CreatedAt:       now,
			UpdatedAt:       now,
			Metadata:        checkoutMetadata(checkout.Address, checkout.Card),
		},
		ShippingFirstName: checkout.ShippingFirstName,
		ShippingLastName:  checkout.ShippingLastName,
	}, nil
}

func mergeCheckoutMetadata(metadata map[string]any, address whtnt.AddressFields, card whtnt.CardDetails) {
	if metadata == nil {
		return
	}
	for key, value := range checkoutMetadata(address, card) {
		metadata[key] = value
	}
}

func mergeSessionCreditMetadata(metadata map[string]any, session engine.SessionRecord) {
	if metadata == nil || session.Metadata == nil {
		return
	}
	if value, ok := session.Metadata["reward_balance_amount_safe"]; ok {
		metadata["reward_balance_amount_safe"] = value
	}
	if currency := metadataString(session.Metadata, "reward_balance_currency"); currency != "" {
		metadata["reward_balance_currency"] = currency
	}
}

func checkoutMetadata(address whtnt.AddressFields, card whtnt.CardDetails) map[string]any {
	metadata := map[string]any{}
	if strings.TrimSpace(address.AddressLine1) != "" {
		metadata["shipping_address_line1"] = strings.TrimSpace(address.AddressLine1)
		metadata["shipping_address_line2"] = strings.TrimSpace(address.AddressLine2)
		metadata["shipping_town"] = strings.TrimSpace(address.Town)
		metadata["shipping_postcode"] = strings.TrimSpace(address.Postcode)
		metadata["shipping_last_name"] = strings.TrimSpace(address.LastName)
		metadata["shipping_address_full"] = joinNonEmptyFlow([]string{
			address.AddressLine1,
			address.AddressLine2,
			address.Town,
			address.Postcode,
		}, ", ")
	}
	last4 := cardLast4(card.Number)
	if last4 != "" {
		metadata["card_last4"] = last4
		metadata["card_expiry"] = strings.TrimSpace(card.ExpMonth) + "/" + strings.TrimSpace(card.ExpYear)
		metadata["card_postcode"] = strings.TrimSpace(card.Postcode)
		metadata["card_hidden_discord"] = fmt.Sprintf("||%s exp %s postcode %s||", maskedCardDisplay(card.Number), metadata["card_expiry"], firstNonEmpty(card.Postcode, "-"))
	}
	return metadata
}

func cardLast4(number string) string {
	digits := onlyDigits(number)
	if len(digits) <= 4 {
		return digits
	}
	return digits[len(digits)-4:]
}

func maskedCardDisplay(number string) string {
	last4 := cardLast4(number)
	if last4 == "" {
		return "card hidden"
	}
	return "**** **** **** " + last4
}

func onlyDigits(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if char >= '0' && char <= '9' {
			builder.WriteRune(char)
		}
	}
	return builder.String()
}

func joinNonEmptyFlow(parts []string, separator string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return strings.Join(out, separator)
}

func sessionFromSnapshot(snapshot whtnt.SessionSnapshot) engine.SessionRecord {
	cookies := make([]engine.CookieRecord, 0, len(snapshot.Cookies))
	for _, cookie := range snapshot.Cookies {
		cookies = append(cookies, engine.CookieRecord{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HttpOnly,
			SameSite: cookie.SameSite,
		})
	}
	metadata := snapshot.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["whtnt_session_id"] = snapshot.SessionID
	metadata["whtnt_app_session_id"] = snapshot.AppSessionID
	metadata["whtnt_request_id"] = snapshot.RequestID
	metadata["whtnt_session_token"] = snapshot.SessionToken
	return engine.SessionRecord{
		ID:        snapshot.ID,
		AccountID: snapshot.AccountID,
		Cookies:   cookies,
		Metadata:  metadata,
	}
}

func snapshotFromSession(session engine.SessionRecord) whtnt.SessionSnapshot {
	cookies := make([]whtnt.CookieJSON, 0, len(session.Cookies))
	for _, cookie := range session.Cookies {
		cookies = append(cookies, whtnt.CookieJSON{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Expires:  cookie.Expires,
			Secure:   cookie.Secure,
			HttpOnly: cookie.HTTPOnly,
			SameSite: cookie.SameSite,
		})
	}
	return whtnt.SessionSnapshot{
		ID:           session.ID,
		AccountID:    session.AccountID,
		SessionID:    metadataString(session.Metadata, "whtnt_session_id"),
		AppSessionID: metadataString(session.Metadata, "whtnt_app_session_id"),
		RequestID:    metadataString(session.Metadata, "whtnt_request_id"),
		SessionToken: metadataString(session.Metadata, "whtnt_session_token"),
		Cookies:      cookies,
		Metadata:     session.Metadata,
	}
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if metadata == nil {
		return nil
	}
	switch value := metadata[key].(type) {
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
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseMinorUnits(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("price is required")
	}
	if strings.Contains(trimmed, ".") {
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0, err
		}
		return int(parsed * 100), nil
	}
	return strconv.Atoi(trimmed)
}
