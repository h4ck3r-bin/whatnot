package whtnt

import (
	"encoding/json"
	"fmt"
	"strings"
)

type purchaseOverview struct {
	ID              string
	UUID            string
	Status          string
	PrettyStatus    string
	CreatedAt       string
	SellerUsername  string
	IsReviewPending bool
	ListingID       string
	ListingTitle    string
	HasUnread       bool
	ConversationID  string
	RefundStatus    string
	SupportChip     string
}

type orderDetailSnapshot struct {
	UUID              string
	ID                string
	DisplayID         string
	PrettyStatus      string
	BuyerUsername     string
	SellerUsername    string
	IsReviewPending   bool
	Subtotal          string
	Shipping          string
	AuthenticationFee string
	Taxes             string
	Credit            string
	Total             string
	ShippingAddress   string
	ConversationID    string
	Items             []string
	ListingIDs        []string
	Shipments         []shipmentSnapshot
}

type shipmentSnapshot struct {
	ID                  string
	Status              string
	BuyerFacingStatus   string
	Method              string
	TrackingCode        string
	TrackingURL         string
	ShippingServiceName string
	ETA                 string
}

func (m *Module) getMyPurchases() ([]purchaseOverview, error) {
	query := `query MyPurchases($first: Int!, $cursor: String, $status: [String]!, $filters: [OrderFilter!]) @attribution(owner: "selling") { myOrders(first: $first, status: $status, filters: $filters, after: $cursor) { __typename pageInfo { __typename ...PageInfoFragment } edges { __typename node { __typename id uuid status prettyStatus isReviewPending createdAt items(first: 1) { __typename edges { __typename node { __typename id listing { __typename id title images { __typename id key bucket } transactionType order { __typename id total { __typename ...Money } conversation { __typename id hasUnreadDirectMessages } items(first: 1) { __typename edges { __typename node { __typename id shipment { __typename id status pickupRequest { __typename id status } trackingMetadata { __typename eta } } } } } } } livestreamProduct { __typename id } } } } seller { __typename id username } refundRequest { __typename id prettyStatus } supportRequest { __typename id ordersPageChip { __typename text variant } } conversation { __typename id hasUnreadDirectMessages } } } } } fragment PageInfoFragment on PageInfo @attribution(owner: "app-platform") { __typename startCursor endCursor hasNextPage hasPreviousPage } fragment Money on Money @attribution(owner: "app-platform") { __typename amountSafe currency }`

	variants := []struct {
		label     string
		variables map[string]any
	}{
		{
			label: "all statuses",
			variables: map[string]any{
				"first":   50,
				"cursor":  nil,
				"status":  []string{},
				"filters": []any{},
			},
		},
		{
			label: "default status fallback",
			variables: map[string]any{
				"first":   20,
				"cursor":  nil,
				"status":  []string{"paid", "shipped", "delivered"},
				"filters": []any{},
			},
		},
	}

	var lastErr error
	for index, variant := range variants {
		orders, err := m.getMyPurchasesVariant(query, variant.variables)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", variant.label, err)
			continue
		}
		if len(orders) > 0 {
			if index > 0 {
				m.log.Scope("MyPurchases returned data after status fallback.")
			}
			return orders, nil
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("get MyPurchases: %w", lastErr)
	}
	return nil, nil
}

func (m *Module) getMyPurchasesVariant(query string, variables map[string]any) ([]purchaseOverview, error) {
	out := []purchaseOverview{}
	seen := map[string]bool{}
	cursor := ""
	for pageNumber := 0; pageNumber < 20; pageNumber++ {
		pageVariables := map[string]any{}
		for key, value := range variables {
			pageVariables[key] = value
		}
		if cursor == "" {
			pageVariables["cursor"] = nil
		} else {
			pageVariables["cursor"] = cursor
		}

		res, err := m.sendMobileCheckoutRequest("MyPurchases", pageVariables, query)
		if err != nil {
			return nil, err
		}

		page, err := parseMyPurchasesPage(res.String())
		if err != nil {
			return nil, err
		}
		for _, order := range page.Orders {
			key := firstNonEmptyString(order.UUID, order.ID)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, order)
		}
		if !page.HasNextPage || strings.TrimSpace(page.EndCursor) == "" || page.EndCursor == cursor {
			break
		}
		cursor = page.EndCursor
	}
	return out, nil
}

