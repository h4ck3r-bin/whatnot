package engine

import "time"

type AccountState string

const (
	AccountStateNew        AccountState = "New"
	AccountStateSignedUp   AccountState = "SignedUp"
	AccountStateCheckedOut AccountState = "CheckedOut"
	AccountStateComplete   AccountState = "Complete"
	AccountStateFailed     AccountState = "Failed"
)

type OrderState string

const (
	OrderStateCreated     OrderState = "Created"
	OrderStateInProgress  OrderState = "InProgress"
	OrderStateReviewReady OrderState = "ReviewReady"
	OrderStateReviewed    OrderState = "Reviewed"
	OrderStateFailed      OrderState = "Failed"
)

type MasterState string
type TaskState string
type TaskType string

const (
	MasterStateCandidate      MasterState = "Candidate"
	MasterStateHealthyPending MasterState = "HealthyPending"
	MasterStateActive         MasterState = "Active"
	MasterStateAging          MasterState = "Aging"
	MasterStateReady          MasterState = "Ready"
	MasterStateDormant        MasterState = "Dormant"
	MasterStateFailed         MasterState = "Failed"
)

const (
	TaskTypeAccountPipeline TaskType = "AccountPipeline"
)

const (
	TaskStatePending   TaskState = "Pending"
	TaskStateRunning   TaskState = "Running"
	TaskStateCompleted TaskState = "Completed"
	TaskStateFailed    TaskState = "Failed"
)

type Account struct {
	ID                 string       `json:"id"`
	Username           string       `json:"username"`
	Email              string       `json:"email"`
	Password           string       `json:"password,omitempty"`
	FirstName          string       `json:"first_name"`
	LastName           string       `json:"last_name"`
	IP                 string       `json:"ip,omitempty"`
	Proxy              string       `json:"proxy,omitempty"`
	MasterUsernameUsed string       `json:"master_username_used"`
	IsMasterCandidate  bool         `json:"is_master_candidate"`
	State              AccountState `json:"state"`
	SessionID          string       `json:"session_id"`
	OrderID            string       `json:"order_id,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	CompletedAt        *time.Time   `json:"completed_at,omitempty"`
	LastError          string       `json:"last_error,omitempty"`
}

type Order struct {
	ID                  string         `json:"id"`
	AccountID           string         `json:"account_id"`
	ExternalOrderID     string         `json:"external_order_id,omitempty"`
	State               OrderState     `json:"state"`
	CheckoutWebhookSent bool           `json:"checkout_webhook_sent"`
	TrackingNumber      string         `json:"tracking_number,omitempty"`
	TrackingWebhookSent bool           `json:"tracking_webhook_sent"`
	ReviewCompleted     bool           `json:"review_completed"`
	ReviewWebhookSent   bool           `json:"review_webhook_sent"`
	ReviewReferenceID   string         `json:"review_reference_id,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
	LastCheckedAt       *time.Time     `json:"last_checked_at,omitempty"`
	LastError           string         `json:"last_error,omitempty"`
	Metadata            map[string]any `json:"metadata,omitempty"`
}

type OrderListRow struct {
	AccountEmail    string `json:"account_email"`
	AccountUsername string `json:"account_username"`
	AccountCredits  string `json:"account_credits,omitempty"`
	Item            string `json:"item"`
	Price           string `json:"price"`
	OrderDate       string `json:"order_date"`
	Status          string `json:"status"`
	Tracking        string `json:"tracking"`
	OrderID         string `json:"order_id"`
}

type SessionCheckSummary struct {
	Checked   int                      `json:"checked"`
	Healthy   int                      `json:"healthy"`
	Banned    int                      `json:"banned"`
	Failed    int                      `json:"failed"`
	Missing   int                      `json:"missing"`
	Skipped   int                      `json:"skipped"`
	Orders    int                      `json:"orders"`
	Reviews   int                      `json:"reviews"`
	Referrers []ReferrerSessionSummary `json:"referrers,omitempty"`
}

type ReferrerSessionSummary struct {
	Referrer string `json:"referrer"`
	Healthy  int    `json:"healthy"`
	Banned   int    `json:"banned"`
}

