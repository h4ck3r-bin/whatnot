package whtnt

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ConversationSnapshot struct {
	ID            string
	Username      string
	DisplayName   string
	Unread        bool
	Preview       string
	LinkedOrder   string
	LinkedItem    string
	LastMessageAt string
	ParticipantID string
}

type ConversationThreadSnapshot struct {
	ID          string
	Username    string
	LinkedOrder string
	LinkedItem  string
	Messages    []MessageSnapshot
}

type MessageSnapshot struct {
	ID         string
	From       string
	Body       string
	SentAt     string
	OwnMessage bool
}

type NotificationSnapshot struct {
	ID             string
	ActivityID     string
	ActivityType   string
	Message        string
	CreatedAt      string
	Unread         bool
	DeepLink       string
	PrimaryActor   string
	RelatedListing string
}

type AddressSnapshot struct {
	ID              string
	FullName        string
	Line1           string
	Line2           string
	City            string
	State           string
	PostalCode      string
	CountryCode     string
	DefaultShipping bool
	ReturnAddress   bool
}

type PaymentMethodSnapshot struct {
	ID              string
	CardDescription string
	CardMetadata    string
	CardType        string
	Gateway         string
	Default         bool
}

type RewardCreditSnapshot struct {
	UserID     string
	AmountSafe int
	Currency   string
}

func (m *Module) ListAddressSnapshots() ([]AddressSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"GetAddresses",
		map[string]any{},
		`query GetAddresses @attribution(owner: "selling") { me { __typename id addresses { __typename id fullName line1 line2 city state postalCode countryCode defaultShipping returnAddress isPickupAddress } } }`,
	)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return nil, fmt.Errorf("parse GetAddresses response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return nil, fmt.Errorf("GetAddresses graphql errors: %s", shortResponse(res.String()))
	}
	me, ok := nestedMap(payload, "data", "me")
	if !ok {
		return nil, fmt.Errorf("GetAddresses response missing me")
	}
	addresses, ok := nestedSlice(me, "addresses")
	if !ok {
		return nil, nil
	}
	out := make([]AddressSnapshot, 0, len(addresses))
	for _, item := range addresses {
		node, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, addressSnapshotFromNode(node))
	}
	return out, nil
}

func (m *Module) CreateAddressSnapshot(address AddressSnapshot) (AddressSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"CreateAddress",
		map[string]any{
			"fullName":          address.FullName,
			"addressLine1":      address.Line1,
			"addressLine2":      address.Line2,
			"city":              address.City,
			"state":             firstNonEmptyString(address.State, address.City),
			"postalCode":        address.PostalCode,
			"countryCode":       firstNonEmptyString(address.CountryCode, checkoutCountryCode),
			"isDefaultShipping": address.DefaultShipping,
			"isReturnAddress":   address.ReturnAddress,
			"seonSession":       nil,
		},
		`mutation CreateAddress($fullName: String!, $addressLine1: String!, $addressLine2: String!, $city: String!, $state: String!, $postalCode: String!, $countryCode: String!, $isDefaultShipping: Boolean!, $isReturnAddress: Boolean!, $seonSession: String) @attribution(owner: "selling") { createAddress(fullName: $fullName, line1: $addressLine1, line2: $addressLine2, city: $city, state: $state, postalCode: $postalCode, countryCode: $countryCode, defaultShipping: $isDefaultShipping, homeAddress: $isReturnAddress, seonSession: $seonSession) { __typename address { __typename id fullName line1 line2 city state postalCode countryCode defaultShipping returnAddress isPickupAddress } } }`,
	)
	if err != nil {
		return AddressSnapshot{}, err
	}
	return parseAddressMutationResponse(res.String(), "createAddress")
}

