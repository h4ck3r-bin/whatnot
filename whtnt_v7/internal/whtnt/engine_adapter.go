package whtnt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/logger"
)

type AccountSnapshot struct {
	ID        string
	Username  string
	Email     string
	Password  string
	FirstName string
	LastName  string
	IP        string
	Proxy     string
}

type SessionSnapshot struct {
	ID           string
	AccountID    string
	SessionID    string
	AppSessionID string
	RequestID    string
	SessionToken string
	Cookies      []CookieJSON
	Metadata     map[string]any
}

type CheckoutSnapshot struct {
	OrderID           string
	OrderIDs          []string
	Username          string
	Products          []Product
	PriceUpdates      []PriceUpdate
	ProductIssues     []ProductIssue
	CheckoutAttempted bool
	ShippingFirstName string
	ShippingLastName  string
	Address           AddressFields
	Card              CardDetails
}

type PriceUpdate struct {
	ProductName      string
	ProductListingID string
	OldPrice         int
	NewPrice         int
	MaxPrice         int
}

type ProductIssue struct {
	ProductName      string
	ProductListingID string
	Reason           string
}

type OrderStatusSnapshot struct {
	TrackingNumber   string
	TrackingURL      string
	ShippingService  string
	ItemNames        []string
	Subtotal         string
	Shipping         string
	Total            string
	DisplayID        string
	PrettyStatus     string
	ReadyForReview   bool
	ReviewShipmentID string
	ExternalOrderID  string
	Metadata         map[string]any
}

func (m *Module) SetReferralCode(code string) {
	m.referralCode = strings.TrimSpace(code)
}

func (m *Module) SetRewardDetectedHook(hook func(AccountSnapshot, SessionSnapshot) error) {
	m.rewardDetectedHook = hook
}

func (m *Module) SetCheckoutOrderHook(hook func(CheckoutSnapshot)) {
	m.checkoutOrderHook = hook
}

func (m *Module) AccountSnapshot() AccountSnapshot {
	return AccountSnapshot{
		ID:        m.accountIDFromCookies(),
		Username:  m.account.username,
		Email:     m.account.email,
		Password:  m.account.password,
		FirstName: m.account.firstName,
		LastName:  m.account.lastName,
		IP:        m.ip,
		Proxy:     m.proxy,
	}
}

func (m *Module) CheckoutSnapshot() CheckoutSnapshot {
	return CheckoutSnapshot{
		OrderID:           m.lastOrderID,
		OrderIDs:          append([]string(nil), m.lastOrderIDs...),
		Username:          m.account.username,
		Products:          append([]Product(nil), m.lastOrderProducts...),
		PriceUpdates:      append([]PriceUpdate(nil), m.priceUpdates...),
		ProductIssues:     append([]ProductIssue(nil), m.productIssues...),
		CheckoutAttempted: m.checkoutAttempted,
		ShippingFirstName: m.account.firstName,
		ShippingLastName:  m.lastAddress.LastName,
		Address:           m.lastAddress,
		Card:              m.lastCard,
	}
}