type SessionRecord struct {
	ID        string         `json:"id"`
	AccountID string         `json:"account_id"`
	Cookies   []CookieRecord `json:"cookies"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CookieRecord struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
	SameSite string `json:"same_site,omitempty"`
}

type MasterAccount struct {
	ID                   string      `json:"id"`
	AccountID            string      `json:"account_id"`
	Username             string      `json:"username"`
	Email                string      `json:"email"`
	State                MasterState `json:"state"`
	SignupCount          int         `json:"signup_count"`
	SignupCap            int         `json:"signup_cap"`
	SessionID            string      `json:"session_id"`
	CandidateCompletedAt *time.Time  `json:"candidate_completed_at,omitempty"`
	LastHealthCheckAt    *time.Time  `json:"last_health_check_at,omitempty"`
	HealthySince         *time.Time  `json:"healthy_since,omitempty"`
	AgingStartedAt       *time.Time  `json:"aging_started_at,omitempty"`
	ReadyWebhookSent     bool        `json:"ready_webhook_sent"`
	CreatedAt            time.Time   `json:"created_at"`
	UpdatedAt            time.Time   `json:"updated_at"`
	LastError            string      `json:"last_error,omitempty"`
}

type SignupInput struct {
	FriendCode     string                   `json:"friend_code"`
	Email          string                   `json:"email,omitempty"`
	Password       string                   `json:"password,omitempty"`
	RewardDetected func(SignupResult) error `json:"-"`
}

type SignupResult struct {
	Account Account       `json:"account"`
	Session SessionRecord `json:"session"`
}

type CheckoutInput struct {
	Account                  Account              `json:"account"`
	Session                  SessionRecord        `json:"session"`
	SkipDelays               bool                 `json:"skip_delays,omitempty"`
	ForceCheckout            bool                 `json:"force_checkout,omitempty"`
	AllowInsufficientBalance bool                 `json:"allow_insufficient_balance,omitempty"`
	OrderPlaced              func(CheckoutResult) `json:"-"`
}

type PreparedCheckoutInput struct {
	Account                  Account              `json:"account"`
	Session                  SessionRecord        `json:"session"`
	AddressLine1             string               `json:"address_line1"`
	AddressLine2             string               `json:"address_line2"`
	Town                     string               `json:"town"`
	Postcode                 string               `json:"postcode"`
	AddressName              string               `json:"address_name"`
	CardNumber               string               `json:"card_number"`
	CardCVC                  string               `json:"card_cvc"`
	CardExpMonth             string               `json:"card_exp_month"`
	CardExpYear              string               `json:"card_exp_year"`
	CardPostcode             string               `json:"card_postcode"`
	PaymentMethodID          string               `json:"payment_method_id,omitempty"`
	ProductsJSON             string               `json:"products_json"`
	RewardLabel              string               `json:"reward_label"`
	SkipDelays               bool                 `json:"skip_delays,omitempty"`
	ForceCheckout            bool                 `json:"force_checkout,omitempty"`
	BalanceLimit             int                  `json:"balance_limit,omitempty"`
	AllowInsufficientBalance bool                 `json:"allow_insufficient_balance,omitempty"`
	OrderPlaced              func(CheckoutResult) `json:"-"`
}

type SingleAccountCheckoutRequest struct {
	Email             string `json:"email"`
	UseCustomDetails  bool   `json:"use_custom_details"`
	UseCustomAddress  bool   `json:"use_custom_address"`
	UseCustomCard     bool   `json:"use_custom_card"`
	UseCustomProducts bool   `json:"use_custom_products"`
	AddressLine1      string `json:"address_line1"`
	AddressLine2      string `json:"address_line2"`
	Town              string `json:"town"`
	Postcode          string `json:"postcode"`
	AddressName       string `json:"address_name"`
	CardNumber        string `json:"card_number"`
	CardCVC           string `json:"card_cvc"`
	CardExpMonth      string `json:"card_exp_month"`
	CardExpYear       string `json:"card_exp_year"`
	CardPostcode      string `json:"card_postcode"`
	PaymentMethodID   string `json:"payment_method_id,omitempty"`
	ProductsJSON      string `json:"products_json"`
	RewardLabel       string `json:"reward_label"`
	ApproveNoBalance  bool   `json:"approve_no_balance,omitempty"`
}

type CheckoutResult struct {
	Order             Order          `json:"order"`
	Orders            []Order        `json:"orders,omitempty"`
	Session           SessionRecord  `json:"session,omitempty"`
	ShippingFirstName string         `json:"shipping_first_name"`
	ShippingLastName  string         `json:"shipping_last_name"`
	PriceUpdates      []PriceUpdate  `json:"price_updates,omitempty"`
	ProductIssues     []ProductIssue `json:"product_issues,omitempty"`
}

type PriceUpdate struct {
	ProductName      string `json:"product_name"`
	ProductListingID string `json:"product_listing_id"`
	OldPrice         int    `json:"old_price"`
	NewPrice         int    `json:"new_price"`
	MaxPrice         int    `json:"max_price"`
}

type ProductIssue struct {
	ProductName      string `json:"product_name"`
	ProductListingID string `json:"product_listing_id"`
	Reason           string `json:"reason"`
}

type AccountOrdersInput struct {
	Account Account       `json:"account"`
	Session SessionRecord `json:"session"`
}

type OrderCheckInput struct {
	Account Account       `json:"account"`
	Order   Order         `json:"order"`
	Session SessionRecord `json:"session"`
}

type OrderCheckResult struct {
	TrackingNumber    string         `json:"tracking_number,omitempty"`
	ReadyForReview    bool           `json:"ready_for_review"`
	ReviewReferenceID string         `json:"review_reference_id,omitempty"`
	ExternalOrderID   string         `json:"external_order_id,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type ReviewInput struct {
	Account Account       `json:"account"`
	Order   Order         `json:"order"`
	Session SessionRecord `json:"session"`
	Stars   int           `json:"stars"`
	Ratings []int         `json:"ratings,omitempty"`
	Text    string        `json:"text,omitempty"`
}

