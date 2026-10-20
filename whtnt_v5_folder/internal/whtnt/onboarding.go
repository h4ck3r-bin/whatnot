package whtnt

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/smsmanager"
	"status403.com/rsavv/seonsdk-go"
)

const (
	pokemonCategoryID    = "TGl2ZXN0cmVhbVRhZ05vZGU6OTA3"
	defaultSMSPoolRegion = "AT"
	smsCodeRetryDelay    = 30 * time.Second
	smsCodeMaxAttempts   = 3
	userUpdateSurface    = "ONBOARDING"
)

func (m *Module) handleOnboarding() error {
	lib := map[string]any{
		"clientLibrary": m.referralClientLibrary(),
	}

	// --- Country selection ---
	stepStart := time.Now()
	ran, err := m.runCheckpointedRequest("onboarding:geo_ip_country", func() error {
		return m.geoIpAndSelectCountry(lib)
	})
	if err != nil {
		return err
	}
	if ran && m.events != nil {
		m.events.SendAcceptTerms(stepStart)
		m.events.SendCountryStep(stepStart)
	}
	m.sleepDelay("after_update_country")
	m.sleepDelay("onboarding_step")

	// --- Post-country: MyUserData + MainApp_Me + Referral__Me ---
	_, _ = m.runCheckpointedRequest("onboarding:post_country_me", func() error {
		m.gqlBestEffort("MyUserData", gqlBody[any]{
			OperationName: "MyUserData", Query: qMyUserData, Extensions: lib,
		})
		m.gqlBestEffort("MainApp_Me", gqlBody[any]{
			OperationName: "MainApp_Me", Query: qMainAppMe,
			Variables: map[string]any{"referralOnly": true}, Extensions: lib,
		})
		m.gqlBestEffort("Referral__Me_referredByName", gqlBody[any]{
			OperationName: "Referral__Me_referredByName", Query: qReferralMeReferredByName,
			Extensions: lib,
		})
		return nil
	})
	m.sleepDelay("onboarding_step")

	// --- Phone verification ---
	stepStart = time.Now()
	ran, err = m.runCheckpointedRequest("onboarding:set_phone_number", m.setPhoneNumber)
	if err != nil {
		return err
	}
	if ran && m.events != nil {
		m.events.SendPhoneStep(stepStart)
	}
	m.sleepDelay("after_set_phone_number")
	m.sleepDelay("onboarding_step")

	// --- Post-phone: MainApp_Me + CompleteOnboardingStep(phone ack) ---
	_, _ = m.runCheckpointedRequest("onboarding:post_phone_ack", func() error {
		m.gqlBestEffort("MainApp_Me", gqlBody[any]{
			OperationName: "MainApp_Me", Query: qMainAppMe,
			Variables: map[string]any{"referralOnly": true}, Extensions: lib,
		})
		m.sendCompleteOnboardingStepWithScreen(lib, map[string]any{
			"acknowledgePhoneNumberVerification": map[string]any{"acknowledged": true},
		}, "onboarding_phone_verification")
		return nil
	})
	m.sleepDelay("onboarding_step")

	// --- Referral info queries ---
	_, _ = m.runCheckpointedRequest("onboarding:referral_info", func() error {
		m.gqlBestEffort("GetReferredByUserWithUpcomingShow", gqlBody[any]{
			OperationName: "GetReferredByUserWithUpcomingShow", Query: qGetReferredByUserWithUpcomingShow,
			Extensions: lib,
		})
		referralCode := m.resolveReferralCode()
		m.gqlBestEffort("UnauthSplash_GetReferralProgram", gqlBody[any]{
			OperationName: "UnauthSplash_GetReferralProgram", Query: qUnauthSplashGetReferralProgram,
			Variables: map[string]any{"username": referralCode}, Extensions: lib,
		})
		m.gqlBestEffort("UnauthSplash_GetUser", gqlBody[any]{
			OperationName: "UnauthSplash_GetUser", Query: qUnauthSplashGetUser,
			Variables: map[string]any{"username": referralCode}, Extensions: lib,
		})
		return nil
	})

	// --- Passkey skip + grant referral credit ---
	stepStart = time.Now()
	ran, _ = m.runCheckpointedRequest("onboarding:passkey_and_referral", func() error {
		m.sendCompleteOnboardingStepWithScreen(lib, map[string]any{
			"passkey": map[string]any{"registered": false},
		}, "onboarding_referral_credit_congrats")
		// Add longer delay before granting credit to avoid triggering unusual activity detection
		m.sleepDelay("before_referral_credit")
		if err := m.grantReferralCreditWithRetry(lib); err != nil {
			m.log.ManagerError("Failed to grant referral credit, continuing anyway: " + err.Error())
		}
		return nil
	})
	if ran && m.events != nil {
		m.events.SendPasskeyStep(stepStart)
		m.events.SendReferralStep(stepStart)
	}
	m.sleepDelay("onboarding_step")

	// --- Username ---
	stepStart = time.Now()
	ran, err = m.runCheckpointedRequest("onboarding:username_flow", func() error {
		m.gqlBestEffort("UsernameSuggestions", gqlBody[any]{
			OperationName: "UsernameSuggestions", Query: qUsernameSuggestions,
			Variables: map[string]any{"limit": 5}, Extensions: lib,
		})
		m.gqlBestEffort("SetHasInterestToSell", gqlBody[any]{
			OperationName: "SetHasInterestToSell", Query: qSetHasInterestToSell,
			Variables: map[string]any{"hasInterestToSell": false}, Extensions: lib,
		})
		return m.updateUsername()
	})
	if err != nil {
		return err
	}
	if ran && m.events != nil {
		m.events.SetUsername(m.account.username)
		m.events.SendUsernameStep(m.account.username, stepStart)
	}
	m.sleepDelay("after_update_username")
	m.sleepDelay("onboarding_step")

	// --- Contacts sync preprompt + gender selection ---
	stepStart = time.Now()
	ran, _ = m.runCheckpointedRequest("onboarding:gender_step", func() error {
		m.gqlBestEffort("ViewOnboardingContactsSyncPreprompt", gqlBody[any]{
			OperationName: "ViewOnboardingContactsSyncPreprompt", Query: qViewOnboardingContactsSyncPreprompt,
			Extensions: lib,
		})
		m.sendCompleteOnboardingStepWithScreen(lib, map[string]any{
			"genderSelection": map[string]any{"gender": "MALE"},
		}, "onboarding_gender_selection")
		return nil
	})
	if ran && m.events != nil {
		m.events.SendContactSyncStep(stepStart)
	}
	m.sleepDelay("onboarding_step")

	// --- Interests ---
	stepStart = time.Now()
	ran, err = m.runCheckpointedRequest("onboarding:interests", func() error {
		return m.interestsAndFollow(lib)
	})
	if err != nil {
		return err
	}
	if ran && m.events != nil {
		m.events.SendInterestPickerStep(stepStart)
	}
	m.sleepDelay("after_follow_pokemons")
	m.sleepDelay("onboarding_step")

	// --- Final: follow top seller + enroll incentives + feed education ---
	stepStart = time.Now()
	ran, _ = m.runCheckpointedRequest("onboarding:finalize", func() error {
		m.gqlBestEffort("ViewOnboardingFollowTopSeller", gqlBody[any]{
			OperationName: "ViewOnboardingFollowTopSeller", Query: qViewOnboardingFollowTopSeller,
			Extensions: lib,
		})
		m.gqlBestEffort("enrollInNewUserIncentives", gqlBody[any]{
			OperationName: "enrollInNewUserIncentives", Query: qEnrollInNewUserIncentives,
			Extensions: lib,
		})
		m.sendCompleteOnboardingStepWithScreen(lib, map[string]any{
			"acknowledgeFeedEducation": map[string]any{"acknowledged": true},
		}, "onboarding_feed_education")
		return nil
	})
	if ran && m.events != nil {
		m.events.SendFollowSellersStep(stepStart)
		m.events.SendOnboardingFinish(stepStart)
	}

	// --- Post-onboarding: re-register device ---
	m.sendPostOnboardingRegisterDevice(lib)

	return nil
}