func (m *Module) SessionSnapshot() (SessionSnapshot, error) {
	cookies, err := m.snapshotCookies()
	if err != nil {
		return SessionSnapshot{}, err
	}

	sessionToken := ""
	if len(m.usedSeonSessionTokens) > 0 {
		sessionToken = m.usedSeonSessionTokens[0]
	}

	accountID := m.accountIDFromCookies()
	seonMachine, seonIOSVersion, seonDeviceName := m.seonDeviceMetadata()
	accessToken := firstNonEmptyString(m.accessToken, accessTokenFromCookieJSON(cookies))
	return SessionSnapshot{
		ID:           stableSessionID(accountID, m.account.email),
		AccountID:    accountID,
		SessionID:    m.session.sessionID,
		AppSessionID: m.session.appSessionID,
		RequestID:    m.session.requestID,
		SessionToken: sessionToken,
		Cookies:      cookies,
		Metadata: map[string]any{
			"email":                                m.account.email,
			"username":                             m.account.username,
			"username_adequate":                    checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName),
			"username_adequate_checked_at":         formatRefreshTime(m.usernameAdequateCheckedAt),
			"first_name":                           m.account.firstName,
			"last_name":                            m.account.lastName,
			"proxy":                                m.proxy,
			"ip":                                   m.ip,
			"checkout_order":                       m.lastOrderID,
			"checkout_orders":                      m.lastOrderIDs,
			"checkout_products":                    m.lastOrderProducts,
			"address":                              m.lastAddress,
			"shipping_created":                     m.shippingAddressCreated,
			"card":                                 m.lastCard,
			"access_token":                         accessToken,
			"sms_provider":                         m.lastSMSProvider,
			"sms_activation_id":                    m.lastSMSActivationID,
			"sms_phone_number":                     m.lastSMSPhoneNumber,
			"sms_code":                             m.lastSMSCode,
			"last_session_refresh_at":              formatRefreshTime(m.lastSessionRefreshAt),
			"payment_method_id":                    m.lastPaymentID,
			"payment_method_card_last4":            m.lastPaymentLast4,
			"completed_requests":                   m.completedRequests,
			"referral_amount":                      m.referralAmountSafe,
			"reward_balance_amount_safe":           m.rewardBalanceAmountSafe,
			"reward_balance_currency":              m.rewardBalanceCurrency,
			"reward_balance_last_seen_amount_safe": m.rewardBalanceAmountSafe,
			"reward_balance_last_seen_currency":    m.rewardBalanceCurrency,
			"reward_credit_check_error":            m.rewardCreditCheckError,
			"reward_credit_checked_at":             time.Now().UTC().Format(time.RFC3339),
			"seon_machine":                         seonMachine,
			"seon_ios_version":                     seonIOSVersion,
			"seon_device_name":                     seonDeviceName,
		},
	}, nil
}

func NewSessionModule(snapshot SessionSnapshot) (*Module, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return nil, err
	}
	module := &Module{
		config:   cfg,
		platform: PlatformIOS,
		client:   mustNewHTTPSession(PlatformIOS),
		log:      logger.New(logger.NextID()),
		session: sessionCreateResult{
			sessionID:    snapshot.SessionID,
			appSessionID: snapshot.AppSessionID,
			requestID:    snapshot.RequestID,
		},
	}
	if err := module.LoadSessionSnapshot(snapshot); err != nil {
		module.Close()
		return nil, err
	}
	return module, nil
}

func (m *Module) LoadSessionSnapshot(snapshot SessionSnapshot) error {
	m.session = sessionCreateResult{
		sessionID:    snapshot.SessionID,
		appSessionID: snapshot.AppSessionID,
		requestID:    snapshot.RequestID,
	}
	m.usedSeonSessionTokens = nil
	if snapshot.SessionToken != "" {
		m.usedSeonSessionTokens = []string{snapshot.SessionToken}
	}
	m.account.email = metadataString(snapshot.Metadata, "email")
	m.account.username = metadataString(snapshot.Metadata, "username")
	m.usernameAdequate = metadataBool(snapshot.Metadata, "username_adequate")
	m.usernameAdequateCheckedAt = metadataTime(snapshot.Metadata, "username_adequate_checked_at")
	m.account.firstName = metadataString(snapshot.Metadata, "first_name")
	m.account.lastName = metadataString(snapshot.Metadata, "last_name")
	m.fastProxyAuthRetry = metadataBool(snapshot.Metadata, "fast_proxy_auth_retry")
	m.proxy = metadataString(snapshot.Metadata, "proxy")
	m.ip = metadataString(snapshot.Metadata, "ip")
	m.accessToken = metadataString(snapshot.Metadata, "access_token")
	m.lastSMSProvider = metadataString(snapshot.Metadata, "sms_provider")
	m.lastSMSActivationID = metadataString(snapshot.Metadata, "sms_activation_id")
	m.lastSMSPhoneNumber = metadataString(snapshot.Metadata, "sms_phone_number")
	m.lastSMSCode = metadataString(snapshot.Metadata, "sms_code")
	m.lastSessionRefreshAt = metadataTime(snapshot.Metadata, "last_session_refresh_at")
	m.referralAmountSafe = metadataInt(snapshot.Metadata, "referral_amount")
	m.rewardBalanceAmountSafe = metadataInt(snapshot.Metadata, "reward_balance_amount_safe")
	m.rewardBalanceCurrency = metadataString(snapshot.Metadata, "reward_balance_currency")
	if m.client != nil {
		m.proxy = normalizeProxyURL(m.proxy)
		_ = m.client.SetProxy(m.proxy)
	}
	m.lastOrderID = metadataString(snapshot.Metadata, "checkout_order")
	m.lastOrderIDs = metadataStringSlice(snapshot.Metadata, "checkout_orders")
	m.lastPaymentID = metadataString(snapshot.Metadata, "payment_method_id")
	m.lastPaymentLast4 = metadataString(snapshot.Metadata, "payment_method_card_last4")
	m.shippingAddressCreated = metadataBool(snapshot.Metadata, "shipping_created")
	m.completedRequests = metadataBoolMap(snapshot.Metadata, "completed_requests")
	m.loadProductMetadata(snapshot.Metadata)
	m.loadAddressMetadata(snapshot.Metadata)
	m.loadCardMetadata(snapshot.Metadata)
	m.restoreSeonDevice(snapshot.Metadata)

	for _, entry := range snapshot.Cookies {
		if err := m.setCookieJSON(entry); err != nil {
			return err
		}
		if entry.Name == accessTokenCookieName && strings.TrimSpace(entry.Value) != "" {
			m.accessToken = strings.TrimSpace(entry.Value)
		}
	}
	m.lastSessionFingerprint = sessionRecordFingerprint(sessionRecordFromSnapshot(snapshot, time.Now()))
	return nil
}

