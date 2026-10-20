package engine

import (
	"fmt"
	"strings"
)

func findAccountIndex(accounts []Account, id string) int {
	for i, account := range accounts {
		if account.ID == id {
			return i
		}
	}
	return -1
}

func findMasterIndex(masters []MasterAccount, id string) int {
	for i, master := range masters {
		if master.ID == id {
			return i
		}
	}
	return -1
}

func findOrderIndex(orders []Order, id string) int {
	for i, order := range orders {
		if order.ID == id {
			return i
		}
	}
	return -1
}

func findMasterByAccountIndex(masters []MasterAccount, accountID string) int {
	for i, master := range masters {
		if master.AccountID == accountID {
			return i
		}
	}
	return -1
}

func findSession(sessions []SessionRecord, id string) (SessionRecord, bool) {
	for _, session := range sessions {
		if session.ID == id {
			return session, true
		}
	}
	return SessionRecord{}, false
}

func findSessionForAccount(sessions []SessionRecord, account Account) (SessionRecord, bool) {
	if session, ok := findSession(sessions, account.SessionID); ok {
		return session, true
	}
	for _, session := range sessions {
		if account.ID != "" && session.AccountID == account.ID {
			return session, true
		}
		if account.Email != "" && strings.EqualFold(metadataValue(session.Metadata, "email"), account.Email) {
			return session, true
		}
		if account.Username != "" && strings.EqualFold(metadataValue(session.Metadata, "username"), account.Username) {
			return session, true
		}
	}
	return SessionRecord{}, false
}

func findSessionForMaster(sessions []SessionRecord, master MasterAccount) (SessionRecord, bool) {
	if session, ok := findSession(sessions, master.SessionID); ok {
		return session, true
	}
	for _, session := range sessions {
		if master.AccountID != "" && session.AccountID == master.AccountID {
			return session, true
		}
		if master.Email != "" && strings.EqualFold(metadataValue(session.Metadata, "email"), master.Email) {
			return session, true
		}
		if master.Username != "" && strings.EqualFold(metadataValue(session.Metadata, "username"), master.Username) {
			return session, true
		}
	}
	return SessionRecord{}, false
}

func upsertAccount(accounts []Account, account Account) []Account {
	if index := findAccountIndex(accounts, account.ID); index >= 0 {
		accounts[index] = account
		return accounts
	}
	return append(accounts, account)
}

func upsertOrder(orders []Order, order Order) []Order {
	for i := range orders {
		if orders[i].ID == order.ID || (orders[i].ExternalOrderID != "" && orders[i].ExternalOrderID == order.ExternalOrderID && orders[i].AccountID == order.AccountID) {
			orders[i] = mergeOrder(orders[i], order)
			return orders
		}
	}
	return append(orders, order)
}

func mergeOrder(existing Order, incoming Order) Order {
	if incoming.ID != "" {
		existing.ID = incoming.ID
	}
	if incoming.AccountID != "" {
		existing.AccountID = incoming.AccountID
	}
	if incoming.ExternalOrderID != "" {
		existing.ExternalOrderID = incoming.ExternalOrderID
	}
	if incoming.State != "" && existing.State != OrderStateReviewed {
		existing.State = incoming.State
	}
	if incoming.TrackingNumber != "" {
		existing.TrackingNumber = incoming.TrackingNumber
	}
	existing.CheckoutWebhookSent = existing.CheckoutWebhookSent || incoming.CheckoutWebhookSent
	existing.TrackingWebhookSent = existing.TrackingWebhookSent || incoming.TrackingWebhookSent
	existing.ReviewCompleted = existing.ReviewCompleted || incoming.ReviewCompleted
	existing.ReviewWebhookSent = existing.ReviewWebhookSent || incoming.ReviewWebhookSent
	if incoming.ReviewReferenceID != "" {
		existing.ReviewReferenceID = incoming.ReviewReferenceID
	}
	if existing.CreatedAt.IsZero() {
		existing.CreatedAt = incoming.CreatedAt
	}
	if !incoming.UpdatedAt.IsZero() {
		existing.UpdatedAt = incoming.UpdatedAt
	}
	if incoming.LastCheckedAt != nil {
		existing.LastCheckedAt = incoming.LastCheckedAt
	}
	if incoming.LastError != "" {
		existing.LastError = incoming.LastError
	}
	if existing.Metadata == nil {
		existing.Metadata = map[string]any{}
	}
	for key, value := range incoming.Metadata {
		existing.Metadata[key] = value
	}
	return existing
}

func upsertMaster(masters []MasterAccount, master MasterAccount) []MasterAccount {
	if index := findMasterIndex(masters, master.ID); index >= 0 {
		masters[index] = master
		return masters
	}
	return append(masters, master)
}

func upsertSession(sessions []SessionRecord, session SessionRecord) []SessionRecord {
	for i := range sessions {
		if sessions[i].ID == session.ID ||
			(sessions[i].AccountID != "" && session.AccountID != "" && sessions[i].AccountID == session.AccountID) ||
			(metadataValue(sessions[i].Metadata, "email") != "" && metadataValue(session.Metadata, "email") != "" && strings.EqualFold(metadataValue(sessions[i].Metadata, "email"), metadataValue(session.Metadata, "email"))) {
			sessions[i] = session
			return sessions
		}
	}
	return append(sessions, session)
}

func validateRefreshedSession(existing SessionRecord, refreshed SessionRecord) error {
	if existing.ID != "" && refreshed.ID != "" && existing.ID != refreshed.ID {
		return fmt.Errorf("session id changed from %s to %s", existing.ID, refreshed.ID)
	}
	if len(existing.Cookies) > 0 && !hasCookieValues(refreshed.Cookies) {
		return fmt.Errorf("cookies are empty")
	}
	for _, name := range []string{"__Secure-access-token", "__Secure-refresh-token"} {
		if sessionCookieValue(existing.Cookies, name) != "" && sessionCookieValue(refreshed.Cookies, name) == "" {
			return fmt.Errorf("%s cookie is empty", name)
		}
	}
	if token := metadataValue(existing.Metadata, "whtnt_session_token"); token != "" && metadataValue(refreshed.Metadata, "whtnt_session_token") == "" {
		return fmt.Errorf("session token is empty")
	}
	if !hasCookieValues(refreshed.Cookies) && metadataValue(refreshed.Metadata, "whtnt_session_token") == "" {
		return fmt.Errorf("no cookies or session token returned")
	}
	return nil
}

func hasCookieValues(cookies []CookieRecord) bool {
	for _, cookie := range cookies {
		if strings.TrimSpace(cookie.Name) != "" && strings.TrimSpace(cookie.Value) != "" {
			return true
		}
	}
	return false
}

func sessionCookieValue(cookies []CookieRecord, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}