// Deprecated: revealReferralCredit is kept for revert path; use grantReferralCreditNew instead.
func (m *Module) revealReferralCredit() error {
	m.log.ManagerNotice("Revealing the referral credit...")

	res, err := m.sendGqlRequest("revealReferralCredit", gqlBody[any]{
		OperationName: "revealReferralCredit",
		Query:         "mutation revealReferralCredit @attribution(owner: \"buyer-growth\") { revealReferralCredit { __typename error reward { __typename amount currency } } }",
		Extensions: map[string]any{
			"clientLibrary": m.referralClientLibrary(),
		},
	})
	if err != nil {
		return err
	}

	var dto revealReferralCreditDto
	if err := res.JSON(&dto); err != nil {
		return fmt.Errorf("parse reveal referral credit response: %w", err)
	}

	result := dto.Data.RevealReferralCredit
	if result == nil {
		return fmt.Errorf("reveal referral credit response missing result: %s", shortResponse(res.String()))
	}
	if strings.TrimSpace(result.Error) != "" {
		return fmt.Errorf("reveal referral credit returned error: %s", strings.TrimSpace(result.Error))
	}
	if result.Reward != nil {
		currency := strings.TrimSpace(result.Reward.Currency)
		if currency == "" {
			currency = "GBP"
		}
		m.log.ManagerNotice(fmt.Sprintf("Referral credit revealed: %v %s.", result.Reward.Amount, currency))
	}
	return nil
}