func (m *Module) RefreshSessionSnapshot() (SessionSnapshot, bool, error) {
	m.refreshingSession = true
	defer func() { m.refreshingSession = false }()
	valid, err := m.checkIfAccountValid(m.session)
	if err != nil || !valid {
		return SessionSnapshot{}, valid, err
	}
	credits, creditErr := m.GetMyCreditsSnapshot()
	if creditErr != nil {
		m.rewardCreditCheckError = creditErr.Error()
		if m.log != nil {
			m.log.ManagerError("Reward credits check failed: " + creditErr.Error())
		}
	} else {
		m.rewardBalanceAmountSafe = credits.AmountSafe
		m.rewardBalanceCurrency = credits.Currency
		m.rewardCreditCheckError = ""
	}
	snapshot, err := m.SessionSnapshot()
	return snapshot, true, err
}

func (m *Module) RefreshSessionSnapshotWithoutCredits() (SessionSnapshot, bool, error) {
	m.refreshingSession = true
	defer func() { m.refreshingSession = false }()
	valid, err := m.checkIfAccountValid(m.session)
	if err != nil || !valid {
		return SessionSnapshot{}, valid, err
	}
	snapshot, err := m.SessionSnapshot()
	return snapshot, true, err
}

func (m *Module) CheckOrderSnapshot(orderID string) (OrderStatusSnapshot, error) {
	orderID = strings.TrimSpace(orderID)
	for _, ref := range uniqueOrderRefs(orderID, strings.TrimPrefix(orderID, "order_"), encodedOrderNodeRef(orderID)) {
		detail, err := m.getOrderDetailBuyerSnapshot(ref)
		if err == nil {
			status := orderStatusFromDetail(detail, ref)
			return status, nil
		}
	}

	orders, err := m.ListOrderSnapshots()
	if err != nil {
		return OrderStatusSnapshot{}, err
	}
	for _, order := range orders {
		if orderID == "" ||
			order.ExternalOrderID == orderID ||
			order.MetadataString("encoded_id") == orderID ||
			order.MetadataString("uuid") == orderID ||
			order.MetadataString("display_id") == orderID ||
			strings.TrimPrefix(order.MetadataString("encoded_id"), "order_") == strings.TrimPrefix(orderID, "order_") {
			return order, nil
		}
	}
	return OrderStatusSnapshot{}, fmt.Errorf("order %s was not found in purchases", orderID)
}

func encodedOrderNodeRef(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "order_"))
	if value == "" {
		return ""
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return ""
		}
	}
	return base64.StdEncoding.EncodeToString([]byte("OrderNode:" + value))
}

func uniqueOrderRefs(values ...string) []string {
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}