func (m *Module) getOrderDetailBuyerSnapshot(orderUUID string) (orderDetailSnapshot, error) {
	res, err := m.sendMobileCheckoutRequest(
		"OrderDetailBuyer",
		map[string]any{
			"orderUuid": orderUUID,
		},
		`query OrderDetailBuyer($orderUuid: ID!) @attribution(owner: "selling") { myOrder(uuid: $orderUuid) { __typename ...SharedOrderFields items { __typename edges { __typename node { __typename listing { __typename ...OrderListing user { __typename ...SellerDetails } } ...OrderItem } } } } } fragment OrderUser on PublicUserNode @attribution(owner: "selling") { __typename id username } fragment Address on AddressNode @attribution(owner: "app-platform") { __typename id fullName line1 line2 city state postalCode countryCode defaultShipping returnAddress isPickupAddress coordinates { __typename longitude latitude } } fragment Money on Money @attribution(owner: "app-platform") { __typename amountSafe currency } fragment OrderConversation on ConversationNode @attribution(owner: "selling") { __typename id hasUnreadDirectMessages amAllowedToMessage lastDirectMessage { __typename id body } } fragment SharedOrderFields on OrderNode @attribution(owner: "selling") { __typename id displayId uuid status prettyStatus createdAt processedAt isReviewPending buyer { __typename ...OrderUser } shippingAddress { __typename ...Address } subtotal { __typename ...Money } shippingPrice { __typename ...Money } authenticationFee { __typename ...Money } taxes { __typename ...Money } credit { __typename ...Money } total { __typename ...Money } conversation { __typename ...OrderConversation } actions { __typename action label description } livestream { __typename id startTime } channelReference returnWindowMinutes videoReceipt { __typename videoUrl expirationMessage status } refundRequest { __typename id status prettyStatus } supportRequest { __typename id bannerTitle bannerDescription } } fragment OrderListing on ListingNode @attribution(owner: "selling") { __typename id title subtitle description status actions { __typename action } images { __typename id key bucket } user { __typename id } } fragment SellerDetails on PublicUserNode @attribution(owner: "selling") { __typename id username sellerRating { __typename overall numReviews } } fragment OrderItem on OrderItemNode @attribution(owner: "selling") { __typename id status orderId shipment { __typename id status buyerFacingStatus method trackingCode trackingUrl shippingServiceName trackingMetadata { __typename title eta } } quantity actions { __typename action label description } }`,
	)
	if err != nil {
		return orderDetailSnapshot{}, fmt.Errorf("get OrderDetailBuyer: %w", err)
	}

	return parseOrderDetailSnapshot(res.String())
}

type myPurchasesPage struct {
	Orders      []purchaseOverview
	EndCursor   string
	HasNextPage bool
}

func parseMyPurchasesPage(body string) (myPurchasesPage, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return myPurchasesPage{}, fmt.Errorf("parse MyPurchases response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return myPurchasesPage{}, fmt.Errorf("graphql errors: %s", shortResponse(body))
	}

	dataNode, ok := payload["data"].(map[string]any)
	if !ok {
		return myPurchasesPage{}, fmt.Errorf("MyPurchases response did not contain data")
	}

	root, ok := nestedMap(dataNode, "myOrders")
	if !ok {
		return myPurchasesPage{}, fmt.Errorf("MyPurchases response missing myOrders")
	}

	page := myPurchasesPage{}
	if pageInfo, ok := root["pageInfo"].(map[string]any); ok {
		page.EndCursor = stringValue(pageInfo, "endCursor")
		page.HasNextPage = boolValue(pageInfo, "hasNextPage")
	}

	edges, ok := nestedSlice(root, "edges")
	if !ok {
		return page, nil
	}

	orders := make([]purchaseOverview, 0, len(edges))
	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}

		node, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}

		order := purchaseOverview{
			ID:              stringValue(node, "id"),
			UUID:            stringValue(node, "uuid"),
			Status:          stringValue(node, "status"),
			PrettyStatus:    stringValue(node, "prettyStatus"),
			CreatedAt:       stringValue(node, "createdAt"),
			SellerUsername:  nestedString(node, "seller", "username"),
			IsReviewPending: boolValue(node, "isReviewPending"),
			ListingID:       firstItemListingID(node),
			ListingTitle:    firstItemListingTitle(node),
			HasUnread:       nestedBool(node, "conversation", "hasUnreadDirectMessages"),
			ConversationID:  nestedString(node, "conversation", "id"),
			RefundStatus:    nestedString(node, "refundRequest", "prettyStatus"),
			SupportChip:     nestedString(node, "supportRequest", "ordersPageChip", "text"),
		}

		if order.UUID == "" && order.ID == "" {
			continue
		}
		orders = append(orders, order)
	}

	page.Orders = orders
	return page, nil
}