func (m *Module) grantReferralCreditNew(lib map[string]any) error {
	m.log.ManagerNotice("Granting referral credit...")

	res, err := m.sendGqlRequest("grantReferralCredit", gqlBody[any]{
		OperationName: "grantReferralCredit",
		Query:         qGrantReferralCredit,
		Extensions:    lib,
	})
	if err != nil {
		return err
	}

	var dto revealReferralCreditDto
	if err := res.JSON(&dto); err != nil {
		return fmt.Errorf("parse grant referral credit response: %w", err)
	}

	result := dto.Data.RevealReferralCredit
	if result == nil {
		return fmt.Errorf("grant referral credit response missing result: %s", shortResponse(res.String()))
	}
	if strings.TrimSpace(result.Error) != "" {
		return fmt.Errorf("grant referral credit returned error: %s", strings.TrimSpace(result.Error))
	}
	if result.Reward != nil {
		currency := strings.TrimSpace(result.Reward.Currency)
		if currency == "" {
			currency = "GBP"
		}
		amountSafe := 0
		switch v := result.Reward.Amount.(type) {
		case float64:
			amountSafe = int(v)
		case int:
			amountSafe = v
		}
		reward := &referralMoneyDto{AmountSafe: amountSafe, Currency: currency}
		m.storeReferralReward(reward, reward)
		m.log.ManagerNotice(fmt.Sprintf("Referral credit granted: %v %s.", result.Reward.Amount, currency))
	}
	return nil
}

func (m *Module) grantReferralCreditWithRetry(lib map[string]any) error {
	maxRetries := 3
	baseDelay := 5 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := m.grantReferralCreditNew(lib)
		if err == nil {
			return nil
		}

		// Check if error is due to unusual activity detection
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "unusual activity") {
			if attempt < maxRetries {
				// Exponential backoff: 5s, 10s, 20s
				delay := baseDelay * time.Duration(1<<uint(attempt-1))
				m.log.ManagerNotice(fmt.Sprintf("Unusual activity detected, retrying in %v (attempt %d/%d)...", delay, attempt, maxRetries))
				time.Sleep(delay)
				continue
			}
		}

		// For other errors or final attempt, return the error
		return err
	}

	return fmt.Errorf("failed to grant referral credit after %d attempts", maxRetries)
}

func (m *Module) geoIpAndSelectCountry(lib map[string]any) error {
	m.log.Manager("Querying GeoIP country and selecting country...")

	m.gqlBestEffort("GeoIpSuggestedCountryQuery", gqlBody[any]{
		OperationName: "GeoIpSuggestedCountryQuery",
		Query:         qGeoIpSuggestedCountryQuery,
		Extensions:    lib,
	})

	m.sendCompleteOnboardingStep(lib, map[string]any{
		"countrySelection": map[string]any{
			"country":                "GB",
			"suggestedCountry":       "GB",
			"suggestedCountrySource": "GEO_IP",
			"tosAccepted":            true,
		},
	})

	m.log.Manager("Country selected via CompleteOnboardingStep.")
	return nil
}

func (m *Module) sendCompleteOnboardingStep(lib map[string]any, stepAction map[string]any) {
	m.sendCompleteOnboardingStepWithScreen(lib, stepAction, "onboarding_country_selection")
}

func (m *Module) sendCompleteOnboardingStepWithScreen(lib map[string]any, stepAction map[string]any, screen string) {
	_, err := m.sendGqlRequestWithScreen("CompleteOnboardingStep", gqlBody[any]{
		OperationName: "CompleteOnboardingStep",
		Query:         qCompleteOnboardingStep,
		Variables: map[string]any{
			"input": map[string]any{
				"onboardingStepActionsInput": stepAction,
			},
		},
		Extensions: lib,
	}, screen)
	if err != nil {
		m.log.Manager("CompleteOnboardingStep failed (non-blocking): " + err.Error())
	}
}