func orderStatusFromDetail(detail orderDetailSnapshot, requestedID string) OrderStatusSnapshot {
	status := OrderStatusSnapshot{
		TrackingNumber:   firstTrackingCode(detail.Shipments),
		TrackingURL:      firstTrackingURL(detail.Shipments),
		ShippingService:  firstShippingService(detail.Shipments),
		ItemNames:        detail.Items,
		Subtotal:         detail.Subtotal,
		Shipping:         detail.Shipping,
		Total:            detail.Total,
		DisplayID:        detail.DisplayID,
		PrettyStatus:     detail.PrettyStatus,
		ReadyForReview:   detail.IsReviewPending,
		ReviewShipmentID: firstShipmentID(detail.Shipments),
		ExternalOrderID:  firstNonEmptyString(detail.UUID, requestedID),
		Metadata: map[string]any{
			"encoded_id":       firstNonEmptyString(detail.ID, requestedID),
			"uuid":             firstNonEmptyString(detail.UUID, requestedID),
			"display_id":       detail.DisplayID,
			"pretty_status":    detail.PrettyStatus,
			"overview_status":  detail.PrettyStatus,
			"seller_username":  detail.SellerUsername,
			"listing_ids":      detail.ListingIDs,
			"conversation_id":  detail.ConversationID,
			"items":            detail.Items,
			"subtotal":         detail.Subtotal,
			"shipping":         detail.Shipping,
			"total":            detail.Total,
			"tracking_url":     firstTrackingURL(detail.Shipments),
			"shipping_service": firstShippingService(detail.Shipments),
		},
	}
	if status.ReadyForReview {
		status.Metadata["review_shipment_id"] = status.ReviewShipmentID
	}
	return status
}

func (m *Module) ListOrderSnapshots() ([]OrderStatusSnapshot, error) {
	purchases, err := m.getMyPurchases()
	if err != nil {
		return nil, err
	}

	orders := make([]OrderStatusSnapshot, 0, len(purchases))
	for _, purchase := range purchases {
		var detail orderDetailSnapshot
		if purchase.UUID != "" {
			detail, _ = m.getOrderDetailBuyerSnapshot(purchase.UUID)
		}
		listingIDs := firstStringSlice(detail.ListingIDs, purchase.ListingID)

		status := OrderStatusSnapshot{
			TrackingNumber:  firstTrackingCode(detail.Shipments),
			TrackingURL:     firstTrackingURL(detail.Shipments),
			ShippingService: firstShippingService(detail.Shipments),
			ItemNames:       firstStringSlice(detail.Items, purchase.ListingTitle),
			Subtotal:        detail.Subtotal,
			Shipping:        detail.Shipping,
			Total:           detail.Total,
			DisplayID:       detail.DisplayID,
			PrettyStatus:    firstNonEmptyString(detail.PrettyStatus, purchase.PrettyStatus, purchase.Status),
			ReadyForReview:  purchase.IsReviewPending || detail.IsReviewPending,
			ExternalOrderID: firstNonEmptyString(detail.UUID, purchase.UUID),
			Metadata: map[string]any{
				"encoded_id":       firstNonEmptyString(detail.ID, purchase.ID),
				"seller_username":  firstNonEmptyString(detail.SellerUsername, purchase.SellerUsername),
				"listing_id":       firstNonEmptyString(listingIDs...),
				"listing_ids":      listingIDs,
				"listing_title":    purchase.ListingTitle,
				"order_date":       purchase.CreatedAt,
				"overview_status":  purchase.Status,
				"pretty_status":    firstNonEmptyString(detail.PrettyStatus, purchase.PrettyStatus),
				"display_id":       detail.DisplayID,
				"support_chip":     purchase.SupportChip,
				"refund_status":    purchase.RefundStatus,
				"has_unread":       purchase.HasUnread,
				"conversation_id":  firstNonEmptyString(detail.ConversationID, purchase.ConversationID),
				"subtotal":         detail.Subtotal,
				"shipping":         detail.Shipping,
				"total":            detail.Total,
				"tracking_url":     firstTrackingURL(detail.Shipments),
				"shipping_service": firstShippingService(detail.Shipments),
				"items":            firstStringSlice(detail.Items, purchase.ListingTitle),
			},
		}
		if status.ReadyForReview {
			status.ReviewShipmentID = firstNonEmptyString(firstShipmentID(detail.Shipments), m.matchPendingReviewShipment(purchase))
			status.Metadata["review_shipment_id"] = status.ReviewShipmentID
			if status.ReviewShipmentID != "" {
				m.log.Order("Order is pending review.")
			}
		}
		orders = append(orders, status)
	}
	return orders, nil
}