func (m *Module) UpdateAddressSnapshot(address AddressSnapshot) (AddressSnapshot, error) {
	if strings.TrimSpace(address.ID) == "" {
		return AddressSnapshot{}, fmt.Errorf("address id is required")
	}
	res, err := m.sendMobileCheckoutRequest(
		"UpdateAddress",
		map[string]any{
			"id":              address.ID,
			"fullName":        nullableString(address.FullName),
			"addressLine1":    nullableString(address.Line1),
			"addressLine2":    nullableString(address.Line2),
			"city":            nullableString(address.City),
			"state":           nullableString(firstNonEmptyString(address.State, address.City)),
			"postalCode":      nullableString(address.PostalCode),
			"countryCode":     nullableString(firstNonEmptyString(address.CountryCode, checkoutCountryCode)),
			"returnAddress":   address.ReturnAddress,
			"defaultShipping": address.DefaultShipping,
		},
		`mutation UpdateAddress($id: ID!, $fullName: String, $addressLine1: String, $addressLine2: String, $city: String, $state: String, $postalCode: String, $countryCode: String, $returnAddress: Boolean, $defaultShipping: Boolean) @attribution(owner: "selling") { updateAddress(id: $id, fullName: $fullName, line1: $addressLine1, line2: $addressLine2, city: $city, state: $state, postalCode: $postalCode, countryCode: $countryCode, returnAddress: $returnAddress, defaultShipping: $defaultShipping) { __typename error address { __typename id fullName line1 line2 city state postalCode countryCode defaultShipping returnAddress isPickupAddress } } }`,
	)
	if err != nil {
		return AddressSnapshot{}, err
	}
	return parseAddressMutationResponse(res.String(), "updateAddress")
}

func (m *Module) DeleteAddressSnapshot(addressID string) error {
	addressID = strings.TrimSpace(addressID)
	if addressID == "" {
		return fmt.Errorf("address id is required")
	}
	res, err := m.sendMobileCheckoutRequest(
		"DeleteAddress",
		map[string]any{"id": addressID},
		`mutation DeleteAddress($id: ID!) @attribution(owner: "selling") { deleteAddress(id: $id) { __typename error } }`,
	)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return fmt.Errorf("parse DeleteAddress response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return fmt.Errorf("DeleteAddress graphql errors: %s", shortResponse(res.String()))
	}
	if errText := nestedString(payload, "data", "deleteAddress", "error"); errText != "" {
		return fmt.Errorf("delete address failed: %s", errText)
	}
	return nil
}

func (m *Module) ListPaymentMethodSnapshots() ([]PaymentMethodSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"GetPaymentMethods",
		map[string]any{},
		`query GetPaymentMethods @attribution(owner: "selling") { paymentMethods userDefaultPayment { __typename id cardDescription cardMetadata cardType gateway default } }`,
	)
	if err != nil {
		return nil, err
	}
	return parsePaymentMethodsResponse(res.String())
}

func (m *Module) AddPaymentMethodSnapshot(card CardDetails) (PaymentMethodSnapshot, SessionSnapshot, error) {
	m.overrideCard = &card
	defer func() { m.overrideCard = nil }()
	paymentID, err := m.setupPaymentMethod()
	if err != nil {
		return PaymentMethodSnapshot{}, SessionSnapshot{}, err
	}
	snapshot, snapErr := m.SessionSnapshot()
	if snapErr != nil {
		return PaymentMethodSnapshot{}, SessionSnapshot{}, snapErr
	}
	return PaymentMethodSnapshot{
		ID:              paymentID,
		CardDescription: "**** " + cardLast4Utility(card.Number),
		CardMetadata:    `{"last4":"` + cardLast4Utility(card.Number) + `"}`,
		CardType:        "card",
		Gateway:         "stripe",
	}, snapshot, nil
}

func (m *Module) DeletePaymentMethodSnapshot(paymentID string) error {
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return fmt.Errorf("payment method id is required")
	}
	res, err := m.sendMobileCheckoutRequest(
		"DeletePaymentMethod",
		map[string]any{"id": paymentID, "paymentMethodId": paymentID},
		`mutation DeletePaymentMethod($id: ID!, $paymentMethodId: ID!) @attribution(owner: "selling") { deletePaymentMethod(id: $id, paymentMethodId: $paymentMethodId) { __typename error } }`,
	)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return fmt.Errorf("parse DeletePaymentMethod response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return fmt.Errorf("payment method delete is not accepted by this API: %s", shortResponse(res.String()))
	}
	if errText := nestedString(payload, "data", "deletePaymentMethod", "error"); errText != "" {
		return fmt.Errorf("delete payment method failed: %s", errText)
	}
	return nil
}