func (m *Module) interestsAndFollow(lib map[string]any) error {
	m.log.Manager("Fetching interests and following categories...")

	m.gqlBestEffort("livestreamTaxonomyDirectDescendants", gqlBody[any]{
		OperationName: "livestreamTaxonomyDirectDescendants",
		Query:         qLivestreamTaxonomyDirectDescendants,
		Variables: map[string]any{
			"includeRefinementImage":  true,
			"overridden_taxonomy_key": nil,
			"schedulingLive":          false,
			"starting_entity_id":      nil,
			"use_case":                "ONBOARDING",
			"withViewerCount":         true,
		},
		Extensions: lib,
	})

	m.gqlBestEffort("followedTags", gqlBody[any]{
		OperationName: "followedTags",
		Query:         qFollowedTags,
		Variables: map[string]any{
			"withLivestreamCount": true,
			"withViewerCount":     false,
		},
		Extensions: lib,
	})

	_, err := m.sendGqlRequest("bulkFollowEntities", gqlBody[any]{
		OperationName: "bulkFollowEntities",
		Query:         qBulkFollowEntities,
		Variables: map[string][]string{
			"entityIds": {pokemonCategoryID},
		},
		Extensions: lib,
	})
	if err != nil {
		m.log.ManagerError("Failed to follow entities: " + err.Error())
		return err
	}

	m.sendCompleteOnboardingStepWithScreen(lib, map[string]any{
		"interestSelection": map[string]any{
			"favoriteInterestEntityId": pokemonCategoryID,
			"followedInterests":        []string{pokemonCategoryID},
		},
	}, "onboarding_primary_category_selection")

	m.log.Manager("Interests followed.")
	return nil
}

func (m *Module) sendPostOnboardingRegisterDevice(lib map[string]any) {
	idfv := ""
	if m.seonDevice != nil {
		idfv = strings.ToUpper(m.seonDevice.DeviceUdid())
	}
	apnsToken := eventsRandomHex(64)
	firebaseToken := fmt.Sprintf("%s:%s", base64LikeToken(22), "APA91b"+base64LikeToken(113))

	m.gqlBestEffort("RegisterUserDevice", gqlBody[any]{
		OperationName: "RegisterUserDevice",
		Query:         qRegisterUserDevice,
		Variables: map[string]any{
			"active":            true,
			"apnsEnvironment":   "PRODUCTION",
			"apnsToken":         apnsToken,
			"appVersion":        m.whatnotIOSAppVersion(),
			"brand":             "Apple",
			"deviceName":        "iPhone",
			"deviceUniqueId":    idfv,
			"firebasePushToken": firebaseToken,
			"manufacturer":      "Apple",
			"notifAuthStatus":   "AUTHORIZED",
			"systemVersion":     m.seonIOSVersion(),
		},
		Extensions: lib,
	})
}

// captureReferralReward reveals the credit granted by the referal_code sent during register
// and then reads the resulting balance, because there is no claim response to read the
// amount from. It never blocks the run: failures are logged and onboarding continues.
func (m *Module) captureReferralReward() error {
	if err := m.revealReferralCredit(); err != nil {
		m.log.ManagerError("Failed to reveal referral credit, continuing anyway: " + err.Error())
	}

	m.log.ManagerNotice("Checking the referral credit granted at signup...")

	credits, err := m.GetMyCreditsSnapshot()
	if err != nil {
		m.rewardCreditCheckError = err.Error()
		m.log.ManagerError("Failed to read referral credit, continuing anyway: " + err.Error())
		return nil
	}

	reward := &referralMoneyDto{AmountSafe: credits.AmountSafe, Currency: credits.Currency}
	m.storeReferralReward(reward, reward)
	if credits.AmountSafe <= 0 {
		m.log.Manager("No referral credit was granted for code " + m.referralCode + "; continuing without a reward.")
		return nil
	}

	m.logReferralRewardApplied(m.referralCode, reward)
	return m.notifyRewardDetected()
}