func (m *Module) ListReviewOrderSnapshots() ([]OrderStatusSnapshot, error) {
	purchases, err := m.getMyPurchases()
	if err != nil {
		return nil, err
	}

	orders := make([]OrderStatusSnapshot, 0)
	for _, purchase := range purchases {
		if !purchaseLooksReviewPending(purchase) {
			continue
		}
		var detail orderDetailSnapshot
		if purchase.UUID != "" {
			detail, _ = m.getOrderDetailBuyerSnapshot(purchase.UUID)
		}
		listingIDs := firstStringSlice(detail.ListingIDs, purchase.ListingID)
		status := OrderStatusSnapshot{
			ItemNames:       firstStringSlice(detail.Items, purchase.ListingTitle),
			Subtotal:        detail.Subtotal,
			Shipping:        detail.Shipping,
			Total:           detail.Total,
			DisplayID:       detail.DisplayID,
			PrettyStatus:    firstNonEmptyString(detail.PrettyStatus, purchase.PrettyStatus, purchase.Status, "Pending Review"),
			ReadyForReview:  true,
			ExternalOrderID: firstNonEmptyString(detail.UUID, purchase.UUID),
			Metadata: map[string]any{
				"encoded_id":      firstNonEmptyString(detail.ID, purchase.ID),
				"seller_username": firstNonEmptyString(detail.SellerUsername, purchase.SellerUsername),
				"listing_id":      firstNonEmptyString(listingIDs...),
				"listing_ids":     listingIDs,
				"listing_title":   purchase.ListingTitle,
				"order_date":      purchase.CreatedAt,
				"overview_status": purchase.Status,
				"pretty_status":   firstNonEmptyString(detail.PrettyStatus, purchase.PrettyStatus, "Pending Review"),
				"display_id":      detail.DisplayID,
				"support_chip":    purchase.SupportChip,
				"refund_status":   purchase.RefundStatus,
				"has_unread":      purchase.HasUnread,
				"conversation_id": firstNonEmptyString(detail.ConversationID, purchase.ConversationID),
				"subtotal":        detail.Subtotal,
				"shipping":        detail.Shipping,
				"total":           detail.Total,
				"items":           firstStringSlice(detail.Items, purchase.ListingTitle),
			},
		}
		status.ReviewShipmentID = firstNonEmptyString(firstShipmentID(detail.Shipments), m.matchPendingReviewShipment(purchase))
		status.Metadata["review_shipment_id"] = status.ReviewShipmentID
		orders = append(orders, status)
	}
	return orders, nil
}

func purchaseLooksReviewPending(purchase purchaseOverview) bool {
	if purchase.IsReviewPending {
		return true
	}
	status := strings.ToLower(strings.Join([]string{purchase.Status, purchase.PrettyStatus, purchase.SupportChip}, " "))
	return strings.Contains(status, "pending review") || strings.Contains(status, "review pending") || strings.Contains(status, "review")
}

type ReviewSubmitSnapshot struct {
	NewUsername      string
	AccountsFollowed int
	Session          SessionSnapshot
}

func (m *Module) SubmitReviewWithRatingsSnapshot(shipmentID string, text string, ratings []int) (ReviewSubmitSnapshot, error) {
	if strings.TrimSpace(shipmentID) == "" {
		return ReviewSubmitSnapshot{}, fmt.Errorf("review shipment id is required")
	}
	return m.submitReviewWithRatings(shipmentID, text, ratings)
}

func (m *Module) CheckoutSpecificProduct(product Product, card CardDetails) (CheckoutSnapshot, error) {
	if strings.TrimSpace(m.lastAddress.AddressLine1) == "" || strings.TrimSpace(m.lastAddress.Postcode) == "" {
		return CheckoutSnapshot{}, fmt.Errorf("stored master session does not include enough address data for checkout")
	}
	m.overrideCard = &card
	defer func() { m.overrideCard = nil }()

	if err := m.runBasketCheckoutPrelude([]Product{product}); err != nil {
		return CheckoutSnapshot{}, err
	}
	paymentID, err := m.setupPaymentMethod()
	if err != nil {
		return CheckoutSnapshot{}, err
	}
	if err := m.runCheckoutPreOrderHygiene(); err != nil {
		return CheckoutSnapshot{}, err
	}
	if err := m.checkoutBasket(m.lastAddress, []Product{product}, paymentID); err != nil {
		return CheckoutSnapshot{}, err
	}
	return m.CheckoutSnapshot(), nil
}