func parseOrderDetailSnapshot(body string) (orderDetailSnapshot, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return orderDetailSnapshot{}, fmt.Errorf("parse OrderDetailBuyer response: %w", err)
	}
	if errorsNode, ok := payload["errors"]; ok && errorsNode != nil {
		return orderDetailSnapshot{}, fmt.Errorf("graphql errors: %s", shortResponse(body))
	}

	dataNode, ok := payload["data"].(map[string]any)
	if !ok {
		return orderDetailSnapshot{}, fmt.Errorf("OrderDetailBuyer response did not contain data")
	}

	root, ok := nestedMap(dataNode, "myOrder")
	if !ok {
		return orderDetailSnapshot{}, fmt.Errorf("OrderDetailBuyer response missing myOrder")
	}

	detail := orderDetailSnapshot{
		UUID:              stringValue(root, "uuid"),
		ID:                stringValue(root, "id"),
		DisplayID:         stringValue(root, "displayId"),
		PrettyStatus:      firstNonEmptyString(stringValue(root, "prettyStatus"), stringValue(root, "status")),
		BuyerUsername:     nestedString(root, "buyer", "username"),
		SellerUsername:    firstDetailSeller(root),
		IsReviewPending:   boolValue(root, "isReviewPending"),
		Subtotal:          moneyLine(root, "subtotal"),
		Shipping:          moneyLine(root, "shippingPrice"),
		AuthenticationFee: moneyLine(root, "authenticationFee"),
		Taxes:             moneyLine(root, "taxes"),
		Credit:            moneyLine(root, "credit"),
		Total:             moneyLine(root, "total"),
		Items:             detailListingTitles(root),
		ListingIDs:        detailListingIDs(root),
		ConversationID:    nestedString(root, "conversation", "id"),
		Shipments:         detailShipmentSnapshots(root),
	}
	if shippingAddress, ok := root["shippingAddress"].(map[string]any); ok {
		detail.ShippingAddress = joinNonEmpty([]string{
			stringValue(shippingAddress, "fullName"),
			stringValue(shippingAddress, "line1"),
			stringValue(shippingAddress, "line2"),
			stringValue(shippingAddress, "city"),
			stringValue(shippingAddress, "state"),
			stringValue(shippingAddress, "postalCode"),
			stringValue(shippingAddress, "countryCode"),
		}, ", ")
	}
	if detail.UUID == "" && detail.ID == "" {
		return orderDetailSnapshot{}, fmt.Errorf("OrderDetailBuyer returned no readable identifiers: %s", shortResponse(body))
	}
	return detail, nil
}

func nestedMap(root map[string]any, path ...string) (map[string]any, bool) {
	current := root
	for _, segment := range path {
		next, ok := current[segment].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}

	return current, true
}

func nestedSlice(root map[string]any, key string) ([]any, bool) {
	raw, ok := root[key].([]any)
	return raw, ok
}

func nestedString(root map[string]any, path ...string) string {
	current := root
	for index, segment := range path {
		value, ok := current[segment]
		if !ok || value == nil {
			return ""
		}

		if index == len(path)-1 {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			default:
				return strings.TrimSpace(fmt.Sprintf("%v", typed))
			}
		}

		next, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		current = next
	}

	return ""
}

func nestedBool(root map[string]any, path ...string) bool {
	current := root
	for index, segment := range path {
		value, ok := current[segment]
		if !ok || value == nil {
			return false
		}

		if index == len(path)-1 {
			typed, ok := value.(bool)
			return ok && typed
		}

		next, ok := value.(map[string]any)
		if !ok {
			return false
		}
		current = next
	}

	return false
}