func (m *Module) handleSEONTesterOnboarding() error {
	lib := map[string]any{
		"clientLibrary": m.referralClientLibrary(),
	}

	if _, err := m.runCheckpointedRequest("onboarding:geo_ip_country", func() error {
		return m.geoIpAndSelectCountry(lib)
	}); err != nil {
		return err
	}
	m.sleepDelay("after_update_country")
	m.sleepDelay("onboarding_step")

	if _, err := m.runCheckpointedRequest("onboarding:interests", func() error {
		return m.interestsAndFollow(lib)
	}); err != nil {
		return err
	}
	m.sleepDelay("after_follow_pokemons")
	m.sleepDelay("onboarding_step")

	if _, err := m.runCheckpointedRequest("onboarding:username_flow", func() error {
		m.gqlBestEffort("UsernameSuggestions", gqlBody[any]{
			OperationName: "UsernameSuggestions", Query: qUsernameSuggestions,
			Variables: map[string]any{"limit": 5}, Extensions: lib,
		})
		m.gqlBestEffort("SetHasInterestToSell", gqlBody[any]{
			OperationName: "SetHasInterestToSell", Query: qSetHasInterestToSell,
			Variables: map[string]any{"hasInterestToSell": false}, Extensions: lib,
		})
		return m.updateUsername()
	}); err != nil {
		return err
	}
	m.sleepDelay("after_update_username")
	m.sleepDelay("onboarding_step")

	return nil
}

func (m *Module) acceptBuyerTerms() error {
	m.log.ManagerNotice("Accepting buyer terms...")

	_, err := m.sendGqlRequest("AcceptTerms", gqlBody[any]{
		OperationName: "AcceptTerms",
		Variables: map[string]string{
			"legalEntity": "WHATNOT_EUROPE_LTD",
		},
		Query: "mutation AcceptTerms($legalEntity: LegalEntity) @attribution(owner: \"app-platform\") { acceptBuyerTerms(buyerTermsLegalEntity: $legalEntity) { __typename user { __typename id buyerTermsLegalEntity } } }",
		Extensions: map[string]any{
			"clientLibrary": m.referralClientLibrary(),
		},
	})

	if err != nil {
		m.log.ManagerError("Failed to accept buyer terms: " + err.Error())
		return err
	}

	m.log.ManagerNotice("Buyer terms accepted successfully.")
	return nil
}

func (m *Module) updateCountry() error {
	m.log.Manager("Updating onboarding country...")

	_, err := m.sendGqlRequest("UpdateOnboardingCountryMutation", gqlBody[any]{
		OperationName: "UpdateOnboardingCountryMutation",
		Variables: map[string]string{
			"country": "GB",
			"surface": userUpdateSurface,
		},
		Query: "mutation UpdateOnboardingCountryMutation($country:String!$surface:UserUpdateSurface){updateOnboardingCountry(country:$country surface:$surface)}",
		Extensions: map[string]any{
			"clientLibrary": m.referralClientLibrary(),
		},
	})

	if err != nil {
		m.log.ManagerError("Failed to update country: " + err.Error())
		return err
	}

	m.log.Manager("Onboarding country updated.")
	return nil
}

func (m *Module) followPokemons() error {
	m.log.Manager("Following Pokemon category...")

	_, err := m.sendGqlRequest("BulkFollowEntities", gqlBody[any]{
		OperationName: "BulkFollowEntities",
		Variables: map[string][]string{
			"entityIds": {pokemonCategoryID},
		},
		Query: "mutation BulkFollowEntities($entityIds:[ID]!){bulkFollowEntities(entityIds:$entityIds){success categories{id label image{id url(width:300 height:100 format:WEBP fit:COVER)__typename}__typename}livestream_tags{id label image{id url(width:300 height:100 format:WEBP fit:COVER)__typename}__typename}__typename}}",
		Extensions: map[string]any{
			"clientLibrary": m.referralClientLibrary(),
		},
	})

	if err != nil {
		m.log.ManagerError("Failed to follow Pokemon category: " + err.Error())
		return err
	}

	m.log.Manager("Pokemon category followed.")
	return nil
}

func (m *Module) updateUsername() error {
	m.log.Manager("Setting username...")

	attempts := 3
	if m.config != nil && m.config.Engine.ReviewUsernameAttempts > 0 {
		attempts = m.config.Engine.ReviewUsernameAttempts
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		username := m.generateAccountUsername()
		if username == "" {
			username = fallbackAccountUsername()
		}
		if available, err := m.usernameAvailableWeb(username); err == nil && !available {
			lastErr = fmt.Errorf("username %s is already taken", username)
			continue
		}
		if err := m.updateUsernameWeb(username); err != nil {
			lastErr = err
			m.log.ManagerError("Failed to update username " + username + ": " + err.Error())
			if isUsernameCooldownError(err) {
				break
			}
			continue
		}
		m.account.username = username
		m.usernameAdequate = checkoutUsernameAdequate(username, m.account.firstName, m.account.lastName)
		m.usernameAdequateCheckedAt = time.Now().UTC()
		m.log.Manager("Username set to " + username + ".")
		return nil
	}
	if lastErr != nil {
		return lastErr
	}
	return nil
}