func (m *Module) CheckoutPreparedProducts(products []Product, address AddressFields, card CardDetails) (CheckoutSnapshot, error) {
	if !hasCheckoutAddress(address) {
		return CheckoutSnapshot{}, fmt.Errorf("prepared checkout row does not include enough address data")
	}
	if len(products) == 0 {
		return CheckoutSnapshot{}, fmt.Errorf("prepared checkout row does not include products")
	}

	if err := m.ensureShippingAddressMatches(address); err != nil {
		return CheckoutSnapshot{}, err
	}

	m.overrideCard = &card
	defer func() { m.overrideCard = nil }()

	if err := m.runBasketCheckoutPrelude(products); err != nil {
		return CheckoutSnapshot{}, err
	}
	if !m.forceCheckout {
		if complete, matched, err := m.existingBasketOrdersSatisfied(products); err != nil {
			return CheckoutSnapshot{}, fmt.Errorf("pre-check basket orders: %w", err)
		} else if complete {
			m.log.Manager(fmt.Sprintf("Existing account orders already satisfy basket (%d/%d); skipping checkout.", matched, len(products)))
			return m.CheckoutSnapshot(), nil
		} else if matched > 0 {
			m.log.Manager(fmt.Sprintf("Existing account orders satisfy %d/%d basket item(s); continuing missing item(s).", matched, len(products)))
		}
	}
	paymentID := ""
	targetLast4 := cardLast4Utility(card.Number)
	if targetLast4 != "" {
		matchedPaymentID, matched, matchErr := m.findMatchingWhatnotPaymentMethod(card)
		if matchErr != nil {
			m.log.ManagerNotice("Could not verify stored payment method card last4: " + matchErr.Error())
		}
		if matched {
			paymentID = matchedPaymentID
			m.lastPaymentID = paymentID
			m.lastPaymentLast4 = targetLast4
			m.log.Manager("Stored payment method matches checkout card last4; continuing checkout.")
		} else {
			m.log.Manager("Stored payment method did not match checkout card last4; setting up checkout card.")
			var err error
			paymentID, err = m.setupPaymentMethodWithoutLiveLookup()
			if err != nil {
				return CheckoutSnapshot{}, err
			}
			m.lastPaymentID = paymentID
			m.lastPaymentLast4 = targetLast4
		}
	} else if strings.TrimSpace(m.lastPaymentID) != "" {
		paymentID = strings.TrimSpace(m.lastPaymentID)
		m.log.Manager("Resuming checkout with stored payment method.")
	} else {
		var err error
		paymentID, err = m.setupPaymentMethod()
		if err != nil {
			return CheckoutSnapshot{}, err
		}
		m.lastPaymentID = paymentID
	}
	if err := m.runCheckoutPreOrderHygiene(); err != nil {
		return CheckoutSnapshot{}, err
	}
	if err := m.checkoutBasket(address, products, paymentID); err != nil {
		return CheckoutSnapshot{}, err
	}
	m.log.Whatnot("Basket checkout completed.")
	return m.CheckoutSnapshot(), nil
}

func (m *Module) SetSkipDelays(skip bool) {
	m.skipDelays = skip
}

func (m *Module) SetForceCheckout(force bool) {
	m.forceCheckout = force
}

func (m *Module) SetAllowInsufficientBalance(allow bool) {
	m.allowInsufficientBalance = allow
}

func (m *Module) SetPaymentMethodID(paymentMethodID string) {
	paymentMethodID = strings.TrimSpace(paymentMethodID)
	if paymentMethodID == "" {
		return
	}
	m.lastPaymentID = paymentMethodID
}