type ReviewResult struct {
	NewUsername      string        `json:"new_username,omitempty"`
	AccountsFollowed int           `json:"accounts_followed,omitempty"`
	Session          SessionRecord `json:"session,omitempty"`
}

type PendingReviewItem struct {
	ID                string         `json:"id"`
	AccountID         string         `json:"account_id"`
	AccountEmail      string         `json:"account_email"`
	AccountUsername   string         `json:"account_username"`
	AccountCredits    string         `json:"account_credits,omitempty"`
	OrderID           string         `json:"order_id"`
	ExternalOrderID   string         `json:"external_order_id,omitempty"`
	ReviewReferenceID string         `json:"review_reference_id"`
	Items             []string       `json:"items,omitempty"`
	DisplayID         string         `json:"display_id,omitempty"`
	Status            string         `json:"status"`
	DetectedAt        time.Time      `json:"detected_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	LastError         string         `json:"last_error,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type AccountDashboard struct {
	Account        Account                 `json:"account"`
	CreditBalance  string                  `json:"credit_balance,omitempty"`
	Addresses      []AccountAddress        `json:"addresses,omitempty"`
	PaymentMethods []AccountPaymentMethod  `json:"payment_methods,omitempty"`
	Orders         []AccountDashboardOrder `json:"orders,omitempty"`
	Messages       []AccountMessageSummary `json:"messages,omitempty"`
	Notifications  []AccountNotification   `json:"notifications,omitempty"`
	Warnings       []string                `json:"warnings,omitempty"`
	RefreshedAt    time.Time               `json:"refreshed_at"`
}

type AccountAddress struct {
	ID              string `json:"id"`
	FullName        string `json:"full_name"`
	Line1           string `json:"line1"`
	Line2           string `json:"line2,omitempty"`
	City            string `json:"city"`
	State           string `json:"state,omitempty"`
	PostalCode      string `json:"postal_code"`
	CountryCode     string `json:"country_code"`
	DefaultShipping bool   `json:"default_shipping"`
	ReturnAddress   bool   `json:"return_address"`
}

type AccountPaymentMethod struct {
	ID              string `json:"id"`
	CardDescription string `json:"card_description,omitempty"`
	CardType        string `json:"card_type,omitempty"`
	Gateway         string `json:"gateway,omitempty"`
	Default         bool   `json:"default"`
}

type AccountAddressInput struct {
	ID              string `json:"id,omitempty"`
	FullName        string `json:"full_name"`
	Line1           string `json:"line1"`
	Line2           string `json:"line2,omitempty"`
	City            string `json:"city"`
	State           string `json:"state,omitempty"`
	PostalCode      string `json:"postal_code"`
	CountryCode     string `json:"country_code"`
	DefaultShipping bool   `json:"default_shipping"`
	ReturnAddress   bool   `json:"return_address"`
}

type AccountPaymentInput struct {
	CardNumber string `json:"card_number"`
	CardCVC    string `json:"card_cvc"`
	ExpMonth   string `json:"exp_month"`
	ExpYear    string `json:"exp_year"`
	Postcode   string `json:"postcode"`
}

type AccountDashboardOrder struct {
	OrderID          string   `json:"order_id"`
	ExternalOrderID  string   `json:"external_order_id,omitempty"`
	Display          string   `json:"display"`
	Items            []string `json:"items,omitempty"`
	SellerUsername   string   `json:"seller_username,omitempty"`
	Status           string   `json:"status"`
	Tracking         string   `json:"tracking,omitempty"`
	TrackingURL      string   `json:"tracking_url,omitempty"`
	Total            string   `json:"total,omitempty"`
	OrderDate        string   `json:"order_date,omitempty"`
	ReviewPending    bool     `json:"review_pending"`
	ReviewReference  string   `json:"review_reference,omitempty"`
	ConversationID   string   `json:"conversation_id,omitempty"`
	ConversationUser string   `json:"conversation_user,omitempty"`
}