func (m *Module) updateUsernameWeb(username string) error {
	res, err := m.sendGqlRequest("UpdateUsernameMutation", gqlBody[any]{
		OperationName: "UpdateUsernameMutation",
		Variables: map[string]string{
			"surface":  userUpdateSurface,
			"username": username,
		},
		Query:      qUpdateUsernameMutation,
		Extensions: map[string]any{"clientLibrary": m.referralClientLibrary()},
	})
	if err != nil {
		return err
	}
	var dto struct {
		Errors []any `json:"errors"`
		Data   struct {
			UpdateUsername struct {
				User struct {
					Username string `json:"username"`
				} `json:"user"`
			} `json:"updateUsername"`
		} `json:"data"`
	}
	if err := res.JSON(&dto); err != nil {
		return err
	}
	if len(dto.Errors) > 0 {
		return fmt.Errorf("update username returned errors: %s", shortResponse(res.String()))
	}
	if !strings.EqualFold(strings.TrimSpace(dto.Data.UpdateUsername.User.Username), username) {
		return fmt.Errorf("update username did not confirm %s", username)
	}
	return nil
}

func (m *Module) setPhoneNumber() error {
	var lastErr error
	lastProvider := ""
	for attempt := 1; attempt <= smsCodeMaxAttempts; attempt++ {
		provider, err := m.setPhoneNumberOnce(lastProvider)
		if err == nil {
			return nil
		}
		lastErr = err
		if provider != "" {
			lastProvider = provider
		}
		if !isSMSRetryable(err) || attempt == smsCodeMaxAttempts {
			return err
		}
		m.log.PhoneError(fmt.Sprintf("SMS step failed; waiting %s before retry %d/%d with a new number.", smsCodeRetryDelay, attempt+1, smsCodeMaxAttempts))
		time.Sleep(smsCodeRetryDelay)
	}
	return lastErr
}