func (m *Module) CheckoutGeneratedAccount() (CheckoutSnapshot, SessionSnapshot, error) {
	if len(m.lastOrderIDs) > 0 || strings.TrimSpace(m.lastOrderID) != "" {
		if m.forceCheckout {
			m.log.Manager("Stored checkout order found; manual checkout will refresh balance and continue.")
		} else {
			m.log.Manager("Stored checkout order found; refreshing session instead of placing again.")
		}
		refreshed, healthy, err := m.RefreshSessionSnapshot()
		if err != nil {
			return CheckoutSnapshot{}, SessionSnapshot{}, err
		}
		if !healthy {
			return CheckoutSnapshot{}, SessionSnapshot{}, fmt.Errorf("session became unhealthy after stored checkout")
		}
		if !m.forceCheckout {
			return m.CheckoutSnapshot(), refreshed, nil
		}
	}

	if err := m.handlePostOnboarding(); err != nil {
		return CheckoutSnapshot{}, SessionSnapshot{}, err
	}
	sessionToken := ""
	if len(m.usedSeonSessionTokens) > 0 {
		sessionToken = m.usedSeonSessionTokens[0]
	}
	if err := m.saveAccountToCsv(sessionToken); err != nil {
		return CheckoutSnapshot{}, SessionSnapshot{}, err
	}
	refreshed, healthy, err := m.RefreshSessionSnapshot()
	if err != nil {
		return CheckoutSnapshot{}, SessionSnapshot{}, err
	}
	if !healthy {
		return CheckoutSnapshot{}, SessionSnapshot{}, fmt.Errorf("session became unhealthy after checkout")
	}
	return m.CheckoutSnapshot(), refreshed, nil
}

func (m *Module) matchPendingReviewShipment(purchase purchaseOverview) string {
	reviews, err := m.getPendingReviews()
	if err != nil {
		return ""
	}
	for _, review := range reviews {
		if strings.EqualFold(review.SellerUsername, purchase.SellerUsername) {
			return review.ShipmentID
		}
		for _, item := range review.OrderItems {
			if strings.EqualFold(strings.TrimSpace(item.Listing.Title), strings.TrimSpace(purchase.ListingTitle)) {
				return review.ShipmentID
			}
		}
	}
	return ""
}

func (m *Module) snapshotCookies() ([]CookieJSON, error) {
	if m.client == nil || m.client.GetCookieJar() == nil {
		return nil, fmt.Errorf("cannot snapshot cookies: client session is closed")
	}
	parsed, err := url.Parse("https://" + WEB_HOST)
	if err != nil {
		return nil, err
	}
	rawCookies := m.client.GetCookieJar().Cookies(parsed)
	out := make([]CookieJSON, 0, len(rawCookies))
	for _, cookie := range rawCookies {
		out = append(out, cookieToJSON(cookie))
	}
	return out, nil
}

func (m *Module) setCookieJSON(entry CookieJSON) error {
	host := entry.Domain
	if host == "" {
		host = WEB_HOST
	}
	host = strings.TrimPrefix(host, ".")
	parsed, err := url.Parse("https://" + host)
	if err != nil {
		return err
	}
	cookie := &http.Cookie{
		Name:     entry.Name,
		Value:    entry.Value,
		Path:     normalizeCookiePath(entry.Path),
		Domain:   entry.Domain,
		MaxAge:   entry.MaxAge,
		Secure:   entry.Secure,
		HttpOnly: entry.HttpOnly,
	}
	if entry.Expires != "" {
		if expires, err := time.Parse(http.TimeFormat, entry.Expires); err == nil {
			cookie.Expires = expires
		}
	}
	m.client.GetCookieJar().SetCookies(parsed, []*http.Cookie{cookie})
	if entry.Name == accessTokenCookieName && strings.TrimSpace(entry.Value) != "" {
		m.accessToken = strings.TrimSpace(entry.Value)
	}
	return nil
}

func cookieToJSON(cookie *http.Cookie) CookieJSON {
	expires := ""
	if !cookie.Expires.IsZero() {
		expires = cookie.Expires.UTC().Format(http.TimeFormat)
	}
	return CookieJSON{
		Name:     cookie.Name,
		Value:    cookie.Value,
		Path:     cookie.Path,
		Domain:   cookie.Domain,
		Expires:  expires,
		MaxAge:   cookie.MaxAge,
		Secure:   cookie.Secure,
		HttpOnly: cookie.HttpOnly,
	}
}