func (m *Module) GetMyCreditsSnapshot() (RewardCreditSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"GetMyCredits",
		map[string]any{},
		`query GetMyCredits { me { id rewardBalance { amountSafe currency } } }`,
	)
	if err != nil {
		return RewardCreditSnapshot{}, err
	}
	var payload struct {
		Errors any `json:"errors"`
		Data   struct {
			Me struct {
				ID            string `json:"id"`
				RewardBalance struct {
					AmountSafe int    `json:"amountSafe"`
					Currency   string `json:"currency"`
				} `json:"rewardBalance"`
			} `json:"me"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return RewardCreditSnapshot{}, fmt.Errorf("parse GetMyCredits response: %w", err)
	}
	if payload.Errors != nil {
		return RewardCreditSnapshot{}, fmt.Errorf("GetMyCredits graphql errors: %s", shortResponse(res.String()))
	}
	return RewardCreditSnapshot{
		UserID:     payload.Data.Me.ID,
		AmountSafe: payload.Data.Me.RewardBalance.AmountSafe,
		Currency:   payload.Data.Me.RewardBalance.Currency,
	}, nil
}

func (m *Module) ListConversationSnapshots() ([]ConversationSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"GetConversationsV2",
		map[string]any{
			"after":  nil,
			"sort":   nil,
			"filter": nil,
		},
		`query GetConversationsV2($after: String, $sort: ConversationSortInput, $filter: ConversationFilterInput) @attribution(owner: "community") { me { __typename id inbox { __typename conversationsV2(first: 20, after: $after, sort: $sort, filter: $filter) { __typename pageInfo { __typename endCursor hasNextPage } edges { __typename node { __typename id hasUnreadDirectMessages displayName participant { __typename id username displayName } externalEntity { __typename id type title subtitle previewLine1 previewLine2 entity { __typename ... on OrderNode { id prettyStatus items { __typename edges { __typename node { __typename listing { __typename title } } } } } } } previewLine1 { __typename text } previewLine2 { __typename items { __typename text } } mostRecentDirectMessage { __typename id body serverTimeUTC sender { __typename id username } } } } } } } }`,
	)
	if err != nil {
		return nil, err
	}
	return parseConversationSnapshots(res.String())
}

func (m *Module) GetConversationSnapshot(conversationID string) (ConversationThreadSnapshot, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return ConversationThreadSnapshot{}, fmt.Errorf("conversation id is required")
	}
	res, err := m.sendMobileCheckoutRequest(
		"GetConversation",
		map[string]any{
			"conversationId": conversationID,
			"first":          30,
			"after":          nil,
		},
		`query GetConversation($conversationId: ID!, $first: Int, $after: String) @attribution(owner: "community") { me { __typename id username inbox { __typename conversation(id: $conversationId) { __typename id displayName participant { __typename id username displayName } externalEntity { __typename id type title subtitle previewLine1 previewLine2 } } conversationMessages(id: $conversationId, first: $first, after: $after) { __typename edges { __typename node { __typename id body serverTimeUTC sender { __typename id username } } } } } } }`,
	)
	if err != nil {
		return ConversationThreadSnapshot{}, err
	}
	return parseConversationThreadSnapshot(conversationID, res.String())
}

func (m *Module) ListNotificationSnapshots() ([]NotificationSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"NotificationsHub__MyNotifications",
		map[string]any{
			"filterType":  nil,
			"sectionType": nil,
		},
		`query NotificationsHub__MyNotifications($filterType: NotificationsHubFilterType, $sectionType: NotificationsHubSectionType) @attribution(owner: "notifications-platform") { myNotifications(filterType: $filterType, sectionType: $sectionType) { __typename id activityId activityType createdAt deepLink message viewedAt isAcknowledged priority leadingItem { __typename entity { __typename ... on UserNode { id username } ... on ListingNode { id title } ... on SellerAndCurrentLoyaltyTier { seller { __typename id username } } } } } }`,
	)
	if err != nil {
		return nil, err
	}
	return parseNotificationSnapshots(res.String())
}

func (m *Module) MarkNotificationsHubViewed() error {
	res, err := m.sendMobileCheckoutRequest(
		"NotificationsHub__MarkNotificationsHubViewed",
		map[string]any{},
		`mutation NotificationsHub__MarkNotificationsHubViewed @attribution(owner: "notifications-platform") { markNotificationsHubViewed }`,
	)
	if err != nil {
		return err
	}
	if err := validateNotificationMutationResponse(res.String(), "markNotificationsHubViewed"); err == nil {
		return nil
	}
	res, err = m.sendMobileCheckoutRequest(
		"NotificationsHub__MarkNotificationsHubViewed",
		map[string]any{},
		`mutation NotificationsHub__MarkNotificationsHubViewed @attribution(owner: "notifications-platform") { markNotificationsHubViewed { __typename } }`,
	)
	if err != nil {
		return err
	}
	return validateNotificationMutationResponse(res.String(), "markNotificationsHubViewed")
}

func (m *Module) AcknowledgeNotificationFeedNode(notificationID string) error {
	notificationID = strings.TrimSpace(notificationID)
	if notificationID == "" {
		return fmt.Errorf("notification id is required")
	}
	variables := map[string]any{"id": notificationID}
	res, err := m.sendMobileCheckoutRequest(
		"AcknowledgeNotificationFeedNode",
		variables,
		`mutation AcknowledgeNotificationFeedNode($id: ID!) @attribution(owner: "notifications-platform") { acknowledgeNotificationFeedNode(id: $id) }`,
	)
	if err != nil {
		return err
	}
	if err := validateNotificationMutationResponse(res.String(), "acknowledgeNotificationFeedNode"); err == nil {
		return nil
	}
	res, err = m.sendMobileCheckoutRequest(
		"AcknowledgeNotificationFeedNode",
		variables,
		`mutation AcknowledgeNotificationFeedNode($id: ID!) @attribution(owner: "notifications-platform") { acknowledgeNotificationFeedNode(id: $id) { __typename } }`,
	)
	if err != nil {
		return err
	}
	return validateNotificationMutationResponse(res.String(), "acknowledgeNotificationFeedNode")
}

func (m *Module) SendMessageSnapshot(conversationID string, participantID string, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		return fmt.Errorf("message body is required")
	}
	var participantIDs any
	if strings.TrimSpace(participantID) != "" {
		participantIDs = []string{strings.TrimSpace(participantID)}
	}
	res, err := m.sendMobileCheckoutRequest(
		"SendDirectMessage",
		map[string]any{
			"conversationId": nullableString(conversationID),
			"participantIds": participantIDs,
			"body":           body,
			"tags":           map[string]any{},
			"inResponseTo":   nil,
			"media":          []any{},
		},
		`mutation SendDirectMessage($conversationId: ID, $participantIds: [ID], $body: String!, $tags: DirectMessageTagsInput!, $inResponseTo: ID, $media: [DirectMessageMediaInput!]!) @attribution(owner: "community") { sendDirectMessageToConversation(conversationId: $conversationId, participantIds: $participantIds, body: $body, inResponseTo: $inResponseTo, tags: $tags, media: $media) { __typename directMessage { __typename id body } error { __typename message errorType } } }`,
	)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return err
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return fmt.Errorf("send message graphql errors: %s", shortResponse(res.String()))
	}
	root, ok := nestedMap(payload, "data", "sendDirectMessageToConversation")
	if !ok {
		return fmt.Errorf("send message response missing result")
	}
	if errMap, ok := root["error"].(map[string]any); ok && errMap != nil {
		return fmt.Errorf("send message failed: %s", firstNonEmptyString(stringValue(errMap, "message"), stringValue(errMap, "errorType")))
	}
	return nil
}

func (m *Module) UserIDForUsername(username string) (string, error) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return "", fmt.Errorf("username is required")
	}
	res, err := m.sendMobileCheckoutRequest(
		"GetUserId",
		map[string]any{"username": username, "excludeBanned": true},
		`query GetUserId($username: String, $excludeBanned: Boolean) @attribution(owner: "app-platform") { getUser(username: $username, excludeBanned: $excludeBanned) { __typename id username } }`,
	)
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return "", err
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return "", fmt.Errorf("get user id graphql errors: %s", shortResponse(res.String()))
	}
	id := nestedString(payload, "data", "getUser", "id")
	if id == "" {
		return "", fmt.Errorf("user @%s was not found", username)
	}
	return id, nil
}

func (m *Module) FollowUserByUsername(username string) error {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if username == "" {
		return fmt.Errorf("username is required")
	}
	userID, err := m.UserIDForUsername(username)
	if err != nil {
		return err
	}
	res, err := m.sendMobileCheckoutRequest("FollowUser", map[string]any{
		"active": true,
		"id":     normalizeReviewSellerUserID(userID),
	}, "mutation FollowUser($id:ID!$active:Boolean){followUser(id:$id active:$active){success user{id isFollowing isFollower __typename}__typename}}")
	if err != nil {
		return err
	}
	return verifyFollowUserSuccess(res.String())
}

func (m *Module) FollowListingSellers(listingIDs []string) (int, error) {
	sellerIDs := []string{}
	seenListings := map[string]bool{}
	seenSellers := map[string]bool{}
	for _, listingID := range listingIDs {
		listingID = strings.TrimSpace(listingID)
		if listingID == "" || seenListings[listingID] {
			continue
		}
		seenListings[listingID] = true
		sellerID, _, err := m.ListingSeller(listingID)
		if err != nil {
			return len(sellerIDs), err
		}
		sellerID = normalizeReviewSellerUserID(sellerID)
		if sellerID == "" || seenSellers[sellerID] {
			continue
		}
		seenSellers[sellerID] = true
		sellerIDs = append(sellerIDs, sellerID)
	}
	if len(sellerIDs) == 0 {
		return 0, nil
	}
	if err := m.bulkFollowSellerIDsWithDelay(sellerIDs, "between_checkout_follows"); err != nil {
		return 0, err
	}
	return len(sellerIDs), nil
}

func (m *Module) ListingSeller(listingID string) (string, string, error) {
	listingID = strings.TrimSpace(listingID)
	if listingID == "" {
		return "", "", fmt.Errorf("listing id is required")
	}
	res, err := m.sendMobileCheckoutRequest(
		"GetListingSellerId",
		map[string]any{"id": listingID},
		`query GetListingSellerId($id: ID!) @attribution(owner: "app-platform") { getListing(id: $id) { __typename id user { __typename id username } } }`,
	)
	if err != nil {
		return "", "", err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return "", "", fmt.Errorf("parse GetListingSellerId response: %w", err)
	}
	if errorsRaw, ok := payload["errors"].([]any); ok && len(errorsRaw) > 0 {
		return "", "", fmt.Errorf("GetListingSellerId returned errors: %s", shortResponse(res.String()))
	}
	user, ok := nestedMap(payload, "data", "getListing", "user")
	if !ok {
		return "", "", fmt.Errorf("GetListingSellerId response missing user: %s", shortResponse(res.String()))
	}
	sellerID := stringValue(user, "id")
	if sellerID == "" {
		return "", "", fmt.Errorf("GetListingSellerId response missing seller id: %s", shortResponse(res.String()))
	}
	return sellerID, stringValue(user, "username"), nil
}

func parseConversationSnapshots(body string) ([]ConversationSnapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("parse conversations response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return nil, fmt.Errorf("conversation graphql errors: %s", shortResponse(body))
	}
	root, ok := nestedMap(payload, "data", "me", "inbox", "conversationsV2")
	if !ok {
		return nil, fmt.Errorf("conversation response missing conversations")
	}
	edges, _ := nestedSlice(root, "edges")
	out := make([]ConversationSnapshot, 0, len(edges))
	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}
		node, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, conversationFromNode(node))
	}
	return out, nil
}

func conversationFromNode(node map[string]any) ConversationSnapshot {
	external, _ := node["externalEntity"].(map[string]any)
	participant, _ := node["participant"].(map[string]any)
	message, _ := node["mostRecentDirectMessage"].(map[string]any)
	preview := firstNonEmptyString(
		stringValue(message, "body"),
		nestedString(node, "previewLine1", "text"),
		stringValue(external, "previewLine1"),
		stringValue(external, "previewLine2"),
	)
	return ConversationSnapshot{
		ID:            stringValue(node, "id"),
		Username:      stringValue(participant, "username"),
		DisplayName:   firstNonEmptyString(stringValue(participant, "displayName"), stringValue(node, "displayName")),
		ParticipantID: stringValue(participant, "id"),
		Unread:        boolValue(node, "hasUnreadDirectMessages"),
		Preview:       preview,
		LinkedOrder:   firstNonEmptyString(stringValue(external, "title"), stringValue(external, "subtitle")),
		LinkedItem:    firstNonEmptyString(stringValue(external, "previewLine1"), stringValue(external, "previewLine2")),
		LastMessageAt: stringValue(message, "serverTimeUTC"),
	}
}

func parseConversationThreadSnapshot(conversationID string, body string) (ConversationThreadSnapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ConversationThreadSnapshot{}, fmt.Errorf("parse conversation response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return ConversationThreadSnapshot{}, fmt.Errorf("conversation graphql errors: %s", shortResponse(body))
	}
	me, _ := nestedMap(payload, "data", "me")
	inbox, ok := nestedMap(payload, "data", "me", "inbox")
	if !ok {
		return ConversationThreadSnapshot{}, fmt.Errorf("conversation response missing inbox")
	}
	selfID := stringValue(me, "id")
	conversation, _ := inbox["conversation"].(map[string]any)
	participant, _ := conversation["participant"].(map[string]any)
	external, _ := conversation["externalEntity"].(map[string]any)
	thread := ConversationThreadSnapshot{
		ID:          conversationID,
		Username:    stringValue(participant, "username"),
		LinkedOrder: firstNonEmptyString(stringValue(external, "title"), stringValue(external, "subtitle")),
		LinkedItem:  firstNonEmptyString(stringValue(external, "previewLine1"), stringValue(external, "previewLine2")),
	}
	messagesRoot, _ := inbox["conversationMessages"].(map[string]any)
	edges, _ := nestedSlice(messagesRoot, "edges")
	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}
		node, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}
		sender, _ := node["sender"].(map[string]any)
		senderID := stringValue(sender, "id")
		from := stringValue(sender, "username")
		if from == "" && senderID == selfID {
			from = "You"
		}
		thread.Messages = append(thread.Messages, MessageSnapshot{
			ID:         stringValue(node, "id"),
			From:       from,
			Body:       stringValue(node, "body"),
			SentAt:     stringValue(node, "serverTimeUTC"),
			OwnMessage: selfID != "" && senderID == selfID,
		})
	}
	return thread, nil
}

func parseNotificationSnapshots(body string) ([]NotificationSnapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("parse notifications response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return nil, fmt.Errorf("notification graphql errors: %s", shortResponse(body))
	}
	data, ok := nestedMap(payload, "data")
	if !ok {
		return nil, fmt.Errorf("notification response missing data")
	}
	notifications, ok := nestedSlice(data, "myNotifications")
	if !ok {
		return nil, fmt.Errorf("notification response missing notifications")
	}
	out := make([]NotificationSnapshot, 0, len(notifications))
	for _, item := range notifications {
		node, ok := item.(map[string]any)
		if !ok {
			continue
		}
		leading, _ := nestedMap(node, "leadingItem", "entity")
		n := NotificationSnapshot{
			ID:             stringValue(node, "id"),
			ActivityID:     stringValue(node, "activityId"),
			ActivityType:   stringValue(node, "activityType"),
			Message:        stringValue(node, "message"),
			CreatedAt:      stringValue(node, "createdAt"),
			Unread:         stringValue(node, "viewedAt") == "" && !boolValue(node, "isAcknowledged"),
			DeepLink:       stringValue(node, "deepLink"),
			PrimaryActor:   stringValue(leading, "username"),
			RelatedListing: stringValue(leading, "title"),
		}
		if n.ID != "" || n.Message != "" {
			out = append(out, n)
		}
	}
	return out, nil
}

func validateNotificationMutationResponse(body string, field string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return fmt.Errorf("parse %s response: %w", field, err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return fmt.Errorf("%s graphql errors: %s", field, shortResponse(body))
	}
	data, ok := nestedMap(payload, "data")
	if !ok {
		return fmt.Errorf("%s response missing data", field)
	}
	if _, ok := data[field]; !ok {
		return fmt.Errorf("%s response missing result", field)
	}
	return nil
}

func parseAddressMutationResponse(body string, field string) (AddressSnapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return AddressSnapshot{}, fmt.Errorf("parse %s response: %w", field, err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return AddressSnapshot{}, fmt.Errorf("%s graphql errors: %s", field, shortResponse(body))
	}
	root, ok := nestedMap(payload, "data", field)
	if !ok {
		return AddressSnapshot{}, fmt.Errorf("%s response missing result", field)
	}
	if errText := stringValue(root, "error"); errText != "" {
		return AddressSnapshot{}, fmt.Errorf("%s failed: %s", field, errText)
	}
	address, ok := root["address"].(map[string]any)
	if !ok {
		return AddressSnapshot{}, fmt.Errorf("%s response missing address", field)
	}
	return addressSnapshotFromNode(address), nil
}

func addressSnapshotFromNode(node map[string]any) AddressSnapshot {
	return AddressSnapshot{
		ID:              stringValue(node, "id"),
		FullName:        stringValue(node, "fullName"),
		Line1:           stringValue(node, "line1"),
		Line2:           stringValue(node, "line2"),
		City:            stringValue(node, "city"),
		State:           stringValue(node, "state"),
		PostalCode:      stringValue(node, "postalCode"),
		CountryCode:     stringValue(node, "countryCode"),
		DefaultShipping: boolValue(node, "defaultShipping"),
		ReturnAddress:   boolValue(node, "returnAddress"),
	}
}

func parsePaymentMethodsResponse(body string) ([]PaymentMethodSnapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return nil, fmt.Errorf("parse GetPaymentMethods response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return nil, fmt.Errorf("GetPaymentMethods graphql errors: %s", shortResponse(body))
	}
	data, ok := nestedMap(payload, "data")
	if !ok {
		return nil, fmt.Errorf("GetPaymentMethods response missing data")
	}
	out := []PaymentMethodSnapshot{}
	seen := map[string]bool{}
	add := func(node map[string]any) {
		method := paymentMethodSnapshotFromNode(node)
		if method.ID == "" || seen[method.ID] {
			return
		}
		seen[method.ID] = true
		out = append(out, method)
	}
	switch raw := data["paymentMethods"].(type) {
	case []any:
		for _, item := range raw {
			if node, ok := item.(map[string]any); ok {
				add(node)
			}
		}
	case map[string]any:
		if edges, ok := nestedSlice(raw, "edges"); ok {
			for _, edge := range edges {
				if edgeMap, ok := edge.(map[string]any); ok {
					if node, ok := edgeMap["node"].(map[string]any); ok {
						add(node)
					}
				}
			}
		} else {
			add(raw)
		}
	}
	if node, ok := data["userDefaultPayment"].(map[string]any); ok {
		method := paymentMethodSnapshotFromNode(node)
		method.Default = true
		if method.ID != "" && !seen[method.ID] {
			out = append(out, method)
		}
	}
	return out, nil
}

func paymentMethodSnapshotFromNode(node map[string]any) PaymentMethodSnapshot {
	return PaymentMethodSnapshot{
		ID:              stringValue(node, "id"),
		CardDescription: stringValue(node, "cardDescription"),
		CardMetadata:    paymentMethodMetadataString(node["cardMetadata"]),
		CardType:        stringValue(node, "cardType"),
		Gateway:         stringValue(node, "gateway"),
		Default:         boolValue(node, "default"),
	}
}

func paymentMethodMetadataString(raw any) string {
	if raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return strings.TrimSpace(fmt.Sprint(value))
		}
		return string(encoded)
	}
}

func cardLast4Utility(number string) string {
	digits := ""
	for _, char := range number {
		if char >= '0' && char <= '9' {
			digits += string(char)
		}
	}
	if len(digits) <= 4 {
		return digits
	}
	return digits[len(digits)-4:]
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
