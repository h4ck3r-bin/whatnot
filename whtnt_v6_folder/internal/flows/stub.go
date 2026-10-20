package flows

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/engine"
)

type Stub struct{}

func NewStub() Stub {
	return Stub{}
}

func (Stub) Signup(ctx context.Context, input engine.SignupInput) (engine.SignupResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.SignupResult{}, err
	}
	now := flowNow()
	id := newID("acct")
	firstName := pick([]string{"Ava", "Mia", "Noah", "Leo", "Ivy", "Theo"})
	lastName := pick([]string{"Stone", "Brooks", "Reed", "Parker", "Mason", "Cole"})
	username := strings.ToLower(fmt.Sprintf("%s%s%d", firstName, lastName, rand.Intn(9000)+1000))

	account := engine.Account{
		ID:                 id,
		Username:           username,
		Email:              firstNonEmpty(input.Email, username+"@example.test"),
		Password:           firstNonEmpty(input.Password, "stub-password"),
		FirstName:          firstName,
		LastName:           lastName,
		MasterUsernameUsed: strings.TrimSpace(input.FriendCode),
		State:              engine.AccountStateSignedUp,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	session := engine.SessionRecord{
		ID:        "sess_" + id,
		AccountID: id,
		Cookies: []engine.CookieRecord{
			{Name: "stub_session", Value: newID("cookie"), Domain: "simulator.local", Path: "/", Secure: true, HTTPOnly: true},
		},
		Metadata:  map[string]any{"stub": true},
		CreatedAt: now,
		UpdatedAt: now,
	}

	return engine.SignupResult{Account: account, Session: session}, nil
}

func (Stub) SignupForSEONTester(ctx context.Context, input engine.SignupInput) (engine.SignupResult, error) {
	return Stub{}.Signup(ctx, input)
}

func (Stub) PrepareCheckoutAccount(ctx context.Context, input engine.SignupInput) (engine.SignupResult, error) {
	result, err := Stub{}.Signup(ctx, input)
	if err != nil {
		return engine.SignupResult{}, err
	}
	if result.Session.Metadata == nil {
		result.Session.Metadata = map[string]any{}
	}
	result.Session.Metadata["referral_amount"] = 1000
	return result, nil
}

func (Stub) Checkout(ctx context.Context, input engine.CheckoutInput) (engine.CheckoutResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.CheckoutResult{}, err
	}
	now := flowNow()
	order := engine.Order{
		ID:              newID("order"),
		AccountID:       input.Account.ID,
		ExternalOrderID: newID("external"),
		State:           engine.OrderStateCreated,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return engine.CheckoutResult{
		Order:             order,
		ShippingFirstName: input.Account.FirstName,
		ShippingLastName:  input.Account.LastName,
	}, nil
}

func (Stub) CheckoutPreparedProducts(ctx context.Context, input engine.PreparedCheckoutInput) (engine.CheckoutResult, error) {
	return Stub{}.Checkout(ctx, engine.CheckoutInput{Account: input.Account, Session: input.Session})
}

func (Stub) CheckOrder(ctx context.Context, input engine.OrderCheckInput) (engine.OrderCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.OrderCheckResult{}, err
	}
	ready := time.Since(input.Order.CreatedAt) >= 30*time.Second
	tracking := input.Order.TrackingNumber
	if tracking == "" {
		tracking = strings.ToUpper(newID("TRK"))
	}
	return engine.OrderCheckResult{
		TrackingNumber: tracking,
		ReadyForReview: ready,
	}, nil
}

