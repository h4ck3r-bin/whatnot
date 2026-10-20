package whtnt

import (
	"bytes"
	crand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go/v5"
	http "github.com/bogdanfinn/fhttp"
)

type gqlBody[Variables any] struct {
	Extensions    any       `json:"extensions,omitempty"`
	OperationName string    `json:"operationName"`
	Query         string    `json:"query"`
	Variables     Variables `json:"variables,omitempty"`
}

type datadogTrace struct {
	traceparent  string
	tracestate   string
	baggage      string
	traceID      string
	parentID     string
	samplingPrio string
	tags         string
}

func generateDatadogTrace(baggageSessionID string) datadogTrace {
	tidHex := fmt.Sprintf("%08x", time.Now().Unix())
	var traceLoBytes [8]byte
	crand.Read(traceLoBytes[:])
	traceLoHex := hex.EncodeToString(traceLoBytes[:])
	var parentBytes [8]byte
	crand.Read(parentBytes[:])
	parentHex := hex.EncodeToString(parentBytes[:])

	traceLoInt := binary.BigEndian.Uint64(traceLoBytes[:])
	parentInt := binary.BigEndian.Uint64(parentBytes[:])

	return datadogTrace{
		traceparent:  fmt.Sprintf("00-%s0000000000000000%s-%s-01", tidHex, traceLoHex, parentHex),
		tracestate:   fmt.Sprintf("dd=o:rum;p:%s;s:1", parentHex),
		baggage:      fmt.Sprintf("session.id=%s,user.id=", baggageSessionID),
		traceID:      fmt.Sprintf("%d", traceLoInt),
		parentID:     fmt.Sprintf("%d", parentInt),
		samplingPrio: "1",
		tags:         fmt.Sprintf("_dd.p.tid=%s00000000", tidHex),
	}
}

func isTracingOperation(name string) bool {
	switch name {
	case "MyUserData", "bindingToLoggedOutUser", "GeoIpSuggestedCountryQuery":
		return true
	}
	return false
}

func (m *Module) sendGqlRequest(operationName string, body gqlBody[any]) (*httpResponse, error) {
	screen := gqlScreenForOperation(operationName)
	return m.sendGqlRequestWithScreen(operationName, body, screen)
}