func (m *Module) setPhoneNumberOnce(previousProvider string) (string, error) {
	cfg, err := appconfig.Load()
	if err != nil {
		return "", err
	}

	smsProvider := chooseSMSProvider(cfg.SMSService, previousProvider)
	smsClient := smsmanager.NewClient(smsmanager.Config{
		Provider:        smsProvider,
		HeroSMSAPIKey:   cfg.HeroSMSKey,
		SMSPoolAPIKey:   cfg.SMSPoolKey,
		SMSBowerAPIKey:  cfg.SMSBowerKey,
		SMSOnlineAPIKey: cfg.SMSOnlineKey,
	})

	smsPhoneNumber := strings.TrimSpace(m.lastSMSPhoneNumber)
	activationID := strings.TrimSpace(m.lastSMSActivationID)

	if smsPhoneNumber != "" && activationID != "" {
		smsProvider = m.lastSMSProvider
		m.log.Phone(fmt.Sprintf("Reusing early-reserved number: %s (%s)", smsPhoneNumber, smsProvider))
	} else {
		smsCountryCode := chooseSMSCountryCode(cfg, smsProvider)
		m.log.Phone(fmt.Sprintf("Using SMS provider: %s (%s)", smsProvider, smsCountryCode))

		activationID, smsPhoneNumber, err = smsClient.ClaimNumber(context.Background(), smsCountryCode, nil)
		if err != nil {
			m.log.PhoneError("Failed to claim SMS number: " + err.Error())
			return smsProvider, err
		}

		smsPhoneNumber = "+" + smsPhoneNumber
		m.lastSMSProvider = smsProvider
		m.lastSMSActivationID = activationID
		m.lastSMSPhoneNumber = smsPhoneNumber
		m.lastSMSCode = ""
		m.autosaveSessionIfChanged("sms number claimed")

		if smsProvider == "herosms" {
			m.log.HeroSMS("Number claimed: " + smsPhoneNumber + ", waiting for code...")
		} else {
			m.log.Phone("Phone number submitted: " + smsPhoneNumber)
		}

		referralCode := m.resolveReferralCode()
		if referralCode != "" {
			if err := m.runWebPreRegistration(smsPhoneNumber, referralCode); err != nil {
				m.log.ManagerError("Web pre-registration for new number failed (non-blocking): " + err.Error())
			}
		}
	}

	if m.seonDevice == nil {
		cancelSMSActivation(smsClient, activationID, m)
		m.clearSMSState()
		return smsProvider, fmt.Errorf("seon device is not available for phone verification")
	}
	var behaviourOpts *devices.BehaviourOptions
	if rand.Float64() >= 0.15 {
		durationMs := 2000 + rand.Intn(8000)
		behaviourOpts = &devices.BehaviourOptions{
			Enabled:             true,
			Duration:            time.Duration(durationMs) * time.Millisecond,
			Activity:            behaviour.RandomizeActivity(),
			HeadingActivity:     behaviour.RandomizeHeadingActivity(),
			InitialHeading:      rand.Float64() * 360,
			MagFieldMagnitude:   devices.BehaviourMagField(m.seonDevice.LocalePreset(), m.seonDevice.Machine()),
			EmitAtEar:           false,
			MagneticDeclination: devices.BehaviourMagDeclination(m.seonDevice.LocalePreset()),
		}
	}
	fpCfg := m.seonDevice.NewFingerprint(devices.FingerprintOverrides{
		Behaviour: behaviourOpts,
	})
	sessionToken, err := seonsdk.Fingerprint(*fpCfg)
	if err != nil {
		m.log.PhoneError("Failed to generate SEON fingerprint: " + err.Error())
		cancelSMSActivation(smsClient, activationID, m)
		m.clearSMSState()
		return smsProvider, err
	}
	_, err = m.sendGqlRequest("SetPhoneNumberMutationV2", gqlBody[any]{
		OperationName: "SetPhoneNumberMutationV2",
		Variables: map[string]string{
			"phoneNumber":         smsPhoneNumber,
			"session":             sessionToken,
			"surface":             userUpdateSurface,
			"verificationChannel": "SMS",
		},
		Query:      qSetPhoneNumberMutationV2,
		Extensions: map[string]any{"clientLibrary": m.referralClientLibrary()},
	})

	if err != nil {
		m.log.PhoneError("Failed to set phone number " + smsPhoneNumber + ": " + err.Error())
		cancelSMSActivation(smsClient, activationID, m)
		m.clearSMSState()
		return smsProvider, err
	}

	if smsProvider == "herosms" {
		m.log.HeroSMS("Waiting for code...")
	} else {
		m.log.Phone("Waiting for verification code...")
	}

	code, err := smsClient.GetCode(context.Background(), activationID)
	if err != nil {
		m.log.PhoneError("Failed to fetch SMS code: " + err.Error())
		cancelSMSActivation(smsClient, activationID, m)
		m.clearSMSState()
		return smsProvider, err
	}

	m.log.Phone("Verification code received.")
	m.lastSMSCode = code
	m.autosaveSessionIfChanged("sms code received")
	m.usedSeonSessionTokens = append(m.usedSeonSessionTokens, sessionToken)
	if err := m.activatePhoneNumber(code); err != nil {
		return smsProvider, err
	}
	return smsProvider, nil
}

func chooseSMSProvider(providers []string, previousProvider string) string {
	normalized := make([]string, 0, len(providers))
	for _, provider := range providers {
		provider = normalizeSMSProviderName(provider)
		if provider == "" {
			continue
		}
		normalized = append(normalized, provider)
	}
	if len(normalized) == 0 {
		return "smspool"
	}
	previousProvider = normalizeSMSProviderName(previousProvider)
	if previousProvider != "" && len(normalized) > 1 {
		candidates := make([]string, 0, len(normalized)-1)
		for _, provider := range normalized {
			if provider != previousProvider {
				candidates = append(candidates, provider)
			}
		}
		if len(candidates) > 0 {
			return candidates[rand.Intn(len(candidates))]
		}
	}
	return normalized[rand.Intn(len(normalized))]
}

func chooseSMSCountryCode(cfg *appconfig.Config, smsProvider string) string {
	smsProvider = normalizeSMSProviderName(smsProvider)
	switch smsProvider {
	case "herosms":
		return randomRegionOrDefault(cfg.HeroSMSRegions)
	case "smsbower":
		return randomRegionOrDefault(cfg.SMSBowerRegions)
	case "smsonline":
		return randomRegionOrDefault(cfg.SMSOnlineRegions)
	case "smspool":
		return randomRegionOrDefault(cfg.SMSPoolRegions)
	default:
		return defaultSMSPoolRegion
	}
}

