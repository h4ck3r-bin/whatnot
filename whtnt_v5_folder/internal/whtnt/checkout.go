package whtnt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/google/uuid"
)

const (
	checkoutCountryCode = "GB"
	checkoutCurrency    = "GBP"

	mobileCheckoutGraphQLURL        = "https://api.whatnot.com/graphql/?operationName=%s"
	mobileCheckoutFallbackMachine   = "iPhone10,6"
	mobileCheckoutFallbackSystemVer = "16.7.15"
	mobileCheckoutSystemName        = "iOS"
	mobileCheckoutTimezone          = "US/Pacific"
	mobileCheckoutAppLocation       = "APP_LOCATION_PROFILE_SHOP"
	mobileCheckoutClientName        = "com.whatnot-apollo-ios"
	mobileCheckoutLibraryName       = "apollo-ios"
	mobileCheckoutUSGMT             = ",F,"
	defaultStripeAPIVersion         = "2020-08-27"
	defaultStripeMobileAppID        = "com.whatnot"
	defaultStripePaymentUserAgent   = "stripe-ios/26.3.0; variant.paymentsheet; PaymentSheet"
	defaultStripeIntegrationVersion = "stripe-ios/26.3.0"
	defaultStripeBindingsVersion    = "26.3.0"
	placeOrderCardDisplay           = "nonce"
	placeOrderCardTokenPayload      = `{"nonce":"nonce","deviceData":"","type":"type","description":"desc"}`
)

var errOrderAlreadyExists = errors.New("matching order already exists on account")

var (
	errCardDeclined       = errors.New("card declined")
	errOutOfStock         = errors.New("listing out of stock")
	errSwitchBasket       = errors.New("switch basket")
	errShippingQuoteEmpty = errors.New("shipping quote returned no quotes")
	ErrNoBalance          = errors.New("NO_BALANCE")
)

type mobileCheckoutBody struct {
	Extensions    any    `json:"extensions,omitempty"`
	OperationName string `json:"operationName"`
	Query         string `json:"query"`
	Variables     any    `json:"variables,omitempty"`
}

type quoteCartResponse struct {
	Data struct {
		QuoteCart quoteCartPayload `json:"quoteCart"`
	} `json:"data"`
}

type quoteCartPayload struct {
	ID                            string         `json:"id"`
	Success                       bool           `json:"success"`
	Message                       string         `json:"message"`
	IsAddressValid                bool           `json:"isAddressValid"`
	AuthenticationFee             *checkoutMoney `json:"authenticationFee"`
	Credit                        *checkoutMoney `json:"credit"`
	ShippingPrice                 *checkoutMoney `json:"shippingPrice"`
	Subtotal                      *checkoutMoney `json:"subtotal"`
	Taxes                         *checkoutMoney `json:"taxes"`
	TotalDiscount                 *checkoutMoney `json:"totalDiscount"`
	Total                         *checkoutMoney `json:"total"`
	LocalizedTotal                *checkoutMoney `json:"localizedTotal"`
	AlternativePaymentMethodTypes any            `json:"alternativePaymentMethodTypes"`
}

type checkoutMoney struct {
	Typename string `json:"__typename"`
	Amount   int    `json:"amount"`
	Currency string `json:"currency"`
}

type stripeSetupData struct {
	IntentSecret   string
	EphemeralKey   string
	StripeCustomer string
	PublishableKey string
}

type mobileCheckoutDevice struct {
	Machine         string
	IOSVersion      string
	UserAgent       string
	StripeUserAgent string
}

func (m *Module) mobileDeviceForCheckout() mobileCheckoutDevice {
	machine := mobileCheckoutFallbackMachine
	iosVersion := mobileCheckoutFallbackSystemVer
	if m.seonDevice != nil {
		if value := strings.TrimSpace(m.seonDevice.Machine()); value != "" {
			machine = value
		}
		if value := strings.TrimSpace(m.seonDevice.IosVersion()); value != "" {
			iosVersion = value
		}
	}

	return mobileCheckoutDevice{
		Machine:         machine,
		IOSVersion:      iosVersion,
		UserAgent:       m.whatnotIOSUserAgent(iosVersion, machine),
		StripeUserAgent: m.stripeUserAgentJSON(iosVersion, machine),
	}
}

func (m *Module) mobileCheckoutLibraryVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.MobileCheckoutLibraryVer != "" {
		return m.config.AppVersions.MobileCheckoutLibraryVer
	}
	return defaultWhatnotIOSApolloClientVersion
}

func (m *Module) mobileCheckoutRequestTag() string {
	if m != nil && m.config != nil && m.config.AppVersions.MobileCheckoutRequestTag != "" {
		return m.config.AppVersions.MobileCheckoutRequestTag
	}
	return "gql_sdk_version=" + m.whatnotIOSApolloClientVersion()
}

func (m *Module) stripeAPIVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.StripeAPIVersion != "" {
		return m.config.AppVersions.StripeAPIVersion
	}
	return defaultStripeAPIVersion
}

func (m *Module) stripeMobileAppID() string {
	if m != nil && m.config != nil && m.config.AppVersions.StripeMobileAppID != "" {
		return m.config.AppVersions.StripeMobileAppID
	}
	return defaultStripeMobileAppID
}

func (m *Module) stripePaymentUserAgent() string {
	if m != nil && m.config != nil && m.config.AppVersions.StripePaymentUserAgent != "" {
		return m.config.AppVersions.StripePaymentUserAgent
	}
	return defaultStripePaymentUserAgent
}

func (m *Module) stripeIntegrationVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.StripeIntegrationVersion != "" {
		return m.config.AppVersions.StripeIntegrationVersion
	}
	return defaultStripeIntegrationVersion
}

func (m *Module) stripeBindingsVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.StripeBindingsVersion != "" {
		return m.config.AppVersions.StripeBindingsVersion
	}
	return defaultStripeBindingsVersion
}

type stripeMobileContext struct {
	ClientSessionID  string
	ElementsConfigID string
	StripeUserAgent  string
}

func (m *Module) handlePostOnboarding() error {
	prizeName, shouldCheckout := m.checkoutPrizeName()
	if !shouldCheckout {
		return nil
	}

	m.log.ManagerNotice("Continuing with shipping, payment, and basket checkout...")

	address := m.lastAddress
	if !hasCheckoutAddress(address) {
		var err error
		address, err = popAddressFromProfilesCSV(1)
		if err != nil {
			return fmt.Errorf("pop address from profiles.csv: %w", err)
		}
		m.lastAddress = address
		m.autosaveSessionIfChanged("claim shipping address")
	} else {
		m.log.Manager("Resuming checkout with stored shipping address.")
	}

	if err := m.ensureShippingAddressMatches(address); err != nil {
		return err
	}

	return m.checkoutPrizeBasket(prizeName, address)
}

func (m *Module) checkoutPrizeName() (string, bool) {
	rewardAmountSafe := m.referralAmountSafe
	if m.forceCheckout && m.rewardBalanceAmountSafe > 0 {
		rewardAmountSafe = m.rewardBalanceAmountSafe
	} else if rewardAmountSafe <= 0 {
		rewardAmountSafe = m.rewardBalanceAmountSafe
	}
	if m.forceCheckout {
		switch {
		case rewardAmountSafe >= 20000:
			m.log.ManagerNotice("Referral reward matched the 200 pound basket.")
			return prize200, true
		case rewardAmountSafe >= 2000:
			m.log.ManagerNotice("Referral reward matched the 20 pound basket.")
			return prize20, true
		case rewardAmountSafe >= 1000:
			m.log.ManagerNotice("10 pound checkout selected for manual checkout.")
			return prize10, true
		default:
			if m.allowInsufficientBalance {
				m.log.ManagerNotice(fmt.Sprintf("No reward balance detected (%d); continuing with the 10 pound checkout anyway.", rewardAmountSafe))
				return prize10, true
			}
			if rewardAmountSafe > 0 {
				m.log.Manager(fmt.Sprintf("No checkout basket mapped for available balance %d.", rewardAmountSafe))
			}
			return "", false
		}
	}

	switch {
	case rewardAmountSafe >= 20000:
		m.log.ManagerNotice("Referral reward matched the 200 pound basket.")
		return prize200, true
	case rewardAmountSafe >= 2000:
		m.log.ManagerNotice("Referral reward matched the 20 pound basket.")
		return prize20, true
	case rewardAmountSafe >= 1000:
		if m.forceCheckout {
			m.log.ManagerNotice("10 pound checkout selected for manual checkout.")
			return prize10, true
		}
		chance := m.config.CheckoutChance
		if chance <= 0 {
			m.log.Manager("Skipping 10 pound checkout because checkout chance is 0.")
			return "", false
		}

		roll := rand.Intn(100) + 1
		if roll > chance {
			m.log.Manager(fmt.Sprintf("Skipping 10 pound checkout after chance roll %d/%d.", roll, chance))
			return "", false
		}

		m.log.ManagerNotice(fmt.Sprintf("10 pound checkout selected after chance roll %d/%d.", roll, chance))
		return prize10, true
	default:
		if rewardAmountSafe > 0 {
			m.log.Manager(fmt.Sprintf("No checkout basket mapped for referral amount %d.", rewardAmountSafe))
		}
		return "", false
	}
}