func (m *Module) sendGqlRequestWithScreen(operationName string, body gqlBody[any], screen string) (*httpResponse, error) {
	if err := m.ensureSessionFreshForAction(operationName); err != nil {
		return nil, err
	}
	return retry.NewWithData[*httpResponse](
		retry.Attempts(3),
		retry.Delay(time.Second*1),
		retry.RetryIf(func(err error) bool { return !errors.Is(err, errKasadaBanned) }),
	).Do(func() (*httpResponse, error) {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		if operationName == "SetPhoneNumberMutationV2" {
			payload = bytes.ReplaceAll(payload, []byte("/"), []byte("\\/"))
		}

		opType := "query"
		if strings.HasPrefix(strings.TrimSpace(body.Query), "mutation") {
			opType = "mutation"
		}

		auth := "Cookie"
		if m.accessToken != "" {
			auth = "Bearer " + m.accessToken
		}

		headers := http.Header{
			"x-system-version":             {m.seonIOSVersion()},
			"x-client-timezone":            {m.deviceTimezone()},
			"user-agent":                   {m.whatnotIOSUserAgent(m.seonIOSVersion(), m.deviceMachine())},
			"x-whatnot-app-build":          {m.whatnotIOSAppBuild()},
			"x-apollo-operation-name":      {operationName},
			"x-whatnot-app":                {"whatnot-ios"},
			"x-datadog-origin":             {"rum"},
			"apollographql-client-name":    {"com.whatnot-apollo-ios"},
			"priority":                     {"u=3, i"},
			"apollographql-client-version": {m.whatnotIOSApolloClientVersion()},
			"x-system-name":                {"iOS"},
			"x-whatnot-app-version":        {m.whatnotIOSAppVersion()},
			"x-whatnot-app-screen":         {screen},
			"authorization":                {auth},
			"accept-language":              {m.deviceAcceptLanguage()},
			"x-request-operation-tag":      {"gql_sdk_version=" + m.whatnotIOSApolloClientVersion()},
			"accept":                       {"multipart/mixed;deferSpec=20220824,application/graphql-response+json,application/json"},
			"content-type":                 {"application/json"},
			"x-whatnot-app-session-id":     {strings.ToLower(m.session.appSessionID)},
			"accept-encoding":              {"gzip, deflate, br"},
			"x-apollo-operation-type":      {opType},
			http.HeaderOrderKey: {
				"x-system-version", "x-client-timezone", "cookie", "user-agent",
				"x-whatnot-app-build", "x-apollo-operation-name", "x-whatnot-app",
				"x-datadog-origin", "apollographql-client-name", "x-whatnot-usgmt",
				"priority", "apollographql-client-version", "x-system-name",
				"x-whatnot-app-version", "x-whatnot-app-screen", "content-length",
				"authorization", "accept-language", "x-request-operation-tag",
				"x-whatnot-view-id", "accept", "content-type",
				"x-whatnot-app-session-id", "accept-encoding", "x-apollo-operation-type",
			},
		}

		if isTracingOperation(operationName) && m.baggageSessionID != "" {
			dd := generateDatadogTrace(m.baggageSessionID)
			headers.Set("traceparent", dd.traceparent)
			headers.Set("tracestate", dd.tracestate)
			headers.Set("baggage", dd.baggage)
			headers.Set("x-datadog-trace-id", dd.traceID)
			headers.Set("x-datadog-parent-id", dd.parentID)
			headers.Set("x-datadog-sampling-priority", dd.samplingPrio)
			headers.Set("x-datadog-tags", dd.tags)
			headers[http.HeaderOrderKey] = []string{
				"x-datadog-origin", "accept", "x-whatnot-view-id", "x-datadog-tags",
				"accept-encoding", "content-length", "user-agent", "x-system-name",
				"cookie", "apollographql-client-name", "apollographql-client-version",
				"priority", "x-whatnot-app-version", "x-whatnot-app",
				"x-datadog-sampling-priority", "x-apollo-operation-type",
				"x-whatnot-app-screen", "accept-language", "x-client-timezone",
				"x-system-version", "x-whatnot-app-session-id", "x-whatnot-app-build",
				"x-apollo-operation-name", "authorization", "content-type",
				"traceparent", "x-datadog-parent-id", "x-datadog-trace-id",
				"x-request-operation-tag", "tracestate", "baggage",
			}
		} else {
			if m.usgmtReady {
				headers.Set("x-whatnot-usgmt", ",F,")
			}
		}
		if m.viewID != "" {
			headers.Set("x-whatnot-view-id", m.viewID)
		}

		res, err := m.postRequestWithRetry(
			"graphql "+operationName,
			m.graphQLEndpoint(operationName),
			payload,
			headers,
		)

		if err != nil {
			return nil, fmt.Errorf("request failed: %w", err)
		}

		if res.StatusCode != http.StatusOK {
			if isInvalidTokenResponse(res) {
				if err := m.refreshCheckoutTokens(operationName); err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("graphql %s returned invalid token; refreshed session and retrying", operationName)
			}
			if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
				return nil, errKasadaBanned
			}

			return nil, fmt.Errorf("graphql %s returned %s: %s", operationName, res.Status, shortResponse(res.String()))
		}

		return res, nil
	})
}

func gqlScreenForOperation(operationName string) string {
	switch operationName {
	case "bindingToLoggedOutUser",
		"RegisterUserDevice",
		"GetAccountAlert",
		"GetAccountControlsAlarms",
		"AcceptTerms",
		"MyUserData",
		"InitialMe",
		"GetStatsigBootstrapConfig":
		return "login_sign_up"

	case "GeoIpSuggestedCountryQuery",
		"CompleteOnboardingStep":
		return "onboarding_country_selection"

	// Deprecated: kept for revert path.
	case "UpdateOnboardingCountryMutation":
		return "onboarding_country_selection"

	case "bulkFollowEntities":
		return "onboarding_primary_category_selection"

	// Deprecated: kept for revert path.
	case "BulkFollowEntities":
		return "onboarding_primary_category_selection"

	case "UpdateUsernameMutation",
		"UsernameSuggestions",
		"SetHasInterestToSell",
		"GetUserId":
		return "onboarding_username_selection"

	// Deprecated: kept for revert path.
	case "UpdateUsername":
		return "onboarding_username_selection"

	case "SetPhoneNumberMutationV2",
		"MainApp_Me":
		return "login_enter_confirmation_code"

	// Deprecated: kept for revert path.
	case "SetPhoneNumberV2Mutation":
		return "login_enter_confirmation_code"

	case "VerifyPhoneNumberMutation",
		"UnauthSplash_GetReferralProgram",
		"UnauthSplash_GetUser",
		"GetReferredByUserWithUpcomingShow":
		return "onboarding_phone_verification"

	case "Referral__Me_referredByName":
		return "login_enter_confirmation_code"

	case "revealReferralCredit",
		"grantReferralCredit":
		return "onboarding_referral_credit_congrats"

	case "ViewOnboardingContactsSyncPreprompt":
		return "onboarding_gender_selection"

	case "ViewOnboardingFollowTopSeller":
		return "onboarding_referral_bookmark_show"

	case "enrollInNewUserIncentives":
		return "onboarding_referral_bookmark_show"

	case "livestreamTaxonomyDirectDescendants",
		"followedTags":
		return "onboarding_interests_selection"

	default:
		return "login_sign_up"
	}
}