func stringValue(root map[string]any, key string) string {
	value, ok := root[key]
	if !ok || value == nil {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func boolValue(root map[string]any, key string) bool {
	value, ok := root[key]
	if !ok || value == nil {
		return false
	}

	typed, ok := value.(bool)
	return ok && typed
}

func firstItemListingTitle(root map[string]any) string {
	titles := itemListingTitles(root)
	if len(titles) == 0 {
		return ""
	}
	return titles[0]
}

func firstItemListingID(root map[string]any) string {
	ids := itemListingIDs(root)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func itemListingTitles(root map[string]any) []string {
	return itemListingFieldValues(root, "title")
}

func itemListingIDs(root map[string]any) []string {
	return itemListingFieldValues(root, "id")
}

func itemListingFieldValues(root map[string]any, field string) []string {
	itemsMap, ok := root["items"].(map[string]any)
	if !ok {
		return nil
	}

	edges, ok := nestedSlice(itemsMap, "edges")
	if !ok {
		return nil
	}

	values := make([]string, 0, len(edges))
	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}

		nodeMap, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}

		listingMap, ok := nodeMap["listing"].(map[string]any)
		if !ok {
			continue
		}

		value := stringValue(listingMap, field)
		if value != "" {
			values = append(values, value)
		}
	}

	return values
}

func detailListingTitles(root map[string]any) []string {
	return detailListingFieldValues(root, "title")
}

func detailListingIDs(root map[string]any) []string {
	return detailListingFieldValues(root, "id")
}

func detailListingFieldValues(root map[string]any, field string) []string {
	itemsMap, ok := root["items"].(map[string]any)
	if !ok {
		return nil
	}

	edges, ok := nestedSlice(itemsMap, "edges")
	if !ok {
		return nil
	}

	values := make([]string, 0, len(edges))
	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}

		nodeMap, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}

		listingMap, ok := nodeMap["listing"].(map[string]any)
		if !ok {
			continue
		}

		value := stringValue(listingMap, field)
		if value != "" {
			values = append(values, value)
		}
	}

	return values
}

func firstDetailSeller(root map[string]any) string {
	itemsMap, ok := root["items"].(map[string]any)
	if !ok {
		return ""
	}

	edges, ok := nestedSlice(itemsMap, "edges")
	if !ok {
		return ""
	}

	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}

		nodeMap, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}

		listingMap, ok := nodeMap["listing"].(map[string]any)
		if !ok {
			continue
		}

		userMap, ok := listingMap["user"].(map[string]any)
		if !ok {
			continue
		}

		username := stringValue(userMap, "username")
		if username != "" {
			return username
		}
	}

	return ""
}

func detailShipmentSnapshots(root map[string]any) []shipmentSnapshot {
	itemsMap, ok := root["items"].(map[string]any)
	if !ok {
		return nil
	}

	edges, ok := nestedSlice(itemsMap, "edges")
	if !ok {
		return nil
	}

	shipments := make([]shipmentSnapshot, 0, len(edges))
	for _, edge := range edges {
		edgeMap, ok := edge.(map[string]any)
		if !ok {
			continue
		}

		nodeMap, ok := edgeMap["node"].(map[string]any)
		if !ok {
			continue
		}

		shipmentMap, ok := nodeMap["shipment"].(map[string]any)
		if !ok {
			continue
		}

		shipments = append(shipments, shipmentSnapshot{
			ID:                  stringValue(shipmentMap, "id"),
			Status:              stringValue(shipmentMap, "status"),
			BuyerFacingStatus:   stringValue(shipmentMap, "buyerFacingStatus"),
			Method:              stringValue(shipmentMap, "method"),
			TrackingCode:        stringValue(shipmentMap, "trackingCode"),
			TrackingURL:         stringValue(shipmentMap, "trackingUrl"),
			ShippingServiceName: stringValue(shipmentMap, "shippingServiceName"),
			ETA:                 nestedString(shipmentMap, "trackingMetadata", "eta"),
		})
	}

	return shipments
}

func moneyLine(root map[string]any, key string) string {
	moneyMap, ok := root[key].(map[string]any)
	if !ok {
		return ""
	}

	amount := stringValue(moneyMap, "amountSafe")
	currency := stringValue(moneyMap, "currency")
	return joinNonEmpty([]string{amount, currency}, " ")
}

func joinNonEmpty(parts []string, separator string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}

	return strings.Join(filtered, separator)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