func accessTokenFromCookieJSON(cookies []CookieJSON) string {
	for _, cookie := range cookies {
		if cookie.Name == accessTokenCookieName && strings.TrimSpace(cookie.Value) != "" {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func (m *Module) accountIDFromCookies() string {
	parsed, err := url.Parse("https://" + WEB_HOST)
	if err != nil || m.client == nil || m.client.GetCookieJar() == nil {
		return ""
	}
	for _, cookie := range m.client.GetCookieJar().Cookies(parsed) {
		if cookie.Name == accessTokenCookieName {
			return extractJWTClaim(cookie.Value, "sub")
		}
	}
	return ""
}

func stableSessionID(accountID string, email string) string {
	if accountID != "" {
		return "sess_" + accountID
	}
	return "sess_" + strings.NewReplacer("@", "_", ".", "_").Replace(email)
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataTime(metadata map[string]any, key string) time.Time {
	value := metadataString(metadata, key)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func formatRefreshTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
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

func metadataInt(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	switch value := metadata[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func metadataBool(metadata map[string]any, key string) bool {
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

func metadataBoolMap(metadata map[string]any, key string) map[string]bool {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	out := map[string]bool{}
	switch value := raw.(type) {
	case map[string]bool:
		for k, v := range value {
			out[normalizeCheckpointKey(k)] = v
		}
	case map[string]any:
		for k, v := range value {
			if parsed, ok := v.(bool); ok && parsed {
				out[normalizeCheckpointKey(k)] = true
			}
		}
	default:
		encoded, err := json.Marshal(raw)
		if err == nil {
			_ = json.Unmarshal(encoded, &out)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (status OrderStatusSnapshot) MetadataString(key string) string {
	if status.Metadata == nil {
		return ""
	}
	value, _ := status.Metadata[key].(string)
	return strings.TrimSpace(value)
}

func (m *Module) loadAddressMetadata(metadata map[string]any) {
	raw, ok := metadata["address"]
	if !ok {
		return
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return
	}
	_ = json.Unmarshal(encoded, &m.lastAddress)
}

func (m *Module) loadCardMetadata(metadata map[string]any) {
	raw, ok := metadata["card"]
	if !ok {
		return
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return
	}
	_ = json.Unmarshal(encoded, &m.lastCard)
}

func (m *Module) loadProductMetadata(metadata map[string]any) {
	raw, ok := metadata["checkout_products"]
	if !ok {
		return
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return
	}
	_ = json.Unmarshal(encoded, &m.lastOrderProducts)
}

func (m *Module) seonDeviceMetadata() (string, string, string) {
	if m == nil || m.seonDevice == nil {
		return "", "", ""
	}
	return strings.TrimSpace(m.seonDevice.Machine()),
		strings.TrimSpace(m.seonDevice.IosVersion()),
		strings.TrimSpace(m.seonDevice.UserDeviceName())
}

func (m *Module) restoreSeonDevice(metadata map[string]any) {
	machine := metadataString(metadata, "seon_machine")
	iosVersion := metadataString(metadata, "seon_ios_version")
	if machine == "" || iosVersion == "" {
		return
	}
	deviceName := metadataString(metadata, "seon_device_name")
	if deviceName == "" {
		deviceName = "iPhone"
	}
	device, err := BuildDevice(DeviceConfig{
		Machine:    machine,
		IOSVersion: iosVersion,
		DeviceName: deviceName,
		Proxy:      m.proxy,
	})
	if err != nil {
		if m.log != nil {
			m.log.Seon("Stored SEON device could not be restored: " + err.Error())
		}
		return
	}
	m.seonDevice = device
}

func firstTrackingCode(shipments []shipmentSnapshot) string {
	for _, shipment := range shipments {
		if strings.TrimSpace(shipment.TrackingCode) != "" {
			return strings.TrimSpace(shipment.TrackingCode)
		}
	}
	return ""
}

func firstTrackingURL(shipments []shipmentSnapshot) string {
	for _, shipment := range shipments {
		if strings.TrimSpace(shipment.TrackingURL) != "" {
			return strings.TrimSpace(shipment.TrackingURL)
		}
	}
	return ""
}

func firstShippingService(shipments []shipmentSnapshot) string {
	for _, shipment := range shipments {
		if strings.TrimSpace(shipment.ShippingServiceName) != "" {
			return strings.TrimSpace(shipment.ShippingServiceName)
		}
	}
	return ""
}

func firstShipmentID(shipments []shipmentSnapshot) string {
	for _, shipment := range shipments {
		if strings.TrimSpace(shipment.ID) != "" {
			return strings.TrimSpace(shipment.ID)
		}
	}
	return ""
}

func firstStringSlice(values []string, fallback string) []string {
	if len(values) > 0 {
		return values
	}
	if strings.TrimSpace(fallback) == "" {
		return nil
	}
	return []string{strings.TrimSpace(fallback)}
}