func (Stub) ListOrders(ctx context.Context, input engine.AccountOrdersInput) ([]engine.OrderCheckResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (Stub) ListReviewOrders(ctx context.Context, input engine.AccountOrdersInput) ([]engine.OrderCheckResult, error) {
	return Stub{}.ListOrders(ctx, input)
}

func (Stub) ListConversations(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountMessageSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (Stub) GetConversation(ctx context.Context, input engine.AccountOrdersInput, conversationID string) (engine.AccountConversation, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountConversation{}, err
	}
	return engine.AccountConversation{ConversationID: strings.TrimSpace(conversationID)}, nil
}

func (Stub) ListNotifications(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountNotification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (Stub) MarkNotificationsViewed(ctx context.Context, input engine.AccountOrdersInput) error {
	return ctx.Err()
}

func (Stub) AcknowledgeNotification(ctx context.Context, input engine.AccountOrdersInput, notificationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(notificationID) == "" {
		return fmt.Errorf("notification id is required")
	}
	return nil
}

func (Stub) SendMessage(ctx context.Context, input engine.AccountOrdersInput, request engine.AccountMessageRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(request.Body) == "" {
		return fmt.Errorf("message body is required")
	}
	return nil
}

func (Stub) FollowUser(ctx context.Context, input engine.AccountOrdersInput, username string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("username is required")
	}
	return nil
}

func (Stub) FollowListingSellers(ctx context.Context, input engine.AccountOrdersInput, listingIDs []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(listingIDs), nil
}

func (Stub) ListAddresses(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountAddress, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (Stub) CreateAddress(ctx context.Context, input engine.AccountOrdersInput, address engine.AccountAddressInput) (engine.AccountAddress, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountAddress{}, err
	}
	return engine.AccountAddress{ID: newID("addr"), FullName: address.FullName, Line1: address.Line1, Line2: address.Line2, City: address.City, State: address.State, PostalCode: address.PostalCode, CountryCode: address.CountryCode, DefaultShipping: address.DefaultShipping, ReturnAddress: address.ReturnAddress}, nil
}

func (Stub) UpdateAddress(ctx context.Context, input engine.AccountOrdersInput, address engine.AccountAddressInput) (engine.AccountAddress, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountAddress{}, err
	}
	if strings.TrimSpace(address.ID) == "" {
		return engine.AccountAddress{}, fmt.Errorf("address id is required")
	}
	return engine.AccountAddress{ID: address.ID, FullName: address.FullName, Line1: address.Line1, Line2: address.Line2, City: address.City, State: address.State, PostalCode: address.PostalCode, CountryCode: address.CountryCode, DefaultShipping: address.DefaultShipping, ReturnAddress: address.ReturnAddress}, nil
}

func (Stub) DeleteAddress(ctx context.Context, input engine.AccountOrdersInput, addressID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(addressID) == "" {
		return fmt.Errorf("address id is required")
	}
	return nil
}

func (Stub) ListPaymentMethods(ctx context.Context, input engine.AccountOrdersInput) ([]engine.AccountPaymentMethod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, nil
}

func (Stub) AddPaymentMethod(ctx context.Context, input engine.AccountOrdersInput, payment engine.AccountPaymentInput) (engine.AccountPaymentMethod, engine.SessionRecord, error) {
	if err := ctx.Err(); err != nil {
		return engine.AccountPaymentMethod{}, engine.SessionRecord{}, err
	}
	return engine.AccountPaymentMethod{ID: newID("pm"), CardDescription: "**** " + lastFour(payment.CardNumber), CardType: "card", Gateway: "stripe"}, input.Session, nil
}

func (Stub) DeletePaymentMethod(ctx context.Context, input engine.AccountOrdersInput, paymentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(paymentID) == "" {
		return fmt.Errorf("payment method id is required")
	}
	return nil
}

func (Stub) LeaveReview(ctx context.Context, input engine.ReviewInput) (engine.ReviewResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.ReviewResult{}, err
	}
	if input.Stars != 5 {
		return engine.ReviewResult{}, fmt.Errorf("stub review only accepts the configured 5 star review path")
	}
	return engine.ReviewResult{}, nil
}

func lastFour(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 4 {
		return value
	}
	return value[len(value)-4:]
}

func (Stub) RefreshSession(ctx context.Context, session engine.SessionRecord) (engine.SessionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return engine.SessionRecord{}, false, err
	}
	session.UpdatedAt = flowNow()
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	session.Metadata["last_stub_refresh"] = session.UpdatedAt.Format(time.RFC3339)
	return session, true, nil
}

func (Stub) RefreshSessionWithoutCredits(ctx context.Context, session engine.SessionRecord) (engine.SessionRecord, bool, error) {
	return Stub{}.RefreshSession(ctx, session)
}

func (Stub) CheckMasterHealth(ctx context.Context, master engine.MasterAccount, session engine.SessionRecord) (engine.SessionRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return engine.SessionRecord{}, false, err
	}
	return Stub{}.RefreshSessionWithoutCredits(ctx, session)
}

func (Stub) MasterCheckout(ctx context.Context, master engine.MasterAccount, session engine.SessionRecord, request engine.MasterCheckoutRequest) (engine.CheckoutResult, error) {
	if err := ctx.Err(); err != nil {
		return engine.CheckoutResult{}, err
	}
	if strings.TrimSpace(request.ProductIdentifier) == "" && strings.TrimSpace(request.ProductID) == "" && strings.TrimSpace(request.ListingID) == "" {
		return engine.CheckoutResult{}, fmt.Errorf("provide at least one product/listing identifier")
	}
	now := flowNow()
	return engine.CheckoutResult{
		Order: engine.Order{
			ID:              newID("master_order"),
			AccountID:       master.AccountID,
			ExternalOrderID: newID("external"),
			State:           engine.OrderStateCreated,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		ShippingFirstName: "",
		ShippingLastName:  "",
	}, nil
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d_%06d", prefix, time.Now().UnixNano(), rand.Intn(1000000))
}

func pick(values []string) string {
	return values[rand.Intn(len(values))]
}