type AccountMessageSummary struct {
	ConversationID string `json:"conversation_id"`
	Username       string `json:"username"`
	AccountCredits string `json:"account_credits,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	Unread         bool   `json:"unread"`
	UnreadCount    int    `json:"unread_count,omitempty"`
	Preview        string `json:"preview,omitempty"`
	LinkedOrder    string `json:"linked_order,omitempty"`
	LinkedItem     string `json:"linked_item,omitempty"`
	LastMessageAt  string `json:"last_message_at,omitempty"`
}

type AccountMessage struct {
	From       string `json:"from"`
	Body       string `json:"body"`
	SentAt     string `json:"sent_at,omitempty"`
	OwnMessage bool   `json:"own_message,omitempty"`
	MessageID  string `json:"message_id,omitempty"`
}

type AccountConversation struct {
	ConversationID string           `json:"conversation_id"`
	Username       string           `json:"username"`
	LinkedOrder    string           `json:"linked_order,omitempty"`
	LinkedItem     string           `json:"linked_item,omitempty"`
	Messages       []AccountMessage `json:"messages,omitempty"`
}

type AccountNotification struct {
	ID             string `json:"id"`
	AccountCredits string `json:"account_credits,omitempty"`
	ActivityID     string `json:"activity_id,omitempty"`
	ActivityType   string `json:"activity_type,omitempty"`
	Message        string `json:"message"`
	CreatedAt      string `json:"created_at,omitempty"`
	Unread         bool   `json:"unread"`
	DeepLink       string `json:"deep_link,omitempty"`
	PrimaryActor   string `json:"primary_actor,omitempty"`
	RelatedListing string `json:"related_listing,omitempty"`
}

type AccountUpdateSummary struct {
	Checked                 int `json:"checked"`
	Healthy                 int `json:"healthy"`
	Banned                  int `json:"banned"`
	Failed                  int `json:"failed"`
	Missing                 int `json:"missing"`
	Skipped                 int `json:"skipped"`
	Orders                  int `json:"orders"`
	PendingReviews          int `json:"pending_reviews"`
	MessageDiscoveries      int `json:"message_discoveries"`
	NotificationDiscoveries int `json:"notification_discoveries"`
	Webhooks                int `json:"webhooks"`
}

type CreditCheckoutCSVSummary struct {
	Checked  int    `json:"checked"`
	Eligible int    `json:"eligible"`
	Exported int    `json:"exported"`
	Banned   int    `json:"banned"`
	Failed   int    `json:"failed"`
	Skipped  int    `json:"skipped"`
	Path     string `json:"path"`
}

type ReviewCSVSummary struct {
	Checked   int    `json:"checked"`
	Exported  int    `json:"exported"`
	Submitted int    `json:"submitted"`
	Failed    int    `json:"failed"`
	Skipped   int    `json:"skipped"`
	Path      string `json:"path"`
}

type TrackingExportSummary struct {
	Exported int    `json:"exported"`
	Skipped  int    `json:"skipped"`
	Path     string `json:"path"`
}

type OngoingOrdersStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type OngoingOrdersExportSummary struct {
	Exported int                        `json:"exported"`
	Skipped  int                        `json:"skipped"`
	Path     string                     `json:"path"`
	Statuses []OngoingOrdersStatusCount `json:"statuses,omitempty"`
}

type FriendCodeCreditSummary struct {
	Checked int `json:"checked"`
	Healthy int `json:"healthy"`
	Updated int `json:"updated"`
	Banned  int `json:"banned"`
	Failed  int `json:"failed"`
	Missing int `json:"missing"`
	Skipped int `json:"skipped"`
}

type AccountMessageRequest struct {
	Query          string `json:"query,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	Username       string `json:"username,omitempty"`
	OrderID        string `json:"order_id,omitempty"`
	Body           string `json:"body"`
}

type BulkReviewSummary struct {
	Submitted int `json:"submitted"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

type MasterCheckoutRequest struct {
	ProductIdentifier string `json:"product_identifier"`
	ProductID         string `json:"product_id"`
	ListingID         string `json:"listing_id"`
	Price             string `json:"price"`
	CardNumber        string `json:"card_number"`
	CardCVC           string `json:"card_cvc"`
	CardExpMonth      string `json:"card_exp_month"`
	CardExpYear       string `json:"card_exp_year"`
	CardPostcode      string `json:"card_postcode"`
	ExtraFields       string `json:"extra_fields"`
}

type RuntimeState struct {
	NextOrderCheckAt  *time.Time `json:"next_order_check_at,omitempty"`
	LastOrderCheckDay string     `json:"last_order_check_day,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Task struct {
	ID           string         `json:"id"`
	Type         TaskType       `json:"type"`
	State        TaskState      `json:"state"`
	AccountID    string         `json:"account_id,omitempty"`
	OrderID      string         `json:"order_id,omitempty"`
	MasterID     string         `json:"master_id,omitempty"`
	LabelWritten bool           `json:"label_written"`
	Attempt      int            `json:"attempt"`
	MaxAttempts  int            `json:"max_attempts"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	LastError    string         `json:"last_error,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}
