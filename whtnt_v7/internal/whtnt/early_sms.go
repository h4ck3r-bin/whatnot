package whtnt

import (
	"context"
	"fmt"
	"strings"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/smsmanager"
)

// reserveEarlySMSNumber claims an SMS number before the web pre-registration
// step so the same number can be used in both the web invite flow and the
// later iOS onboarding phone verification.
func (m *Module) reserveEarlySMSNumber() error {
	if strings.TrimSpace(m.lastSMSPhoneNumber) != "" {
		return nil
	}

	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	smsProvider := chooseSMSProvider(cfg.SMSService, "")
	smsCountryCode := chooseSMSCountryCode(cfg, smsProvider)
	m.log.Phone(fmt.Sprintf("Early SMS reservation: %s (%s)", smsProvider, smsCountryCode))

	smsClient := smsmanager.NewClient(smsmanager.Config{
		Provider:        smsProvider,
		HeroSMSAPIKey:   cfg.HeroSMSKey,
		SMSPoolAPIKey:   cfg.SMSPoolKey,
		SMSBowerAPIKey:  cfg.SMSBowerKey,
		SMSOnlineAPIKey: cfg.SMSOnlineKey,
	})

	activationID, smsPhoneNumber, err := smsClient.ClaimNumber(context.Background(), smsCountryCode, nil)
	if err != nil {
		return fmt.Errorf("claim SMS number: %w", err)
	}

	smsPhoneNumber = "+" + smsPhoneNumber
	m.lastSMSProvider = smsProvider
	m.lastSMSActivationID = activationID
	m.lastSMSPhoneNumber = smsPhoneNumber
	m.lastSMSCode = ""
	m.autosaveSessionIfChanged("early sms number claimed")

	m.log.Phone("Early SMS number reserved: " + smsPhoneNumber)
	return nil
}