func randomRegionOrDefault(regions []string) string {
	if len(regions) == 0 {
		return defaultSMSPoolRegion
	}
	return regions[rand.Intn(len(regions))]
}

func normalizeSMSProviderName(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "sms_bower", "sms-bower", "sms bower":
		return "smsbower"
	case "sms_online", "sms-online", "sms online", "smsonlinepro", "sms-online-pro", "sms_online_pro":
		return "smsonline"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func isSMSCodeTimeout(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded")
}

func isSMSRetryable(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, terminal := range []string{
		"bad_key",
		"invalid api key",
		"api key is required",
		"no_balance",
		"insufficient balance",
		"bad_service",
		"wrong_service",
		"bad_country",
		"unauthorized sms",
	} {
		if strings.Contains(text, terminal) {
			return false
		}
	}
	if isSMSCodeTimeout(err) {
		return true
	}
	for _, transient := range []string{
		"unknown_error",
		"http 422",
		"timeout",
		"temporary",
		"too many requests",
		"429",
		"502",
		"503",
		"504",
		"connection reset",
		"connection refused",
		"eof",
		"no numbers",
		"no_numbers",
		"phone number",
		"setphonenumber",
	} {
		if strings.Contains(text, transient) {
			return true
		}
	}
	return strings.Contains(text, "sms") || strings.Contains(text, "phone")
}

func (m *Module) clearSMSState() {
	m.lastSMSPhoneNumber = ""
	m.lastSMSActivationID = ""
	m.lastSMSCode = ""
}

func cancelSMSActivation(smsClient *smsmanager.Client, activationID string, m *Module) {
	if smsClient == nil || strings.TrimSpace(activationID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := smsClient.CancelActivation(ctx, activationID); err != nil && m != nil && m.log != nil {
		m.log.PhoneError("Failed to cancel SMS activation: " + err.Error())
	}
}

func (m *Module) activatePhoneNumber(code string) error {
	m.log.Phone("Verifying phone number...")

	_, err := m.sendGqlRequest("VerifyPhoneNumberMutation", gqlBody[any]{
		OperationName: "VerifyPhoneNumberMutation",
		Variables: map[string]string{
			"code":    code,
			"surface": userUpdateSurface,
		},
		Query:      qVerifyPhoneNumberMutation,
		Extensions: map[string]any{"clientLibrary": m.referralClientLibrary()},
	})

	if err != nil {
		m.log.PhoneError("Failed to verify phone number: " + err.Error())
		return err
	}

	m.log.Phone("Phone number verified successfully")

	return nil
}

func (m *Module) storeReferralReward(applied *referralMoneyDto, balance *referralMoneyDto) {
	if applied != nil {
		m.referralAmountSafe = applied.AmountSafe
	}
	if balance != nil {
		m.rewardBalanceAmountSafe = balance.AmountSafe
		m.rewardBalanceCurrency = strings.TrimSpace(balance.Currency)
	}
	if m.rewardBalanceAmountSafe <= 0 && applied != nil {
		m.rewardBalanceAmountSafe = applied.AmountSafe
	}
	if m.rewardBalanceCurrency == "" && applied != nil {
		m.rewardBalanceCurrency = strings.TrimSpace(applied.Currency)
	}
	if m.rewardBalanceCurrency == "" {
		m.rewardBalanceCurrency = "GBP"
	}
	m.rewardCreditCheckError = ""
	m.autosaveSessionIfChanged("referral reward stored")
}

func (m *Module) logReferralRewardApplied(code string, amount *referralMoneyDto) {
	if amount == nil {
		m.log.ManagerNotice("Referral credit applied successfully (" + code + ").")
		return
	}
	currency := strings.TrimSpace(amount.Currency)
	if currency == "" {
		currency = "GBP"
	}
	m.log.ManagerNotice("Referral credit applied successfully (" + code + "): " + fmt.Sprintf("%.2f %s",
		float64(amount.AmountSafe)/100,
		currency,
	))
}

func (m *Module) notifyRewardDetected() error {
	if m.rewardDetectedHook == nil {
		return nil
	}
	snapshot, err := m.SessionSnapshot()
	if err != nil {
		return fmt.Errorf("snapshot reward session: %w", err)
	}
	if err := m.rewardDetectedHook(m.AccountSnapshot(), snapshot); err != nil {
		return err
	}
	return nil
}
