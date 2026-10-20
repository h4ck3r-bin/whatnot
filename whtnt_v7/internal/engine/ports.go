package engine

import "context"

type Flows interface {
	Signup(ctx context.Context, input SignupInput) (SignupResult, error)
	PrepareCheckoutAccount(ctx context.Context, input SignupInput) (SignupResult, error)
	SignupForSEONTester(ctx context.Context, input SignupInput) (SignupResult, error)
	Checkout(ctx context.Context, input CheckoutInput) (CheckoutResult, error)
	CheckoutPreparedProducts(ctx context.Context, input PreparedCheckoutInput) (CheckoutResult, error)
	ListOrders(ctx context.Context, input AccountOrdersInput) ([]OrderCheckResult, error)
	ListReviewOrders(ctx context.Context, input AccountOrdersInput) ([]OrderCheckResult, error)
	CheckOrder(ctx context.Context, input OrderCheckInput) (OrderCheckResult, error)
	ListConversations(ctx context.Context, input AccountOrdersInput) ([]AccountMessageSummary, error)
	GetConversation(ctx context.Context, input AccountOrdersInput, conversationID string) (AccountConversation, error)
	ListNotifications(ctx context.Context, input AccountOrdersInput) ([]AccountNotification, error)
	MarkNotificationsViewed(ctx context.Context, input AccountOrdersInput) error
	AcknowledgeNotification(ctx context.Context, input AccountOrdersInput, notificationID string) error
	SendMessage(ctx context.Context, input AccountOrdersInput, request AccountMessageRequest) error
	FollowUser(ctx context.Context, input AccountOrdersInput, username string) error
	FollowListingSellers(ctx context.Context, input AccountOrdersInput, listingIDs []string) (int, error)
	ListAddresses(ctx context.Context, input AccountOrdersInput) ([]AccountAddress, error)
	CreateAddress(ctx context.Context, input AccountOrdersInput, address AccountAddressInput) (AccountAddress, error)
	UpdateAddress(ctx context.Context, input AccountOrdersInput, address AccountAddressInput) (AccountAddress, error)
	DeleteAddress(ctx context.Context, input AccountOrdersInput, addressID string) error
	ListPaymentMethods(ctx context.Context, input AccountOrdersInput) ([]AccountPaymentMethod, error)
	AddPaymentMethod(ctx context.Context, input AccountOrdersInput, payment AccountPaymentInput) (AccountPaymentMethod, SessionRecord, error)
	DeletePaymentMethod(ctx context.Context, input AccountOrdersInput, paymentID string) error
	LeaveReview(ctx context.Context, input ReviewInput) (ReviewResult, error)
	RefreshSession(ctx context.Context, session SessionRecord) (SessionRecord, bool, error)
	RefreshSessionWithoutCredits(ctx context.Context, session SessionRecord) (SessionRecord, bool, error)
	CheckMasterHealth(ctx context.Context, master MasterAccount, session SessionRecord) (SessionRecord, bool, error)
	MasterCheckout(ctx context.Context, master MasterAccount, session SessionRecord, request MasterCheckoutRequest) (CheckoutResult, error)
}

type Store interface {
	LoadAccounts() ([]Account, error)
	SaveAccounts([]Account) error
	LoadOrders() ([]Order, error)
	SaveOrders([]Order) error
	LoadNormalSessions() ([]SessionRecord, error)
	SaveNormalSessions([]SessionRecord) error
	LoadMasterSessions() ([]SessionRecord, error)
	SaveMasterSessions([]SessionRecord) error
	LoadMasters() ([]MasterAccount, error)
	SaveMasters([]MasterAccount) error
	LoadRuntimeState() (RuntimeState, error)
	SaveRuntimeState(RuntimeState) error
	LoadTasks() ([]Task, error)
	SaveTasks([]Task) error
	LoadPendingReviews() ([]PendingReviewItem, error)
	SavePendingReviews([]PendingReviewItem) error
	LoadBackendJSON(key string, dest any) error
	SaveBackendJSON(key string, value any) error
}