func (m *Module) createShippingAddress(address AddressFields) error {
	m.log.Manager("Running mobile address flow...")

	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:getCouponIncentiveData",
		"getCouponIncentiveData",
		map[string]any{
			"incentiveSource":  "NEW_USER_BLANKET_INCENTIVE_CAMPAIGN",
			"skipValidation":   false,
			"filterOutExpired": true,
		},
		`query getCouponIncentiveData($incentiveSource: IncentiveSource!, $skipValidation: Boolean = false, $filterOutExpired: Boolean = false) @attribution(owner: "buyer-growth") { getCouponIncentiveData( incentiveSource: $incentiveSource skipValidation: $skipValidation filterOutExpired: $filterOutExpired ) { __typename coupon { __typename id code discountType discountValue { __typename amount currency } minPurchaseValue { __typename amount currency } status maxUsesPerUser userCouponUsageInfo { __typename status numRemainingUses } } expirationTime couponClaimed couponApplied } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:DefaultPayment",
		"DefaultPayment",
		nil,
		`query DefaultPayment @attribution(owner: "selling") { userDefaultPayment { __typename id gateway cardType cardMetadata cardDescription } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:ClaimedCoupons",
		"ClaimedCoupons",
		map[string]any{"livestreamId": nil},
		`query ClaimedCoupons($livestreamId: ID) @attribution(owner: "community") { claimedCoupons(livestreamId: $livestreamId) { __typename ...RewardsCouponFragment } }
fragment RewardsCouponFragment on CouponNode @attribution(owner: "community") { __typename id code description limitingCriteriaDescription customerEligibilityType }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:GetExperience",
		"GetExperience",
		map[string]any{
			"surface":         "IOS_ADDRESS_LIST",
			"surfaceMetadata": nil,
		},
		`query GetExperience($surface: SurfaceName!, $surfaceMetadata: SurfaceMetadataInput) @attribution(owner: "seller-growth") { getExperience(surface: $surface, surfaceMetadata: $surfaceMetadata) { __typename id name ... on GlobalBannerExperienceNode { data { __typename images { __typename light { id __typename smallImage: url(width: 120, height: 120, format: WEBP, fit: COVER) } dark { id __typename smallImage: url(width: 120, height: 120, format: WEBP, fit: COVER) } } title subTitle learnMoreUrl experienceSubtype } } ... on SellerLiveSharingUpsellModalExperienceNode { title primaryButtonText liveId liveTitle } ... on NewPaidUserPromotionMegaphoneExperienceNode { newPaidUserPromotionMegaphoneTitle: title newPaidUserPromotionMegaphoneDescription: description } ... on LoyaltyProgramLivestreamNewUserSheetExperienceNode { title description primaryButtonText } ... on RewardsClubProgramDataUpsellSheetExperienceNode { title description primaryButtonText } ... on LoyaltyProgramSellerUpsellSheetExperienceNode { nonOptionalTitle: title nonOptionalDescription: description nonOptionalButtonText: primaryButtonText } ... on FtbIncentiveCouponBannerExperienceNode { expirationTs coupon { __typename id discountValue { __typename amount } } } ... on ChitChatsIntroUpsellExperienceNode { title description linkText linkUrl primaryButtonText secondaryButtonText secondaryButtonUrl tertiaryButtonText } ... on GiveawayAccessBannerExperienceNode { markdownTitle markdownBody severity ctaText ctaLink isDismissable } ... on GiveawayAccessDialogueExperienceNode { title body giveawayRestrictionState ctaText ctaLink } ... on CouponIncentiveBannerExperienceNode { data { __typename title expiredAt coupon { __typename id } } } ... on UniversalBundlingUpsellExperienceNode { universalBundlingToastTitle: toastTitle universalBundlingToastDescription: toastDescription universalBundlingToastAutoDismissMilliseconds: toastAutoDismissMilliseconds universalBundlingModalTitle: modalTitle universalBundlingModalDescription: modalDescription universalBundlingModalToggleLabel: modalToggleLabel } ... on RewardsHubReferralUpsellExperienceNode { data { __typename referredUsernames referredProfileImageUrls totalCreditEarned { __typename amount currency } } } ... on GiveawayCreditsNuxBannerExperienceNode { heading description } ... on PointStoreTutorialExperienceNode { itemCode } ... on SellerOrderMilestoneExperienceNode { data { __typename milestoneIndex targetValue currentValue experienceType shareImageUrl challengeId } } ... on DelayedActivationExperienceNode { data { __typename halfSheetTitleCopy halfSheetSubtitleCopy halfSheetImageUrl } } } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:ShippingInfo",
		"ShippingInfo",
		nil,
		`query ShippingInfo @attribution(owner: "selling") { info: me { __typename id email username address: defaultShippingAddress { __typename id fullName line1 line2 city state zip: postalCode country: countryCode } } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:GetAddresses",
		"GetAddresses",
		nil,
		`query GetAddresses @attribution(owner: "selling") { me { __typename id fullName addresses { __typename id fullName line1 line2 city state postalCode countryCode defaultShipping returnAddress isPickupAddress } } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:GetPreferredPointRelais",
		"GetPreferredPointRelais",
		nil,
		`query GetPreferredPointRelais @attribution(owner: "selling") { me { __typename id buyerShippingSettings { __typename preferredMondialRelayPoint { __typename id num lgAdr1 lgAdr2 lgAdr3 lgAdr4 cp ville pays distance latitude longitude } } } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:GetMondialRelaySettings",
		"GetMondialRelaySettings",
		nil,
		`query GetMondialRelaySettings @attribution(owner: "international") { getMondialRelaySettings { __typename destinationCountries originCountries upsellCountries isEnabledForUser } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:address:MyUserData",
		"MyUserData",
		nil,
		`query MyUserData @attribution(owner: "app-platform") { me { __typename ...UserDataFragment } }
fragment AddressFragment on AddressNode @attribution(owner: "app-platform") { __typename id fullName line1 line2 city state postalCode countryCode }
fragment MoneyFragment on Money @attribution(owner: "app-platform") { __typename amount currency }
fragment PremierShopStatusFragment on PremierShopStatus @attribution(owner: "app-platform") { __typename isPremierShop }
fragment UserDataFragment on UserNode @attribution(owner: "app-platform") { __typename id firstName lastName username email phoneNumber phoneVerifiedAt createdAt daysSinceCreate canGoLive daysSinceCanGoLiveGranted canGoLiveGrantedAt hasMarketplaceAccess hasAccountHealthDashboard showRestrictedXP onboardingCountryCode needsToAcceptFirstLiveBuyerAgreement directMessagingDisabled soldCount sellerCurrency defaultShippingAddress { __typename ...AddressFragment } homeAddress { __typename ...AddressFragment } rewardBalance { __typename ...MoneyFragment } profileImage { __typename id bucket key url } premierShopStatus { __typename ...PremierShopStatusFragment } purchases { __typename totalCount } paidPromotion { __typename title description } referredByUser { __typename id enabledForRandomizedReferral } buyerTermsLegalEntity buyerTermsUpdate { __typename requiredAction legalEntity acceptancePrompt { __typename header message } terms { __typename name url version date } } userIPGeo { __typename countryCode } hasFreeFTBShippingCredit crashReportingSegments isEmployee canImpersonate resolvedCountryCode }`,
	)

	if _, err := m.runCheckpointedRequest("checkout:address:MainApp_ValidateAddress", func() error {
		return m.validateShippingAddress(address)
	}); err != nil {
		return err
	}
	if _, err := m.runCheckpointedRequest("checkout:address:MainApp_CreateAddress", func() error {
		return m.createShippingAddressNode(address)
	}); err != nil {
		return err
	}

	m.log.Checkout("Shipping address created.")
	m.shippingAddressCreated = true
	m.autosaveSessionIfChanged("create shipping address")
	m.sleepDelay("after_shipping_address")
	return nil
}

func (m *Module) ensureShippingAddressMatches(address AddressFields) error {
	if !hasCheckoutAddress(address) {
		return fmt.Errorf("checkout address is incomplete")
	}
	currentAddress, foundAddress, err := m.currentDefaultShippingAddress()
	if err != nil {
		return fmt.Errorf("check current shipping address: %w", err)
	}
	if !foundAddress {
		m.log.Manager("No default shipping address found on account; creating checkout input address.")
		m.lastAddress = address
		m.autosaveSessionIfChanged("claim shipping address")
		return m.createShippingAddress(address)
	}
	if checkoutAddressSnapshotEqual(currentAddress, address) {
		m.lastAddress = address
		m.shippingAddressCreated = true
		m.autosaveSessionIfChanged("confirmed shipping address from account")
		m.log.Manager("Account shipping address matches checkout input; continuing checkout.")
		return nil
	}

	m.log.Manager("Account shipping address differs from checkout input; updating shipping address.")
	m.lastAddress = address
	m.shippingAddressCreated = false
	m.clearCheckpointsWithPrefix("checkout:address:")
	m.autosaveSessionIfChanged("update shipping address from checkout input")
	return m.createShippingAddress(address)
}

func (m *Module) currentDefaultShippingAddress() (AddressSnapshot, bool, error) {
	addresses, err := m.ListAddressSnapshots()
	if err != nil {
		return AddressSnapshot{}, false, err
	}
	for _, address := range addresses {
		if address.DefaultShipping {
			return address, true, nil
		}
	}
	if len(addresses) > 0 {
		return addresses[0], true, nil
	}
	return AddressSnapshot{}, false, nil
}

func (m *Module) validateShippingAddress(address AddressFields) error {
	res, err := m.sendMobileCheckoutRequest(
		"MainApp_ValidateAddress",
		map[string]any{
			"fullName":     m.fullName(address),
			"addressLine1": strings.TrimSpace(address.AddressLine1),
			"addressLine2": strings.TrimSpace(address.AddressLine2),
			"city":         strings.TrimSpace(address.Town),
			"state":        strings.TrimSpace(address.Town),
			"postalCode":   strings.TrimSpace(address.Postcode),
			"countryCode":  checkoutCountryCode,
		},
		`mutation MainApp_ValidateAddress($fullName: String!, $addressLine1: String!, $addressLine2: String!, $city: String!, $state: String!, $postalCode: String!, $countryCode: String!) @attribution(owner: "app-platform") { validateAddress( fullName: $fullName addressLine1: $addressLine1 addressLine2: $addressLine2 addressCity: $city addressState: $state addressPostalCode: $postalCode addressCountryCode: $countryCode ) { __typename validation { __typename isValid fullName line1 line2 city state postalCode countryCode } } }`,
	)
	if err != nil {
		return fmt.Errorf("validate shipping address: %w", err)
	}

	type validateAddressResponse struct {
		Data struct {
			ValidateAddress struct {
				Validation struct {
					IsValid bool `json:"isValid"`
				} `json:"validation"`
			} `json:"validateAddress"`
		} `json:"data"`
	}

	var dto validateAddressResponse
	if err := res.JSON(&dto); err != nil {
		return fmt.Errorf("parse validate address response: %w", err)
	}
	if !dto.Data.ValidateAddress.Validation.IsValid {
		return fmt.Errorf("address validation failed: %s", shortResponse(res.String()))
	}

	return nil
}

func (m *Module) createShippingAddressNode(address AddressFields) error {
	res, err := m.sendMobileCheckoutRequest(
		"MainApp_CreateAddress",
		map[string]any{
			"fullName":        m.fullName(address),
			"addressLine1":    strings.TrimSpace(address.AddressLine1),
			"addressLine2":    strings.TrimSpace(address.AddressLine2),
			"city":            strings.TrimSpace(address.Town),
			"state":           "",
			"postalCode":      strings.TrimSpace(address.Postcode),
			"countryCode":     checkoutCountryCode,
			"homeAddress":     false,
			"billingAddress":  false,
			"defaultShipping": true,
		},
		`mutation MainApp_CreateAddress($fullName: String!, $addressLine1: String!, $addressLine2: String!, $city: String!, $state: String!, $postalCode: String!, $countryCode: String!, $homeAddress: Boolean!, $billingAddress: Boolean!, $defaultShipping: Boolean!) @attribution(owner: "app-platform") { createAddress( fullName: $fullName line1: $addressLine1 line2: $addressLine2 city: $city state: $state postalCode: $postalCode countryCode: $countryCode defaultShipping: $defaultShipping homeAddress: $homeAddress billingAddress: $billingAddress ) { __typename address { __typename id fullName line1 line2 city state postalCode countryCode } } }`,
	)
	if err != nil {
		return fmt.Errorf("create shipping address: %w", err)
	}
	if !strings.Contains(res.String(), `"address":{"__typename":"AddressNode","id":"`) {
		return fmt.Errorf("unexpected create address response: %s", shortResponse(res.String()))
	}

	return nil
}

func (m *Module) setupPaymentMethod() (string, error) {
	return m.setupPaymentMethodWithLookup(true)
}

func (m *Module) setupPaymentMethodWithoutLiveLookup() (string, error) {
	return m.setupPaymentMethodWithLookup(false)
}

func (m *Module) setupPaymentMethodWithLookup(checkExisting bool) (string, error) {
	var card CardDetails
	if m.overrideCard != nil {
		card = *m.overrideCard
	} else {
		var err error
		card, err = popCardFromCardsCSV(1)
		if err != nil {
			return "", fmt.Errorf("pop card from cards.csv: %w", err)
		}
	}
	card = m.cardWithShippingPostcode(card)
	m.lastCard = card
	if checkExisting {
		if paymentID, ok, err := m.findMatchingWhatnotPaymentMethod(card); err == nil && ok {
			m.log.Manager("Stored payment method matches checkout card last4; continuing checkout.")
			m.lastPaymentID = paymentID
			m.lastPaymentLast4 = cardLast4Utility(card.Number)
			m.autosaveSessionIfChanged("reuse matching payment method")
			return paymentID, nil
		} else if err != nil {
			m.log.ManagerNotice("Could not verify stored payment methods before card setup: " + err.Error())
		}
	}

	m.log.Manager("Running mobile card setup flow...")

	m.runCheckpointedCheckoutBestEffort(
		"checkout:payment:getCouponIncentiveData",
		"getCouponIncentiveData",
		map[string]any{
			"incentiveSource":  "NEW_USER_BLANKET_INCENTIVE_CAMPAIGN",
			"skipValidation":   false,
			"filterOutExpired": true,
		},
		`query getCouponIncentiveData($incentiveSource: IncentiveSource!, $skipValidation: Boolean = false, $filterOutExpired: Boolean = false) @attribution(owner: "buyer-growth") { getCouponIncentiveData( incentiveSource: $incentiveSource skipValidation: $skipValidation filterOutExpired: $filterOutExpired ) { __typename coupon { __typename id code discountType discountValue { __typename amount currency } minPurchaseValue { __typename amount currency } status maxUsesPerUser userCouponUsageInfo { __typename status numRemainingUses } } expirationTime couponClaimed couponApplied } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:payment:SupportedPaymentMethods",
		"SupportedPaymentMethods",
		nil,
		`query SupportedPaymentMethods @attribution(owner: "selling") { paymentMethods me { __typename id buyerTermsLegalEntity } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:payment:ClaimedCoupons",
		"ClaimedCoupons",
		map[string]any{"livestreamId": nil},
		`query ClaimedCoupons($livestreamId: ID) @attribution(owner: "community") { claimedCoupons(livestreamId: $livestreamId) { __typename ...RewardsCouponFragment } }
fragment RewardsCouponFragment on CouponNode @attribution(owner: "community") { __typename id code description limitingCriteriaDescription customerEligibilityType }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:payment:PaymentConfiguration",
		"PaymentConfiguration",
		nil,
		`query PaymentConfiguration @attribution(owner: "selling") { paymentConfiguration { __typename stripe { __typename appleMerchantId merchantCountryCode currencyCode } } }`,
	)

	setupData, err := m.fetchStripeSetup()
	if err != nil {
		return "", err
	}

	var paymentMethodID string
	for attempt := 1; attempt <= 4; attempt++ {
		stripeContext, err := m.buildStripeMobileContext(setupData)
		if err != nil {
			if attempt == 4 {
				return "", err
			}
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		m.listStripeSavedPaymentMethods(setupData.StripeCustomer, setupData.EphemeralKey, stripeContext.StripeUserAgent)

		if err := m.confirmStripeSetupIntent(card, setupData.PublishableKey, setupData.IntentSecret, stripeContext); err != nil {
			if errors.Is(err, errCardDeclined) {
				m.log.ManagerError("Card declined during Stripe setup. Loading the next card...")
				if m.overrideCard != nil {
					return "", err
				}
				card, err = popCardFromCardsCSV(1)
				if err != nil {
					return "", fmt.Errorf("load replacement card: %w", err)
				}
				card = m.cardWithShippingPostcode(card)
				m.lastCard = card
				continue
			}

			if attempt == 4 {
				return "", fmt.Errorf("confirm stripe setup intent: %w", err)
			}

			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		paymentMethodID, err = m.fetchStripeSetupIntentPaymentMethod(setupData.PublishableKey, setupData.IntentSecret, stripeContext.StripeUserAgent)
		if err == nil {
			break
		}
		if attempt == 4 {
			return "", err
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}

	if paymentMethodID == "" {
		return "", fmt.Errorf("stripe setup intent did not return a payment method")
	}

	m.log.Manager("Registering Whatnot payment method...")
	for attempt := 1; attempt <= 4; attempt++ {
		whatnotPaymentID, err := m.createPaymentMethod(paymentMethodID)
		if err == nil {
			m.runCheckpointedCheckoutBestEffort(
				"checkout:payment:DefaultPayment:after_create",
				"DefaultPayment",
				nil,
				`query DefaultPayment @attribution(owner: "selling") { userDefaultPayment { __typename id gateway cardType cardMetadata cardDescription } }`,
			)
			m.log.Checkout("Payment method created.")
			m.lastPaymentID = whatnotPaymentID
			m.lastPaymentLast4 = cardLast4Utility(card.Number)
			m.autosaveSessionIfChanged("create payment method")
			return whatnotPaymentID, nil
		} else if attempt == 4 {
			return "", err
		} else {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
	}

	return "", fmt.Errorf("payment method creation finished without a result")
}

func (m *Module) cardWithShippingPostcode(card CardDetails) CardDetails {
	if postcode := strings.TrimSpace(m.lastAddress.Postcode); postcode != "" {
		card.Postcode = postcode
	}
	return card
}

func (m *Module) findMatchingWhatnotPaymentMethod(card CardDetails) (string, bool, error) {
	targetLast4 := cardLast4Utility(card.Number)
	if targetLast4 == "" {
		return "", false, nil
	}
	m.log.Manager("Checking account payment methods for card ending " + targetLast4 + ".")
	methods, err := m.ListPaymentMethodSnapshots()
	if err != nil {
		return "", false, err
	}
	for _, method := range methods {
		if !paymentMethodLast4Matches(method, targetLast4) {
			continue
		}
		if strings.TrimSpace(method.ID) == "" {
			continue
		}
		return strings.TrimSpace(method.ID), true, nil
	}
	return "", false, nil
}

func paymentMethodLast4Matches(method PaymentMethodSnapshot, targetLast4 string) bool {
	targetLast4 = strings.TrimSpace(targetLast4)
	if targetLast4 == "" {
		return false
	}
	fields := []string{method.CardDescription, method.CardMetadata, method.CardType}
	for _, field := range fields {
		if paymentMethodFieldLast4(field) == targetLast4 {
			return true
		}
	}
	return false
}

func paymentMethodFieldLast4(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		for _, key := range []string{"last4", "last_4", "lastFour", "last_four"} {
			if last4 := cardLast4Utility(fmt.Sprint(decoded[key])); last4 != "" {
				return last4
			}
		}
		if card, ok := decoded["card"].(map[string]any); ok {
			for _, key := range []string{"last4", "last_4", "lastFour", "last_four"} {
				if last4 := cardLast4Utility(fmt.Sprint(card[key])); last4 != "" {
					return last4
				}
			}
		}
		return ""
	}
	return cardLast4Utility(value)
}

func (m *Module) fetchStripeSetup() (stripeSetupData, error) {
	type stripeSetupResponse struct {
		Data struct {
			StripeSetupToken struct {
				IntentSecret   string `json:"intentSecret"`
				EphemeralKey   string `json:"ephemeralKey"`
				StripeCustomer string `json:"stripeCustomer"`
				PublishableKey string `json:"publishableKey"`
			} `json:"stripeSetupToken"`
		} `json:"data"`
	}

	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		res, err := m.sendMobileCheckoutRequest(
			"StripeSetup",
			map[string]any{"paymentMethodType": "CARD"},
			`query StripeSetup($paymentMethodType: PaymentMethodType) @attribution(owner: "selling") { stripeSetupToken(paymentMethodType: $paymentMethodType) { __typename intentSecret ephemeralKey stripeCustomer publishableKey } }`,
		)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		var dto stripeSetupResponse
		if err := res.JSON(&dto); err != nil {
			lastErr = fmt.Errorf("parse StripeSetup response: %w", err)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}

		data := stripeSetupData{
			IntentSecret:   strings.TrimSpace(dto.Data.StripeSetupToken.IntentSecret),
			EphemeralKey:   strings.TrimSpace(dto.Data.StripeSetupToken.EphemeralKey),
			StripeCustomer: strings.TrimSpace(dto.Data.StripeSetupToken.StripeCustomer),
			PublishableKey: strings.TrimSpace(dto.Data.StripeSetupToken.PublishableKey),
		}
		if data.IntentSecret != "" && data.EphemeralKey != "" && data.StripeCustomer != "" && data.PublishableKey != "" {
			return data, nil
		}

		lastErr = fmt.Errorf("stripe setup response was missing required fields")
		time.Sleep(time.Duration(attempt) * time.Second)
	}

	return stripeSetupData{}, fmt.Errorf("fetch stripe setup: %w", lastErr)
}

func (m *Module) buildStripeMobileContext(setup stripeSetupData) (stripeMobileContext, error) {
	stripeUserAgent := m.mobileDeviceForCheckout().StripeUserAgent
	elementsConfigID, clientSessionID, err := m.fetchStripeElementsSessionConfig(setup, stripeUserAgent)
	if err != nil {
		return stripeMobileContext{}, err
	}

	return stripeMobileContext{
		ClientSessionID:  clientSessionID,
		ElementsConfigID: elementsConfigID,
		StripeUserAgent:  stripeUserAgent,
	}, nil
}

func (m *Module) fetchStripeElementsSessionConfig(setup stripeSetupData, stripeUserAgent string) (string, string, error) {
	clientSessionID := uuid.NewString()
	query := url.Values{
		"client_secret":                 {setup.IntentSecret},
		"expand[0]":                     {"payment_method_preference.setup_intent.payment_method"},
		"legacy_customer_ephemeral_key": {setup.EphemeralKey},
		"locale":                        {"en-GB"},
		"mobile_app_id":                 {m.stripeMobileAppID()},
		"mobile_session_id":             {clientSessionID},
		"type":                          {"setup_intent"},
	}

	stripeReq, reqErr := newGetRequest("https://api.stripe.com/v1/elements/sessions?"+query.Encode(), http.Header{
		"accept":              {"*/*"},
		"authorization":       {"Bearer " + setup.PublishableKey},
		"accept-encoding":     {"gzip, deflate, br"},
		"stripe-version":      {m.stripeAPIVersion()},
		"user-agent":          {m.whatnotIOSStripeUserAgent()},
		"accept-language":     {"en-GB,en;q=0.9 "},
		"x-stripe-user-agent": {stripeUserAgent},
		http.PHeaderOrderKey:  {":method", ":scheme", ":path", ":authority"},
		http.HeaderOrderKey:   {"accept", "authorization", "accept-encoding", "stripe-version", "user-agent", "accept-language", "x-stripe-user-agent"},
	})
	if reqErr != nil {
		return "", "", fmt.Errorf("fetch stripe elements session: %w", reqErr)
	}
	res, err := m.doRequestWithRetry("fetch stripe elements session", stripeReq)
	if err != nil {
		return "", "", fmt.Errorf("fetch stripe elements session: %w", err)
	}

	type stripeElementsSessionResponse struct {
		ConfigID string `json:"config_id"`
	}

	var dto stripeElementsSessionResponse
	if err := res.JSON(&dto); err != nil {
		return "", "", fmt.Errorf("parse stripe elements session response: %w", err)
	}
	if strings.TrimSpace(dto.ConfigID) == "" {
		return "", "", fmt.Errorf("stripe elements session response was missing config_id")
	}

	return dto.ConfigID, clientSessionID, nil
}

func (m *Module) listStripeSavedPaymentMethods(customer, ephemeralKey, stripeUserAgent string) {
	if strings.TrimSpace(customer) == "" || strings.TrimSpace(ephemeralKey) == "" {
		return
	}

	for _, methodType := range []string{"card", "sepa_debit", "us_bank_account"} {
		query := url.Values{
			"customer": {customer},
			"limit":    {"100"},
			"type":     {methodType},
		}

		listReq, listReqErr := newGetRequest("https://api.stripe.com/v1/payment_methods?"+query.Encode(), http.Header{
			"accept":              {"*/*"},
			"authorization":       {"Bearer " + ephemeralKey},
			"accept-encoding":     {"gzip, deflate, br"},
			"stripe-version":      {m.stripeAPIVersion()},
			"user-agent":          {m.whatnotIOSStripeUserAgent()},
			"accept-language":     {"en-GB,en;q=0.9 "},
			"x-stripe-user-agent": {stripeUserAgent},
			http.PHeaderOrderKey:  {":method", ":scheme", ":path", ":authority"},
			http.HeaderOrderKey:   {"accept", "authorization", "accept-encoding", "stripe-version", "user-agent", "accept-language", "x-stripe-user-agent"},
		})
		if listReqErr != nil {
			m.log.Scope(fmt.Sprintf("Mobile filler %s skipped: %v", methodType, listReqErr))
			continue
		}
		_, err := m.doRequestWithRetry("list stripe saved payment methods", listReq)
		if err != nil {
			m.log.Scope(fmt.Sprintf("Mobile filler %s skipped: %v", methodType, err))
		}
	}
}

func (m *Module) confirmStripeSetupIntent(card CardDetails, publishableKey, intentSecret string, stripeContext stripeMobileContext) error {
	setupIntentID := strings.Split(intentSecret, "_secret_")[0]
	var bodyParts []string
	addField := func(key, value string) {
		bodyParts = append(bodyParts, fmt.Sprintf("%s=%s", key, url.QueryEscape(value)))
	}

	addField("client_attribution_metadata[client_session_id]", stripeContext.ClientSessionID)
	addField("client_attribution_metadata[elements_session_config_id]", stripeContext.ElementsConfigID)
	addField("client_attribution_metadata[merchant_integration_source]", "elements")
	addField("client_attribution_metadata[merchant_integration_subtype]", "mobile")
	addField("client_attribution_metadata[merchant_integration_version]", m.stripeIntegrationVersion())
	addField("client_attribution_metadata[payment_intent_creation_flow]", "standard")
	addField("client_attribution_metadata[payment_method_selection_flow]", "merchant_specified")
	addField("client_secret", intentSecret)
	addField("expand[0]", "payment_method")
	addField("payment_method_data[allow_redisplay]", "unspecified")
	addField("payment_method_data[billing_details][address][country]", checkoutCountryCode)
	addField("payment_method_data[billing_details][address][postal_code]", normalizeStripePostcode(card.Postcode))
	addField("payment_method_data[card][cvc]", strings.TrimSpace(card.CVC))
	addField("payment_method_data[card][exp_month]", strings.TrimSpace(card.ExpMonth))
	addField("payment_method_data[card][exp_year]", normalizeExpYear(card.ExpYear))
	addField("payment_method_data[card][number]", normalizeCardNumber(card.Number))
	addField("payment_method_data[client_attribution_metadata][client_session_id]", stripeContext.ClientSessionID)
	addField("payment_method_data[client_attribution_metadata][elements_session_config_id]", stripeContext.ElementsConfigID)
	addField("payment_method_data[client_attribution_metadata][merchant_integration_source]", "elements")
	addField("payment_method_data[client_attribution_metadata][merchant_integration_subtype]", "mobile")
	addField("payment_method_data[client_attribution_metadata][merchant_integration_version]", m.stripeIntegrationVersion())
	addField("payment_method_data[client_attribution_metadata][payment_intent_creation_flow]", "standard")
	addField("payment_method_data[client_attribution_metadata][payment_method_selection_flow]", "merchant_specified")
	addField("payment_method_data[payment_user_agent]", m.stripePaymentUserAgent())
	addField("payment_method_data[type]", "card")
	addField("return_url", "whatnot://stripe-redirect")
	addField("use_stripe_sdk", "true")

	encodedData := strings.Join(bodyParts, "&")

	confirmReq, confirmReqErr := newPostRequest(
		fmt.Sprintf("https://api.stripe.com/v1/setup_intents/%s/confirm", setupIntentID),
		strings.NewReader(encodedData),
		http.Header{
			"accept":              {"*/*"},
			"content-type":        {"application/x-www-form-urlencoded"},
			"x-stripe-user-agent": {stripeContext.StripeUserAgent},
			"stripe-version":      {m.stripeAPIVersion()},
			"authorization":       {"Bearer " + publishableKey},
			"content-length":      {fmt.Sprintf("%d", len(encodedData))},
			"user-agent":          {m.whatnotIOSStripeUserAgent()},
			"accept-language":     {"en-GB,en;q=0.9 "},
			"accept-encoding":     {"gzip, deflate, br"},
			http.PHeaderOrderKey:  {":method", ":scheme", ":path", ":authority"},
			http.HeaderOrderKey:   {"accept", "content-type", "x-stripe-user-agent", "stripe-version", "authorization", "content-length", "user-agent", "accept-language", "accept-encoding"},
		},
	)
	if confirmReqErr != nil {
		return fmt.Errorf("confirm stripe setup intent: %w", confirmReqErr)
	}
	res, err := m.doRequestWithRetry("confirm stripe setup intent", confirmReq)
	if err != nil {
		return fmt.Errorf("confirm stripe setup intent: %w", err)
	}

	body := res.String()
	if strings.Contains(body, `repeated attempts too frequently`) || strings.Contains(body, `"declined"`) {
		return fmt.Errorf("%w: %s", errCardDeclined, shortResponse(body))
	}
	if !strings.Contains(body, `"status": "succeeded"`) && !strings.Contains(body, `"status":"succeeded"`) {
		return fmt.Errorf("unexpected Stripe confirm response: %s", shortResponse(body))
	}

	return nil
}

func (m *Module) fetchStripeSetupIntentPaymentMethod(publishableKey, intentSecret, stripeUserAgent string) (string, error) {
	setupIntentID := strings.Split(intentSecret, "_secret_")[0]
	query := url.Values{
		"client_secret": {intentSecret},
	}

	fetchReq, fetchReqErr := newGetRequest(
		fmt.Sprintf("https://api.stripe.com/v1/setup_intents/%s?%s", setupIntentID, query.Encode()),
		http.Header{
			"accept":              {"*/*"},
			"authorization":       {"Bearer " + publishableKey},
			"accept-encoding":     {"gzip, deflate, br"},
			"stripe-version":      {m.stripeAPIVersion()},
			"user-agent":          {m.whatnotIOSStripeUserAgent()},
			"accept-language":     {"en-GB,en;q=0.9 "},
			"x-stripe-user-agent": {stripeUserAgent},
			http.PHeaderOrderKey:  {":method", ":scheme", ":path", ":authority"},
			http.HeaderOrderKey:   {"accept", "authorization", "accept-encoding", "stripe-version", "user-agent", "accept-language", "x-stripe-user-agent"},
		},
	)
	if fetchReqErr != nil {
		return "", fmt.Errorf("fetch stripe setup intent payment method: %w", fetchReqErr)
	}
	res, err := m.doRequestWithRetry("fetch stripe setup intent payment method", fetchReq)
	if err != nil {
		return "", fmt.Errorf("fetch stripe setup intent payment method: %w", err)
	}

	type stripeSetupIntentResponse struct {
		Status        string `json:"status"`
		PaymentMethod string `json:"payment_method"`
	}

	var dto stripeSetupIntentResponse
	if err := res.JSON(&dto); err != nil {
		return "", fmt.Errorf("parse stripe setup intent response: %w", err)
	}
	if strings.TrimSpace(dto.Status) != "succeeded" {
		return "", fmt.Errorf("stripe setup intent not succeeded: %s", shortResponse(res.String()))
	}
	if strings.TrimSpace(dto.PaymentMethod) == "" {
		return "", fmt.Errorf("stripe setup intent response was missing payment_method")
	}

	return dto.PaymentMethod, nil
}

func (m *Module) createPaymentMethod(paymentMethodID string) (string, error) {
	paymentData, err := json.Marshal(map[string]string{
		"gateway": "stripe",
		"id":      paymentMethodID,
		"type":    "card",
	})
	if err != nil {
		return "", fmt.Errorf("marshal paymentData: %w", err)
	}

	res, err := m.sendMobileCheckoutRequest(
		"CreatePayment",
		map[string]any{"paymentData": string(paymentData)},
		`mutation CreatePayment($paymentData: JSONString!) @attribution(owner: "selling") { createPaymentMethod(paymentData: $paymentData) { __typename paymentMethod { __typename id cardDescription cardType gateway } } }`,
	)
	if err != nil {
		return "", fmt.Errorf("create payment request: %w", err)
	}
	type createPaymentResponse struct {
		Data struct {
			CreatePaymentMethod struct {
				PaymentMethod struct {
					ID string `json:"id"`
				} `json:"paymentMethod"`
			} `json:"createPaymentMethod"`
		} `json:"data"`
	}
	var dto createPaymentResponse
	if err := res.JSON(&dto); err == nil && strings.TrimSpace(dto.Data.CreatePaymentMethod.PaymentMethod.ID) != "" {
		return strings.TrimSpace(dto.Data.CreatePaymentMethod.PaymentMethod.ID), nil
	}

	return "", fmt.Errorf("unexpected create payment response: %s", shortResponse(res.String()))
}

func (m *Module) checkoutPrizeBasket(prizeName string, address AddressFields) error {
	baskets, debugMessage, err := GetShuffledEligibleBasketsForPrizeDebug(prizeName)
	if err != nil {
		return fmt.Errorf("select baskets for %s: %w", prizeName, err)
	}
	m.log.Scope("Basket pool: " + debugMessage)

	failures := make([]string, 0, len(baskets))
	paymentID := strings.TrimSpace(m.lastPaymentID)
	paymentReady := paymentID != ""
	if paymentReady {
		m.log.Manager("Resuming checkout with stored payment method.")
	}

	balanceLimit, hasBalanceLimit := m.checkoutBalanceLimit()
	if !hasBalanceLimit {
		return fmt.Errorf("%w: no proven available reward balance", ErrNoBalance)
	}
	balanceSkipped := 0
	attemptedCheckout := 0
	for index, basket := range baskets {
		basketName := basketDisplayName(basket)
		basketTotal := checkoutBasketTotal(basket)
		if balanceLimit > 0 && basketTotal > balanceLimit {
			message := fmt.Sprintf("%s total %s exceeds available reward balance %s", basketName, checkoutMoneyLabel(basketTotal), checkoutMoneyLabel(balanceLimit))
			m.log.Manager("Skipping basket: " + message + ".")
			failures = append(failures, message)
			balanceSkipped++
			continue
		}
		m.log.Scope(fmt.Sprintf("Basket selected (%d/%d): %s", index+1, len(baskets), basketName))
		attemptedCheckout++

		if err := m.runBasketCheckoutPrelude(basket); err != nil {
			if errors.Is(err, errOutOfStock) || errors.Is(err, errSwitchBasket) {
				m.log.Manager("Switching to a new basket after a stock failure.")
				m.recordProductIssue(basket, err)
			} else {
				m.log.ManagerError(fmt.Sprintf("Checkout prep failed for %s: %s. Trying another product.", basketName, err.Error()))
			}
			failures = append(failures, fmt.Sprintf("%s prep: %v", basketName, err))
			continue
		}

		if !paymentReady {
			var err error
			paymentID, err = m.setupPaymentMethod()
			if err != nil {
				return err
			}
			m.lastPaymentID = paymentID
			paymentReady = true
		}

		if err := m.runCheckoutPreOrderHygiene(); err != nil {
			return err
		}
		err = m.checkoutBasket(address, basket, paymentID)
		if err == nil {
			m.log.Whatnot("Basket checkout completed.")
			return nil
		}
		if errors.Is(err, errOutOfStock) || errors.Is(err, errSwitchBasket) {
			m.log.Manager("Switching to a new basket after a stock failure.")
			m.recordProductIssue(basket, err)
		} else {
			m.log.ManagerError(fmt.Sprintf("Checkout failed for %s: %s. Trying another product.", basketName, err.Error()))
		}
		failures = append(failures, fmt.Sprintf("%s checkout: %v", basketName, err))
	}

	if attemptedCheckout == 0 && balanceSkipped > 0 {
		return fmt.Errorf("%w: all %d basket(s) for %s exceed available reward balance", ErrNoBalance, balanceSkipped, prizeName)
	}
	return fmt.Errorf("all %d eligible baskets failed for %s: %s", len(baskets), prizeName, strings.Join(failures, " | "))
}

func (m *Module) recordProductIssue(basket []Product, err error) {
	reason := "unavailable"
	if err != nil {
		reason = err.Error()
	}
	for _, product := range basket {
		m.productIssues = append(m.productIssues, ProductIssue{
			ProductName:      product.ProductName,
			ProductListingID: product.ProductListingID,
			Reason:           reason,
		})
	}
}

func (m *Module) checkoutBalanceLimit() (int, bool) {
	if m.allowInsufficientBalance {
		return 0, true
	}
	if m.rewardBalanceAmountSafe > 0 {
		return m.rewardBalanceAmountSafe, true
	}
	return 0, false
}

func checkoutBasketTotal(basket []Product) int {
	total := 0
	for _, product := range basket {
		total += product.ProductPrice
	}
	return total
}

func checkoutMoneyLabel(amount int) string {
	return fmt.Sprintf("%s %.2f", checkoutCurrency, float64(amount)/100)
}

func basketDisplayName(basket []Product) string {
	names := make([]string, 0, len(basket))
	for _, product := range basket {
		if strings.TrimSpace(product.ProductName) != "" {
			names = append(names, strings.TrimSpace(product.ProductName))
		}
	}
	if len(names) == 0 {
		return BasketSignature(basket)
	}
	return strings.Join(names, " + ")
}

func (m *Module) runBasketCheckoutPrelude(basket []Product) error {
	m.runCheckpointedCheckoutBestEffort(
		"checkout:prelude:MyUserData:"+BasketSignature(basket),
		"MyUserData",
		nil,
		`query MyUserData @attribution(owner: "app-platform") { me { __typename ...UserDataFragment } }
fragment AddressFragment on AddressNode @attribution(owner: "app-platform") { __typename id fullName line1 line2 city state postalCode countryCode }
fragment MoneyFragment on Money @attribution(owner: "app-platform") { __typename amount currency }
fragment PremierShopStatusFragment on PremierShopStatus @attribution(owner: "app-platform") { __typename isPremierShop }
fragment UserDataFragment on UserNode @attribution(owner: "app-platform") { __typename id firstName lastName username email phoneNumber phoneVerifiedAt createdAt daysSinceCreate canGoLive daysSinceCanGoLiveGranted canGoLiveGrantedAt hasMarketplaceAccess hasAccountHealthDashboard showRestrictedXP onboardingCountryCode needsToAcceptFirstLiveBuyerAgreement directMessagingDisabled soldCount sellerCurrency defaultShippingAddress { __typename ...AddressFragment } homeAddress { __typename ...AddressFragment } rewardBalance { __typename ...MoneyFragment } profileImage { __typename id bucket key url } premierShopStatus { __typename ...PremierShopStatusFragment } purchases { __typename totalCount } paidPromotion { __typename title description } referredByUser { __typename id enabledForRandomizedReferral } buyerTermsLegalEntity buyerTermsUpdate { __typename requiredAction legalEntity acceptancePrompt { __typename header message } terms { __typename name url version date } } userIPGeo { __typename countryCode } hasFreeFTBShippingCredit crashReportingSegments isEmployee canImpersonate resolvedCountryCode }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:prelude:GetAddresses:"+BasketSignature(basket),
		"GetAddresses",
		nil,
		`query GetAddresses @attribution(owner: "selling") { me { __typename id fullName addresses { __typename id fullName line1 line2 city state postalCode countryCode defaultShipping returnAddress isPickupAddress } } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:prelude:ShippingInfo:"+BasketSignature(basket),
		"ShippingInfo",
		nil,
		`query ShippingInfo @attribution(owner: "selling") { info: me { __typename id email username address: defaultShippingAddress { __typename id fullName line1 line2 city state zip: postalCode country: countryCode } } }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		"checkout:prelude:GetPreferredPointRelais:"+BasketSignature(basket),
		"GetPreferredPointRelais",
		nil,
		`query GetPreferredPointRelais @attribution(owner: "selling") { me { __typename id buyerShippingSettings { __typename preferredMondialRelayPoint { __typename id num lgAdr1 lgAdr2 lgAdr3 lgAdr4 cp ville pays distance latitude longitude } } } }`,
	)

	for _, product := range basket {
		if _, err := m.runCheckpointedRequest(checkoutProductCheckpoint("checkout:prelude:ShippingQuoteiOS", product), func() error {
			_, err := m.fetchShippingQuote(product)
			return err
		}); err != nil {
			if errors.Is(err, errShippingQuoteEmpty) {
				m.log.Manager(fmt.Sprintf("Shipping quote returned no quotes for %s; continuing checkout anyway.", product.ProductName))
				m.markCheckpointDone(checkoutProductCheckpoint("checkout:prelude:ShippingQuoteiOS", product))
				continue
			}
			if errors.Is(err, errOutOfStock) {
				_, _ = SetPriceAndInStockByListingID(product.ProductListingID, product.ProductPrice, false)
				return errSwitchBasket
			}
			return err
		}
	}

	m.runCheckpointedCheckoutBestEffort(
		"checkout:prelude:getConversionRates:"+BasketSignature(basket),
		"getConversionRates",
		nil,
		`query getConversionRates @attribution(owner: "app-platform") { getConversionRates { __typename buyerCurrency conversionRates { __typename rate currency } } }`,
	)

	return nil
}

func (m *Module) fetchShippingQuote(product Product) (string, error) {
	res, err := m.sendMobileCheckoutRequest(
		"ShippingQuoteiOS",
		map[string]any{
			"listingId":    product.ProductListingID,
			"quantity":     1,
			"salesChannel": map[string]any{"type": "MARKETPLACE"},
			"orderStyle":   "DIRECT_ORDER",
		},
		`query ShippingQuoteiOS($listingId: ID!, $quantity: Int!, $salesChannel: SalesChannelInfoInput!, $orderStyle: OrderStyle!) @attribution(owner: "logistics") { getShippingQuotes( listingId: $listingId salesChannel: $salesChannel orderStyle: $orderStyle quantity: $quantity ) { __typename ...ShippingQuoteResponse } }
fragment MoneyFragment on Money @attribution(owner: "app-platform") { __typename amount currency }
fragment ShippingQuoteResponse on ShippingQuotesResult @attribution(owner: "logistics") { __typename quotes { __typename shippingEstimate { __typename disclosures taxDisclosures tariffDisclosures } itemPriceLowerBound { __typename ...MoneyFragment } itemPriceUpperBound { __typename ...MoneyFragment } shippingQuoteAmount { __typename ...MoneyFragment } deliveryMethod tokenizedQuoteDetails } unshippableReason error { __typename ... on BaseError { title detail } } }`,
	)
	if err != nil {
		return "", fmt.Errorf("shipping quote for %s: %w", product.ProductName, err)
	}

	type shippingQuoteResponse struct {
		Data struct {
			GetShippingQuotes struct {
				Quotes []struct {
					TokenizedQuoteDetails string `json:"tokenizedQuoteDetails"`
				} `json:"quotes"`
				UnshippableReason string `json:"unshippableReason"`
				Error             *struct {
					Title  string `json:"title"`
					Detail string `json:"detail"`
				} `json:"error"`
			} `json:"getShippingQuotes"`
		} `json:"data"`
	}

	var dto shippingQuoteResponse
	if err := res.JSON(&dto); err != nil {
		return "", fmt.Errorf("parse shipping quote response: %w", err)
	}
	if dto.Data.GetShippingQuotes.Error != nil {
		message := strings.TrimSpace(dto.Data.GetShippingQuotes.Error.Detail)
		if message == "" {
			message = strings.TrimSpace(dto.Data.GetShippingQuotes.Error.Title)
		}
		if strings.Contains(strings.ToLower(message), "not enough quantity") {
			return "", errOutOfStock
		}
		return "", fmt.Errorf("shipping quote error: %s", message)
	}
	if strings.TrimSpace(dto.Data.GetShippingQuotes.UnshippableReason) != "" {
		return "", fmt.Errorf("listing unshippable: %s", dto.Data.GetShippingQuotes.UnshippableReason)
	}
	if len(dto.Data.GetShippingQuotes.Quotes) == 0 {
		return "", errShippingQuoteEmpty
	}

	return strings.TrimSpace(dto.Data.GetShippingQuotes.Quotes[0].TokenizedQuoteDetails), nil
}

func (m *Module) checkoutBasket(address AddressFields, basket []Product, paymentID string) error {
	m.checkoutAttempted = true
	for index, product := range basket {
		if !product.InStock {
			return errSwitchBasket
		}

		if !m.forceCheckout {
			satisfied, err := m.existingBasketOrderSatisfied(product, index, basket)
			if err != nil {
				return fmt.Errorf("pre-check existing orders for %s: %w", product.ProductName, err)
			}
			if satisfied {
				m.log.Manager(fmt.Sprintf("Existing order already satisfies basket item %d/%d: %s; continuing to next item.", index+1, len(basket), product.ProductName))
				m.markCheckpointDone(checkoutProductCheckpoint("checkout:order:PlaceOrder", product))
				continue
			}
		}

		m.log.Scope(fmt.Sprintf("Checking out %s for %s %.2f...", product.ProductName, checkoutCurrency, float64(product.ProductPrice)/100))
		if err := m.placeOrderWithRetries(address, product, paymentID, index, basket); err != nil {
			if errors.Is(err, errOutOfStock) {
				_, _ = SetPriceAndInStockByListingID(product.ProductListingID, product.ProductPrice, false)
				return errSwitchBasket
			}
			return fmt.Errorf("checkout %s: %w", product.ProductName, err)
		}
		m.sleepDelay("after_order")
		if index < len(basket)-1 {
			m.sleepDelay("between_basket_orders")
		}
	}

	return nil
}

func (m *Module) placeOrderWithRetries(address AddressFields, product Product, paymentID string, basketIndex int, basket []Product) error {
	if !m.forceCheckout && m.productOrderAlreadyRecorded(product, basketIndex, basket) {
		m.markCheckpointDone(checkoutProductCheckpoint("checkout:order:PlaceOrder", product))
		return nil
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		lastErr = m.placeOrder(address, product, paymentID, basketIndex, basket)
		if lastErr == nil {
			return nil
		}
		if !m.forceCheckout && errors.Is(lastErr, errOrderAlreadyExists) {
			m.markCheckpointDone(checkoutProductCheckpoint("checkout:order:PlaceOrder", product))
			return nil
		}
		if errors.Is(lastErr, errOutOfStock) {
			return lastErr
		}
		time.Sleep(time.Duration(attempt) * time.Second)
	}

	return lastErr
}

func (m *Module) existingBasketOrderSatisfied(product Product, basketIndex int, basket []Product) (bool, error) {
	if m.productOrderAlreadyRecorded(product, basketIndex, basket) {
		return true, nil
	}
	orders, err := m.ListOrderSnapshots()
	if err != nil {
		return false, err
	}
	required := requiredProductCountThroughIndex(product, basketIndex, basket)
	if required <= 0 {
		required = 1
	}
	if match, ok := matchingExistingOrder(product, orders, required); ok {
		m.recordExistingOrderMatch(product, match)
		return true, nil
	}
	return false, nil
}

func (m *Module) existingBasketOrdersSatisfied(basket []Product) (bool, int, error) {
	if len(basket) == 0 {
		return false, 0, nil
	}
	orders, err := m.ListOrderSnapshots()
	if err != nil {
		return false, 0, err
	}
	used := make([]bool, len(orders))
	matched := 0
	for _, product := range basket {
		for index, order := range orders {
			if used[index] || !orderMatchesCheckoutProduct(product, order) {
				continue
			}
			used[index] = true
			m.recordExistingOrderMatch(product, order)
			matched++
			break
		}
	}
	return matched >= len(basket), matched, nil
}

func (m *Module) placeOrder(address AddressFields, product Product, paymentID string, basketIndex int, basket []Product) error {
	m.runCheckpointedCheckoutBestEffort(
		checkoutProductCheckpoint("checkout:order:getCouponIncentiveData", product),
		"getCouponIncentiveData",
		map[string]any{
			"incentiveSource":  "NEW_USER_BLANKET_INCENTIVE_CAMPAIGN",
			"skipValidation":   false,
			"filterOutExpired": true,
		},
		`query getCouponIncentiveData($incentiveSource: IncentiveSource!, $skipValidation: Boolean = false, $filterOutExpired: Boolean = false) @attribution(owner: "buyer-growth") { getCouponIncentiveData( incentiveSource: $incentiveSource skipValidation: $skipValidation filterOutExpired: $filterOutExpired ) { __typename coupon { __typename id code discountType discountValue { __typename amount currency } minPurchaseValue { __typename amount currency } status maxUsesPerUser userCouponUsageInfo { __typename status numRemainingUses } } expirationTime couponClaimed couponApplied } }`,
	)

	if _, err := m.runCheckpointedRequest(checkoutProductCheckpoint("checkout:order:QuoteCart", product), func() error {
		return m.quoteCart(address, &product)
	}); err != nil {
		return err
	}

	m.runCheckpointedCheckoutBestEffort(
		checkoutProductCheckpoint("checkout:order:ClaimedCoupons", product),
		"ClaimedCoupons",
		map[string]any{"livestreamId": nil},
		`query ClaimedCoupons($livestreamId: ID) @attribution(owner: "community") { claimedCoupons(livestreamId: $livestreamId) { __typename ...RewardsCouponFragment } }
fragment RewardsCouponFragment on CouponNode @attribution(owner: "community") { __typename id code description limitingCriteriaDescription customerEligibilityType }`,
	)
	m.runCheckpointedCheckoutBestEffort(
		checkoutProductCheckpoint("checkout:order:DefaultPayment", product),
		"DefaultPayment",
		nil,
		`query DefaultPayment @attribution(owner: "selling") { userDefaultPayment { __typename id gateway cardType cardMetadata cardDescription } }`,
	)

	productID := placeOrderProductID(product)
	if productID == "" {
		return fmt.Errorf("missing product id for %s", product.ProductName)
	}
	if err := m.confirmPlaceOrderReady(product, basketIndex, basket); err != nil {
		return err
	}
	m.sleepDelay("before_place_order")

	res, err := m.sendMobileCheckoutRequest(
		"PlaceOrder",
		map[string]any{
			"email":              m.account.email,
			"fullName":           m.fullName(address),
			"addressLine1":       address.AddressLine1,
			"addressLine2":       address.AddressLine2,
			"addressCountryCode": checkoutCountryCode,
			"addressCity":        address.Town,
			"addressState":       address.Town,
			"addressPostalCode":  address.Postcode,
			"cctoken":            placeOrderCardTokenPayload,
			"cc4digs":            placeOrderCardDisplay,
			"items": []map[string]any{
				{
					"listingId": product.ProductListingID,
					"price": map[string]any{
						"amount":   product.ProductPrice,
						"currency": checkoutCurrency,
					},
					"productId": productID,
				},
			},
			"gateway":            "STRIPE",
			"orderStyle":         "DIRECT_ORDER",
			"credit":             nil,
			"salesChannel":       "STANDARD",
			"channelReference":   nil,
			"paymentId":          nil,
			"shippingQuoteToken": nil,
		},
		`mutation PlaceOrder($email: String, $fullName: String, $addressLine1: String, $addressLine2: String, $addressCountryCode: String, $addressCity: String, $addressState: String, $addressPostalCode: String, $cctoken: String, $cc4digs: String, $items: [OrderPlacementItem], $gateway: String, $orderStyle: OrderStyle, $credit: MoneyInput, $salesChannel: OrderSalesChannel, $channelReference: String, $paymentId: ID, $shippingQuoteToken: String) @attribution(owner: "selling") { placeOrder( email: $email fullName: $fullName addressLine1: $addressLine1 addressLine2: $addressLine2 addressCity: $addressCity addressState: $addressState addressPostalCode: $addressPostalCode addressCountryCode: $addressCountryCode fundingSource: $cctoken fundingSourceDisplay: $cc4digs shippingMethod: "standard" gateway: $gateway items: $items orderStyle: $orderStyle credit: $credit salesChannel: $salesChannel channelReference: $channelReference paymentId: $paymentId shippingQuoteToken: $shippingQuoteToken ) { __typename success message listing { __typename ...ListingFragment } order { __typename id } actionableError { __typename type paymentId } } }
fragment BreakFragment on Break @attribution(owner: "app-platform") { __typename id title description format status listingId createdAt totalBreakSpots filledBreakSpots spotType defaultTransactionType }
fragment ListingBaseFragment on ListingNode @attribution(owner: "app-platform") { __typename id uuid title subtitle listingStatus: publicStatus price { __typename ...MoneyFragment } conditionName productId: productIdV2 description quantity images { __typename ...ListingImageFragment } videos { __typename id duration thumbnailUrl url hasAudioTrack } isLive listingLabels: labels isSaved: userBookmark bookmarkCount: totalBookmarks livestreamStartTime shippingDetails category { __typename id label } transactionType user { __typename ...ListingUserFragment } listingAttributeValues { __typename value unit attribute { __typename id key label valueType } } transactionProps { __typename isOfferable giveaway { __typename onlyFollowers onlyDomestic buyerAppreciation buyerAppreciationSellerRules } auction { __typename durationSeconds isSuddenDeath } } asyncAuctionInfo: auctionInfo { __typename channelId bidCount endTime currentPrice { __typename ...MoneyFragment } } variantEnabled }
fragment ListingFragment on ListingNode @attribution(owner: "app-platform") { __typename ...ListingBaseFragment breakInfo: break { __typename ...BreakFragment } breakSpot { __typename id title } }
fragment ListingImageFragment on ListingImage @attribution(owner: "app-platform") { __typename id url key bucket label }
fragment ListingUserFragment on PublicUserNode @attribution(owner: "app-platform") { __typename id username isBlockingMe isBlockedByMe canBeMessagedByMe sellerRating { __typename overall numReviews } profileImage { __typename id url key bucket } isLive premierShopStatus { __typename ...PremierShopStatusFragment } }
fragment MoneyFragment on Money @attribution(owner: "app-platform") { __typename amount currency }
fragment PremierShopStatusFragment on PremierShopStatus @attribution(owner: "app-platform") { __typename isPremierShop }`,
	)
	if err != nil {
		return err
	}

	body := res.String()
	if strings.Contains(body, `"success":true`) || strings.Contains(body, `"success": true`) {
		placedOrderID := ""
		var dto struct {
			Data struct {
				PlaceOrder struct {
					Order struct {
						ID string `json:"id"`
					} `json:"order"`
				} `json:"placeOrder"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &dto); err == nil {
			placedOrderID = strings.TrimSpace(dto.Data.PlaceOrder.Order.ID)
			if placedOrderID != "" {
				m.lastOrderID = placedOrderID
				m.lastOrderIDs = append(m.lastOrderIDs, m.lastOrderID)
				m.lastOrderProducts = append(m.lastOrderProducts, product)
				m.markCheckpointDone(checkoutProductCheckpoint("checkout:order:PlaceOrder", product))
				m.autosaveSessionIfChanged("place order")
				if m.checkoutOrderHook != nil {
					m.checkoutOrderHook(m.CheckoutSnapshot())
				}
			}
		}
		if placedOrderID == "" {
			orders, err := m.ListOrderSnapshots()
			if err != nil {
				return fmt.Errorf("placeOrder returned success without order id; post-submit order check failed: %w", err)
			}
			required := requiredProductCountThroughIndex(product, basketIndex, basket)
			if match, ok := matchingExistingOrder(product, orders, required); ok {
				m.recordExistingOrderMatch(product, match)
				m.markCheckpointDone(checkoutProductCheckpoint("checkout:order:PlaceOrder", product))
			} else {
				return fmt.Errorf("placeOrder returned success without order id and no matching live order was found")
			}
		}
		m.log.Checkout("Order placed successfully.")
		return nil
	}
	if strings.Contains(body, "Not enough quantity remaining") {
		return errOutOfStock
	}
	if strings.Contains(body, "declined") {
		return fmt.Errorf("%w: %s", errCardDeclined, shortResponse(body))
	}

	return fmt.Errorf("unexpected place order response: %s", shortResponse(body))
}

func hasCheckoutAddress(address AddressFields) bool {
	return strings.TrimSpace(address.AddressLine1) != "" &&
		strings.TrimSpace(address.Town) != "" &&
		strings.TrimSpace(address.Postcode) != ""
}

func checkoutAddressSnapshotEqual(left AddressSnapshot, right AddressFields) bool {
	return normalizeAddressPart(left.Line1) == normalizeAddressPart(right.AddressLine1) &&
		normalizeAddressPart(left.Line2) == normalizeAddressPart(right.AddressLine2) &&
		normalizeAddressPart(left.City) == normalizeAddressPart(right.Town) &&
		normalizeAddressPart(left.PostalCode) == normalizeAddressPart(right.Postcode)
}

func normalizeAddressPart(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func (m *Module) quoteCart(address AddressFields, product *Product) error {
	if product == nil {
		return fmt.Errorf("missing product for quoteCart")
	}
	productID := placeOrderProductID(*product)
	if productID == "" {
		return fmt.Errorf("missing product id for quoteCart")
	}

	res, err := m.sendMobileCheckoutRequest(
		"QuoteCart",
		map[string]any{
			"email":              m.account.email,
			"addressLine1":       address.AddressLine1,
			"addressLine2":       address.AddressLine2,
			"addressCountryCode": checkoutCountryCode,
			"addressCity":        address.Town,
			"addressPhoneNumber": nil,
			"addressPostalCode":  address.Postcode,
			"addressState":       address.Town,
			"fullName":           m.fullName(address),
			"items": []map[string]any{
				{
					"listingConditionId": nil,
					"listingId":          product.ProductListingID,
					"price": map[string]any{
						"amount":   product.ProductPrice,
						"currency": checkoutCurrency,
					},
					"productId": productID,
				},
			},
			"orderStyle": "DIRECT_ORDER",
		},
		`mutation QuoteCart($email: String, $addressLine1: String, $addressLine2: String, $addressCountryCode: String, $addressCity: String, $addressPhoneNumber: String, $addressPostalCode: String, $addressState: String, $fullName: String, $items: [OrderPlacementItem], $orderStyle: OrderStyle) @attribution(owner: "selling") { quoteCart( email: $email addressLine1: $addressLine1 addressLine2: $addressLine2 addressCountryCode: $addressCountryCode addressCity: $addressCity addressPhoneNumber: $addressPhoneNumber addressPostalCode: $addressPostalCode addressState: $addressState fullName: $fullName items: $items orderStyle: $orderStyle shippingMethod: "standard" ) { __typename id success message authenticationFee { __typename ...MoneyFragment } credit { __typename ...MoneyFragment } isAddressValid shippingPrice { __typename ...MoneyFragment } subtotal { __typename ...MoneyFragment } taxes { __typename ...MoneyFragment } totalDiscount { __typename ...MoneyFragment } total { __typename ...MoneyFragment } localizedTotal { __typename ...MoneyFragment } alternativePaymentMethodTypes { __typename buyNowPayLaterOptions bankRedirectsOptions } } }
fragment MoneyFragment on Money @attribution(owner: "app-platform") { __typename amount currency }`,
	)
	if err != nil {
		return fmt.Errorf("quote cart request: %w", err)
	}

	var dto quoteCartResponse
	if err := res.JSON(&dto); err != nil {
		return fmt.Errorf("parse QuoteCart response: %w", err)
	}
	if err := validateQuoteCartResponse(*product, dto.Data.QuoteCart, res.String()); err != nil {
		return err
	}
	if dto.Data.QuoteCart.Subtotal != nil && dto.Data.QuoteCart.Subtotal.Amount > 0 && dto.Data.QuoteCart.Subtotal.Amount != product.ProductPrice {
		oldPrice := product.ProductPrice
		product.ProductPrice = dto.Data.QuoteCart.Subtotal.Amount
		m.log.Manager(fmt.Sprintf("QuoteCart price changed for %s: %s %.2f -> %.2f.", product.ProductName, checkoutCurrency, float64(oldPrice)/100, float64(product.ProductPrice)/100))
		m.priceUpdates = append(m.priceUpdates, PriceUpdate{
			ProductName:      product.ProductName,
			ProductListingID: product.ProductListingID,
			OldPrice:         oldPrice,
			NewPrice:         product.ProductPrice,
			MaxPrice:         product.MaxPrice,
		})
		_, _ = SetPriceAndInStockByListingIDWithMax(product.ProductListingID, product.ProductPrice, product.MaxPrice, true)
	}

	return nil
}

func validateQuoteCartResponse(product Product, quote quoteCartPayload, responseBody string) error {
	message := strings.TrimSpace(quote.Message)
	if strings.Contains(strings.ToLower(message), "not enough quantity") {
		return errOutOfStock
	}
	if !quote.Success {
		if message == "" {
			message = shortResponse(responseBody)
		}
		return fmt.Errorf("quoteCart failed: %s", message)
	}
	if strings.TrimSpace(quote.ID) == "" {
		return fmt.Errorf("quoteCart succeeded but returned no quote id: %s", shortResponse(responseBody))
	}
	if !quote.IsAddressValid {
		return fmt.Errorf("quoteCart marked the address invalid")
	}
	if quote.Subtotal == nil {
		return fmt.Errorf("quoteCart succeeded but returned no subtotal: %s", shortResponse(responseBody))
	}
	if err := validateQuoteSubtotal(product, quote.Subtotal); err != nil {
		return err
	}

	optionalMoney := map[string]*checkoutMoney{
		"authenticationFee": quote.AuthenticationFee,
		"credit":            quote.Credit,
		"shippingPrice":     quote.ShippingPrice,
		"taxes":             quote.Taxes,
		"totalDiscount":     quote.TotalDiscount,
		"total":             quote.Total,
		"localizedTotal":    quote.LocalizedTotal,
	}
	for label, money := range optionalMoney {
		if err := validateQuoteMoney(label, money, 0, false); err != nil {
			return err
		}
	}
	if quote.Total == nil {
		return fmt.Errorf("quoteCart succeeded but returned no total: %s", shortResponse(responseBody))
	}
	if quote.Total.Amount < 0 {
		return fmt.Errorf("quoteCart returned a negative total: %d", quote.Total.Amount)
	}
	if quote.LocalizedTotal != nil && quote.Total.Currency != "" && quote.LocalizedTotal.Currency != "" && quote.LocalizedTotal.Currency != quote.Total.Currency {
		return fmt.Errorf("quoteCart total currency mismatch: total=%s localizedTotal=%s", quote.Total.Currency, quote.LocalizedTotal.Currency)
	}

	return nil
}

func validateQuoteSubtotal(product Product, subtotal *checkoutMoney) error {
	if err := validateQuoteMoney("subtotal", subtotal, 0, false); err != nil {
		return err
	}
	if subtotal == nil {
		return nil
	}
	if subtotal.Amount == product.ProductPrice {
		return nil
	}
	// A quote below the stored price only ever costs less, so take it; quoteCart writes the
	// new price back to baskets.json. Only increases are gated by maxPrice.
	if subtotal.Amount > 0 && subtotal.Amount < product.ProductPrice {
		return nil
	}
	if product.MaxPrice > 0 {
		if subtotal.Amount <= product.MaxPrice {
			return nil
		}
		return fmt.Errorf("quoteCart product price changed above maxPrice for %s: got %d max %d previous %d", product.ProductName, subtotal.Amount, product.MaxPrice, product.ProductPrice)
	}
	return fmt.Errorf("quoteCart product price increased for %s and maxPrice is 0: got %d expected %d", product.ProductName, subtotal.Amount, product.ProductPrice)
}

func validateQuoteMoney(label string, money *checkoutMoney, expectedAmount int, requireExactAmount bool) error {
	if money == nil {
		return nil
	}
	if strings.TrimSpace(money.Currency) != checkoutCurrency {
		return fmt.Errorf("quoteCart %s currency mismatch: got %q expected %q", label, money.Currency, checkoutCurrency)
	}
	if money.Amount < 0 {
		return fmt.Errorf("quoteCart %s was negative: %d", label, money.Amount)
	}
	if requireExactAmount && money.Amount > expectedAmount {
		return fmt.Errorf("quoteCart %s above expected: got %d expected at most %d", label, money.Amount, expectedAmount)
	}
	return nil
}

func (m *Module) sendMobileCheckoutRequest(operationName string, variables any, query string) (*httpResponse, error) {
	if err := m.ensureSessionFreshForAction(operationName); err != nil {
		return nil, err
	}
	if err := m.syncMobileCheckoutCookies(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(mobileCheckoutBody{
		Extensions: map[string]any{
			"clientLibrary": map[string]string{
				"name":    mobileCheckoutLibraryName,
				"version": m.mobileCheckoutLibraryVersion(),
			},
		},
		OperationName: operationName,
		Query:         query,
		Variables:     variables,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", operationName, err)
	}

	feedSessionID := uuid.NewString()
	verticalScrollID := strings.ToUpper(uuid.NewString())
	viewID := strings.ToUpper(uuid.NewString())
	device := m.mobileDeviceForCheckout()

	req, err := m.mobileCheckoutRequest(operationName, query, payload, device, feedSessionID, verticalScrollID, viewID)
	if err != nil {
		return nil, err
	}
	res, err := m.doRequestWithRetry(operationName, req)
	if err != nil {
		return nil, fmt.Errorf("%s request failed: %w", operationName, err)
	}
	if isInvalidTokenResponse(res) {
		if err := m.refreshCheckoutTokens(operationName); err != nil {
			return nil, err
		}
		req, err = m.mobileCheckoutRequest(operationName, query, payload, device, feedSessionID, verticalScrollID, viewID)
		if err != nil {
			return nil, err
		}
		res, err = m.doRequestWithRetry(operationName+" after token refresh", req)
		if err != nil {
			return nil, fmt.Errorf("%s request failed after token refresh: %w", operationName, err)
		}
		if isInvalidTokenResponse(res) {
			if err := m.refreshCheckoutTokens(operationName + " second invalid token"); err != nil {
				return nil, err
			}
			req, err = m.mobileCheckoutRequest(operationName, query, payload, device, feedSessionID, verticalScrollID, viewID)
			if err != nil {
				return nil, err
			}
			res, err = m.doRequestWithRetry(operationName+" after second token refresh", req)
			if err != nil {
				return nil, fmt.Errorf("%s request failed after second token refresh: %w", operationName, err)
			}
		}
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s: %s", operationName, res.Status, shortResponse(res.String()))
	}

	return res, nil
}

func (m *Module) mobileCheckoutRequest(operationName string, query string, payload []byte, device mobileCheckoutDevice, feedSessionID string, verticalScrollID string, viewID string) (*http.Request, error) {
	if err := m.syncMobileCheckoutCookies(); err != nil {
		return nil, err
	}
	accessToken, err := m.checkoutAccessToken()
	if err != nil {
		return nil, err
	}
	req, reqErr := http.NewRequest("POST", fmt.Sprintf(mobileCheckoutGraphQLURL, url.QueryEscape(operationName)), bytes.NewReader(payload))
	if reqErr != nil {
		return nil, reqErr
	}
	req.Header = http.Header{
		"x-system-version":                     {device.IOSVersion},
		"x-client-timezone":                    {mobileCheckoutTimezone},
		"x-whatnot-vertical-scroll-session-id": {verticalScrollID},
		"user-agent":                           {device.UserAgent},
		"x-whatnot-feed-session-id":            {feedSessionID},
		"x-whatnot-app-build":                  {m.whatnotIOSAppBuild()},
		"x-apollo-operation-name":              {operationName},
		"x-datadog-origin":                     {"rum"},
		"x-whatnot-app":                        {"whatnot-ios"},
		"apollographql-client-name":            {mobileCheckoutClientName},
		"priority":                             {"u=3, i"},
		"apollographql-client-version":         {m.whatnotIOSApolloClientVersion()},
		"x-system-name":                        {mobileCheckoutSystemName},
		"x-whatnot-app-version":                {m.whatnotIOSAppVersion()},
		"x-whatnot-app-location":               {mobileCheckoutAppLocation},
		"x-whatnot-usgmt":                      {mobileCheckoutUSGMT},
		"authorization":                        {"Bearer " + accessToken},
		"accept-language":                      {"en-GB,en;q=0.9 "},
		"x-request-operation-tag":              {m.mobileCheckoutRequestTag()},
		"x-whatnot-view-id":                    {viewID},
		"accept":                               {"multipart/mixed;deferSpec=20220824,application/graphql-response+json,application/json"},
		"content-type":                         {"application/json"},
		"x-whatnot-app-session-id":             {strings.ToLower(m.session.appSessionID)},
		"accept-encoding":                      {"gzip, deflate, br"},
		"x-apollo-operation-type":              {graphqlOperationType(query)},
		http.HeaderOrderKey:                    {"x-system-version", "x-client-timezone", "x-whatnot-vertical-scroll-session-id", "user-agent", "x-whatnot-feed-session-id", "x-whatnot-app-build", "x-apollo-operation-name", "x-datadog-origin", "x-whatnot-app", "apollographql-client-name", "priority", "apollographql-client-version", "x-system-name", "x-whatnot-app-version", "x-whatnot-app-location", "x-whatnot-usgmt", "authorization", "accept-language", "x-request-operation-tag", "x-whatnot-view-id", "accept", "content-type", "x-whatnot-app-session-id", "accept-encoding", "x-apollo-operation-type"},
	}
	return req, nil
}

func (m *Module) refreshCheckoutTokens(operationName string) error {
	lastRefreshBeforeWait := m.lastSessionRefreshAt
	m.sessionRefreshMu.Lock()
	defer m.sessionRefreshMu.Unlock()
	if !m.lastSessionRefreshAt.Equal(lastRefreshBeforeWait) &&
		!m.lastSessionRefreshAt.IsZero() &&
		time.Since(m.lastSessionRefreshAt) < sessionRefreshMaxAge {
		if err := m.syncMobileCheckoutCookies(); err != nil {
			return fmt.Errorf("%s token cookie sync failed after shared refresh: %w", operationName, err)
		}
		return nil
	}
	if m.log != nil {
		m.log.ManagerNotice(operationName + " returned invalid token; refreshing session and retrying.")
	}
	healthy, err := m.checkIfAccountValid(m.session)
	if err != nil {
		return fmt.Errorf("%s token refresh failed: %w", operationName, err)
	}
	if !healthy {
		return fmt.Errorf("%s token refresh failed: session is unhealthy", operationName)
	}
	m.autosaveSessionIfChanged(operationName + " token refresh")
	if err := m.syncMobileCheckoutCookies(); err != nil {
		return fmt.Errorf("%s token cookie sync failed: %w", operationName, err)
	}
	return nil
}

func isInvalidTokenResponse(res *httpResponse) bool {
	if res == nil || res.StatusCode != http.StatusUnauthorized {
		return false
	}
	body := strings.ToLower(res.String())
	return strings.Contains(body, "invalid token")
}

func (m *Module) runCheckoutBestEffort(operationName string, variables any, query string) {
	if _, err := m.sendMobileCheckoutRequest(operationName, variables, query); err != nil {
		m.log.Scope(fmt.Sprintf("Mobile filler %s skipped: %v", operationName, err))
	}
}

func (m *Module) runCheckpointedCheckoutBestEffort(key string, operationName string, variables any, query string) {
	m.checkpointedBestEffort(key, func() {
		m.runCheckoutBestEffort(operationName, variables, query)
	})
}

func (m *Module) productOrderAlreadyRecorded(product Product, basketIndex int, basket []Product) bool {
	required := requiredProductCountThroughIndex(product, basketIndex, basket)
	if required <= 0 {
		required = 1
	}
	return countMatchingProducts(product, m.lastOrderProducts) >= required
}

func (m *Module) runCheckoutPreOrderHygiene() error {
	if err := m.ensureCheckoutUsernameAdequate(); err != nil {
		m.log.ManagerNotice("Checkout username preflight skipped: " + err.Error())
	}
	m.followRandomCheckoutSellers()
	return nil
}

func (m *Module) ensureCheckoutUsernameAdequate() error {
	if m == nil {
		return nil
	}
	if err := m.syncCurrentAccountUsername("checkout username preflight"); err != nil {
		return err
	}
	if checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName) {
		m.usernameAdequate = true
		m.usernameAdequateCheckedAt = time.Now().UTC()
		m.autosaveSessionIfChanged("checkout username adequate")
		return nil
	}
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
		if !checkoutUsernameAdequate(username, m.account.firstName, m.account.lastName) {
			lastErr = fmt.Errorf("generated username %s did not contain first or last name", username)
			continue
		}
		if available, err := m.usernameAvailableMobile(username); err == nil && !available {
			lastErr = fmt.Errorf("username %s is already taken", username)
			continue
		} else if err != nil {
			lastErr = fmt.Errorf("check username availability: %w", err)
			continue
		}
		m.log.Manager(fmt.Sprintf("Checkout username preflight: updating username to %s.", username))
		if err := m.updateUsernameMobileRaw(username); err != nil {
			lastErr = err
			if isUsernameCooldownError(err) || isUsernameOrdersProcessingError(err) {
				m.log.ManagerNotice("Checkout username preflight skipped: " + usernamePreflightSkipReason(err) + "; continuing checkout.")
				m.usernameAdequate = checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName)
				m.usernameAdequateCheckedAt = time.Now().UTC()
				m.autosaveSessionIfChanged("checkout username skipped")
				return nil
			}
			continue
		}
		m.log.Manager("Checkout username preflight complete.")
		return nil
	}
	m.usernameAdequate = false
	m.usernameAdequateCheckedAt = time.Now().UTC()
	m.autosaveSessionIfChanged("checkout username inadequate")
	if lastErr == nil {
		lastErr = fmt.Errorf("current username %q does not contain first or last name", m.account.username)
	}
	return fmt.Errorf("checkout username preflight failed: %w", lastErr)
}

func isUsernameOrdersProcessingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "orders are processing")
}

func usernamePreflightSkipReason(err error) string {
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "once per day"):
		return "username can only be changed once per day"
	case strings.Contains(text, "orders are processing"):
		return "username cannot be changed while orders are processing"
	default:
		return err.Error()
	}
}

func (m *Module) followRandomCheckoutSellers() {
	if m == nil || m.skipDelays {
		return
	}
	cfg := m.config
	if cfg == nil {
		return
	}
	minFollows := cfg.Engine.CheckoutPreorderMinFollows
	maxFollows := cfg.Engine.CheckoutPreorderMaxFollows
	if minFollows <= 0 || maxFollows <= 0 {
		return
	}
	if maxFollows < minFollows {
		minFollows, maxFollows = maxFollows, minFollows
	}
	target := minFollows
	if maxFollows > minFollows {
		target = minFollows + rand.Intn(maxFollows-minFollows+1)
	}
	ids, err := m.reviewSellerEntityIDs(cfg, target)
	if err != nil {
		m.log.ManagerNotice("Checkout seller follow preflight skipped: " + err.Error())
		return
	}
	if len(ids) > target {
		ids = ids[:target]
	}
	if len(ids) == 0 {
		return
	}
	m.log.Manager(fmt.Sprintf("Checkout follow preflight: following %d seller(s).", len(ids)))
	if err := m.bulkFollowSellerIDsWithDelay(ids, "between_checkout_follows"); err != nil {
		m.log.ManagerNotice("Checkout seller follow preflight skipped: " + err.Error())
		return
	}
	m.log.Manager("Checkout follow preflight complete.")
}

func (m *Module) confirmPlaceOrderReady(product Product, basketIndex int, basket []Product) error {
	if !m.forceCheckout {
		orders, err := m.ListOrderSnapshots()
		if err != nil {
			return fmt.Errorf("pre-submit order check failed: %w", err)
		}
		required := requiredProductCountThroughIndex(product, basketIndex, basket)
		if required <= 0 {
			required = 1
		}
		if match, ok := matchingExistingOrder(product, orders, required); ok {
			m.recordExistingOrderMatch(product, match)
			return fmt.Errorf("%w: %s", errOrderAlreadyExists, firstNonEmptyString(match.DisplayID, match.ExternalOrderID, metadataString(match.Metadata, "encoded_id"), product.ProductName))
		}
	}
	credits, err := m.GetMyCreditsSnapshot()
	if err != nil {
		if m.allowInsufficientBalance {
			m.log.ManagerError("Pre-submit credit check failed, continuing anyway: " + err.Error())
			return nil
		}
		return fmt.Errorf("pre-submit credit check failed: %w", err)
	}
	currency := strings.TrimSpace(credits.Currency)
	if currency == "" {
		currency = checkoutCurrency
	}
	if !strings.EqualFold(currency, checkoutCurrency) && !m.allowInsufficientBalance {
		return fmt.Errorf("pre-submit credit currency mismatch: got %s expected %s", currency, checkoutCurrency)
	}
	if credits.AmountSafe < product.ProductPrice {
		message := fmt.Sprintf("balance %s %.2f is below %s %.2f for %s", currency, float64(credits.AmountSafe)/100, checkoutCurrency, float64(product.ProductPrice)/100, product.ProductName)
		if !m.allowInsufficientBalance {
			return fmt.Errorf("pre-submit credit check failed: %s", message)
		}
		m.log.ManagerError("Placing order despite insufficient balance: " + message + ".")
	}
	m.rewardBalanceAmountSafe = credits.AmountSafe
	m.rewardBalanceCurrency = currency
	m.rewardCreditCheckError = ""
	m.autosaveSessionIfChanged("pre-submit credit check")
	return nil
}

func matchingExistingOrder(product Product, orders []OrderStatusSnapshot, requiredCount int) (OrderStatusSnapshot, bool) {
	if requiredCount <= 0 {
		requiredCount = 1
	}
	matched := 0
	var lastMatch OrderStatusSnapshot
	for _, order := range orders {
		if orderMatchesCheckoutProduct(product, order) {
			matched++
			lastMatch = order
			if matched >= requiredCount {
				return lastMatch, true
			}
		}
	}
	return OrderStatusSnapshot{}, false
}

func requiredProductCountThroughIndex(product Product, basketIndex int, basket []Product) int {
	if len(basket) == 0 {
		return 1
	}
	if basketIndex < 0 || basketIndex >= len(basket) {
		basketIndex = len(basket) - 1
	}
	count := 0
	for i := 0; i <= basketIndex; i++ {
		if checkoutProductsEquivalent(product, basket[i]) {
			count++
		}
	}
	return count
}

func countMatchingProducts(product Product, products []Product) int {
	count := 0
	for _, existing := range products {
		if checkoutProductsEquivalent(product, existing) {
			count++
		}
	}
	return count
}

func checkoutProductsEquivalent(left Product, right Product) bool {
	if strings.TrimSpace(left.ProductListingID) != "" && strings.TrimSpace(left.ProductListingID) == strings.TrimSpace(right.ProductListingID) {
		return true
	}
	if strings.TrimSpace(left.ProductID) != "" && strings.TrimSpace(left.ProductID) == strings.TrimSpace(right.ProductID) {
		return true
	}
	leftName := normalizeCheckoutProductName(left.ProductName)
	rightName := normalizeCheckoutProductName(right.ProductName)
	return leftName != "" && leftName == rightName
}

func orderMatchesCheckoutProduct(product Product, order OrderStatusSnapshot) bool {
	listingID := strings.TrimSpace(product.ProductListingID)
	if listingID != "" {
		for _, key := range []string{"listing_id", "listingId", "product_listing_id"} {
			if strings.TrimSpace(metadataString(order.Metadata, key)) == listingID {
				return true
			}
		}
		for _, existingID := range metadataStringSlice(order.Metadata, "listing_ids") {
			if strings.TrimSpace(existingID) == listingID {
				return true
			}
		}
	}
	productName := normalizeCheckoutProductName(product.ProductName)
	if productName == "" {
		return false
	}
	for _, item := range order.ItemNames {
		if normalizeCheckoutProductName(item) == productName {
			return true
		}
	}
	if normalizeCheckoutProductName(metadataString(order.Metadata, "listing_title")) == productName {
		return true
	}
	if normalizeCheckoutProductName(metadataString(order.Metadata, "items")) == productName {
		return true
	}
	return false
}

func normalizeCheckoutProductName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	builder.Grow(len(value))
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			builder.WriteRune(char)
		} else {
			builder.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(builder.String()), " ")
}

func (m *Module) recordExistingOrderMatch(product Product, order OrderStatusSnapshot) {
	orderID := firstNonEmptyString(order.ExternalOrderID, order.DisplayID, metadataString(order.Metadata, "encoded_id"))
	if orderID == "" {
		orderID = product.ProductListingID
	}
	if orderID != "" {
		m.lastOrderID = orderID
		m.lastOrderIDs = appendUniqueCheckoutString(m.lastOrderIDs, orderID)
	}
	m.lastOrderProducts = append(m.lastOrderProducts, product)
	m.autosaveSessionIfChanged("pre-submit existing order match")
	if m.checkoutOrderHook != nil {
		m.checkoutOrderHook(m.CheckoutSnapshot())
	}
}

func appendUniqueCheckoutString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.TrimSpace(existing) == value {
			return values
		}
	}
	return append(values, value)
}

func (m *Module) checkoutAccessToken() (string, error) {
	if err := m.ensureHTTPClient("checkout access token"); err != nil {
		return "", fmt.Errorf("missing %s cookie: recover client session: %w", accessTokenCookieName, err)
	}
	parsedURL, err := url.Parse(fmt.Sprintf("https://%s", WEB_HOST))
	if err != nil {
		return "", fmt.Errorf("parse whatnot url: %w", err)
	}

	for _, cookie := range m.client.GetCookieJar().Cookies(parsedURL) {
		if cookie.Name == accessTokenCookieName && strings.TrimSpace(cookie.Value) != "" {
			m.accessToken = strings.TrimSpace(cookie.Value)
			return m.accessToken, nil
		}
	}
	if strings.TrimSpace(m.accessToken) != "" {
		return strings.TrimSpace(m.accessToken), nil
	}

	return "", fmt.Errorf("missing %s cookie", accessTokenCookieName)
}

func (m *Module) syncMobileCheckoutCookies() error {
	if err := m.ensureHTTPClient("mobile checkout cookies"); err != nil {
		return fmt.Errorf("mobile checkout cookies: recover client session: %w", err)
	}
	webURL, err := url.Parse(fmt.Sprintf("https://%s", WEB_HOST))
	if err != nil {
		return err
	}
	apiURL, err := url.Parse("https://api.whatnot.com")
	if err != nil {
		return err
	}

	source := m.client.GetCookieJar().Cookies(webURL)
	copied := make([]*http.Cookie, 0, len(source)+4)
	for _, cookie := range source {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" || strings.TrimSpace(cookie.Value) == "" {
			continue
		}
		copied = append(copied, cloneCheckoutCookie(cookie))
	}

	accessToken := cookieValueFromHTTPCookies(source, accessTokenCookieName)
	if accessToken == "" {
		accessToken = strings.TrimSpace(m.accessToken)
		if accessToken != "" {
			copied = append(copied, newCheckoutCookie(accessTokenCookieName, accessToken))
		}
	} else {
		m.accessToken = accessToken
	}
	refreshToken := cookieValueFromHTTPCookies(source, refreshTokenCookieName)
	if accessToken != "" {
		copied = append(copied,
			newCheckoutCookie("__Secure-access-token-fp", "none"),
			newCheckoutCookie("__Secure-is-http-only-auth", "1"),
		)
		if expiration := jwtClaimString(accessToken, "exp"); expiration != "" {
			copied = append(copied, newCheckoutCookie("__Secure-access-token-expiration", expiration))
		}
	}
	if refreshToken != "" {
		copied = append(copied, newCheckoutCookie("__Secure-refresh-token-fp", "none"))
	}
	if len(copied) > 0 {
		m.client.GetCookieJar().SetCookies(apiURL, copied)
	}
	return nil
}

func cloneCheckoutCookie(cookie *http.Cookie) *http.Cookie {
	out := *cookie
	out.Domain = ""
	out.Path = normalizeCookiePath(out.Path)
	out.Secure = true
	return &out
}

func newCheckoutCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
	}
}

func cookieValueFromHTTPCookies(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie != nil && strings.EqualFold(strings.TrimSpace(cookie.Name), name) {
			return strings.TrimSpace(cookie.Value)
		}
	}
	return ""
}

func jwtClaimString(token, claim string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	switch value := claims[claim].(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return fmt.Sprintf("%.0f", value)
	default:
		return ""
	}
}

func (m *Module) fullName(address AddressFields) string {
	firstName := strings.TrimSpace(address.FirstName)
	if firstName == "" {
		firstName = strings.TrimSpace(m.account.firstName)
	}
	lastName := strings.TrimSpace(address.LastName)
	if lastName == "" {
		return firstName
	}
	if strings.Contains(lastName, " ") {
		return lastName
	}

	return strings.TrimSpace(firstName + " " + lastName)
}

func normalizeCardNumber(cardNumber string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, cardNumber)
}

func normalizeExpYear(expYear string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, expYear)

	if len(digits) == 4 && strings.HasPrefix(digits, "20") {
		return digits[2:]
	}

	return digits
}

func normalizeStripePostcode(postcode string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(postcode), " ", ""))
}

func placeOrderProductID(product Product) string {
	raw := strings.TrimSpace(product.ProductID)
	if raw == "" {
		return ""
	}
	if isDecimalString(raw) {
		return raw
	}

	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return raw
	}

	parts := strings.Split(string(decoded), ":")
	candidate := strings.TrimSpace(parts[len(parts)-1])
	if isDecimalString(candidate) {
		return candidate
	}

	return raw
}

func isDecimalString(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (m *Module) stripeUserAgentJSON(iosVersion string, machine string) string {
	bindingsVersion := m.stripeBindingsVersion()
	payload := map[string]string{
		"model":             "iPhone",
		"vendor_identifier": strings.ToUpper(uuid.NewString()),
		"bindings_version":  bindingsVersion,
		"lang":              "objective-c",
		"os_version":        strings.TrimSpace(iosVersion),
		"type":              strings.TrimSpace(machine),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"model":"iPhone","vendor_identifier":"%s","bindings_version":"%s","lang":"objective-c","os_version":"%s","type":"%s"}`, strings.ToUpper(uuid.NewString()), bindingsVersion, iosVersion, machine)
	}
	return string(raw)
}

func graphqlOperationType(query string) string {
	trimmed := strings.TrimSpace(query)
	if strings.HasPrefix(trimmed, "mutation") {
		return "mutation"
	}
	return "query"
}
