package referrors

import (
	"fmt"
	"strings"
)

const (
	StatusIneligible       = "INELIGIBLE"
	TitleReferrerNotLocate = "We are unable to locate your referrer"
)

type ApplyError struct {
	Code   string
	Status string
	Title  string
	Detail string
}

// InvalidReferrer builds the error the engine uses to rotate friend codes when a referral
// code is rejected outside the GraphQL apply flow, e.g. by the register endpoint.
func InvalidReferrer(code string, detail string) ApplyError {
	return ApplyError{
		Code:   code,
		Status: StatusIneligible,
		Title:  TitleReferrerNotLocate,
		Detail: detail,
	}
}

func (e ApplyError) Error() string {
	return fmt.Sprintf("referral apply failed for %s: status=%s title=%s detail=%s", e.Code, e.Status, e.Title, e.Detail)
}

func (e ApplyError) IsInvalidReferrer() bool {
	return strings.EqualFold(strings.TrimSpace(e.Status), StatusIneligible) &&
		strings.EqualFold(strings.TrimSpace(e.Title), TitleReferrerNotLocate)
}

func (e ApplyError) IsPendingPhoneVerification() bool {
	return strings.EqualFold(strings.TrimSpace(e.Status), "PENDING_PHONE_VERIFICATION")
}

func (e ApplyError) IsPhoneNumberAlreadyUsed() bool {
	title := strings.ToLower(strings.TrimSpace(e.Title))
	return strings.EqualFold(strings.TrimSpace(e.Status), StatusIneligible) &&
		strings.Contains(title, "phone number has already been used")
}

func (e ApplyError) NeedsShippingAddress() bool {
	title := strings.ToLower(strings.TrimSpace(e.Title))
	return strings.EqualFold(strings.TrimSpace(e.Status), StatusIneligible) &&
		strings.Contains(title, "provide a shipping address")
}
