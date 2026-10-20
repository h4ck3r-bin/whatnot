package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

const friendCodeHealthTTL = time.Hour
const randomSellerReferralSource = "random_seller"

func (e *Engine) handleReferralSignupError(referral referralSelection, err error) error {
	if !isInvalidReferrerError(err) {
		return nil
	}
	if referral.Source == randomSellerReferralSource {
		logger.Manager(fmt.Sprintf("Random seller referral %s was not accepted; retrying with a different seller.", referral.Username))
		return nil
	}
	if removeErr := replaceFriendCodeWithInvalid(e.cfg.Engine.FriendCodesPath, referral.Username); removeErr != nil {
		return fmt.Errorf("mark invalid friend code %s immediately: %w", referral.Username, removeErr)
	}
	logger.Manager(fmt.Sprintf("Friend code %s marked INVALID in %s after referrer lookup failure.", referral.Username, e.cfg.Engine.FriendCodesPath))

	if referral.MasterID == "" {
		return nil
	}
	masters, loadErr := e.store.LoadMasters()
	if loadErr != nil {
		return fmt.Errorf("load masters while marking invalid referrer %s: %w", referral.Username, loadErr)
	}
	index := findMasterIndex(masters, referral.MasterID)
	if index < 0 {
		return nil
	}
	now := e.now()
	masters[index].State = MasterStateDormant
	masters[index].LastError = err.Error()
	masters[index].UpdatedAt = now
	if saveErr := e.store.SaveMasters(masters); saveErr != nil {
		return fmt.Errorf("save dormant invalid referrer master %s: %w", referral.Username, saveErr)
	}
	if removeErr := replaceFriendCodeWithInvalid(e.cfg.Engine.FriendCodesPath, referral.Username); removeErr != nil {
		return fmt.Errorf("mark invalid master friend code %s after dormant master save: %w", referral.Username, removeErr)
	}
	logger.Manager(fmt.Sprintf("Master referrer %s moved to Dormant and friend code was marked INVALID.", referral.Username))
	return nil
}

func (e *Engine) runSignupWithReferralRetry(ctx context.Context, masters []MasterAccount, label string, run func(referralSelection) (SignupResult, error)) (SignupResult, referralSelection, int, error) {
	var lastErr error
	invalidThisRun := map[string]bool{}
	for attempt := 1; attempt <= 10; attempt++ {
		if err := ctx.Err(); err != nil {
			return SignupResult{}, referralSelection{}, -1, err
		}
		referral, masterIndex, err := e.selectValidatedReferralCode(ctx, masters, invalidThisRun, label)
		if err != nil {
			if lastErr != nil {
				return SignupResult{}, referralSelection{}, -1, fmt.Errorf("%w; then select referral: %v", lastErr, err)
			}
			return SignupResult{}, referralSelection{}, -1, err
		}
		signup, err := run(referral)
		if err == nil {
			return signup, referral, masterIndex, nil
		}
		if !isInvalidReferrerError(err) {
			return SignupResult{}, referral, masterIndex, err
		}
		if markErr := e.handleReferralSignupError(referral, err); markErr != nil {
			return SignupResult{}, referral, masterIndex, markErr
		}
		lastErr = err
		excludeReferralForRun(invalidThisRun, referral.Username)
		logger.Manager(fmt.Sprintf("%s referral %s invalid; retrying with another code.", label, referral.Username))
		masters, _ = e.store.LoadMasters()
	}
	return SignupResult{}, referralSelection{}, -1, fmt.Errorf("%s exhausted referral retry attempts: %w", label, lastErr)
}

func (e *Engine) selectValidatedReferralCode(ctx context.Context, masters []MasterAccount, excluded map[string]bool, label string) (referralSelection, int, error) {
	var lastErr error
	for checks := 0; checks < 20; checks++ {
		referral, masterIndex, err := e.selectReferralCodeExcluding(masters, excluded)
		if err != nil {
			if lastErr != nil {
				return referralSelection{}, -1, fmt.Errorf("%w; then select referral: %v", lastErr, err)
			}
			return referralSelection{}, -1, err
		}
		if isRandomSellerReferral(referral.Username) {
			resolved, err := e.resolveRandomSellerReferral(ctx, excluded)
			if err != nil {
				lastErr = err
				excludeReferralForRun(excluded, referral.Username)
				logger.ManagerError(fmt.Sprintf("%s RANDOM_SELLER lookup failed; rotating to another code: %s", label, err.Error()))
				continue
			}
			return referralSelection{Username: resolved, Source: randomSellerReferralSource}, -1, nil
		}
		usable, permanentFailure, err := e.validateReferralCodeSession(ctx, referral)
		if usable {
			return referral, masterIndex, nil
		}
		lastErr = err
		excludeReferralForRun(excluded, referral.Username)
		if permanentFailure {
			if markErr := e.markReferralSessionInvalid(referral, err); markErr != nil {
				return referralSelection{}, -1, markErr
			}
			masters, _ = e.store.LoadMasters()
		}
		logger.ManagerError(fmt.Sprintf("%s referral %s failed health check; rotating to another code: %s", label, referral.Username, err.Error()))
	}
	return referralSelection{}, -1, fmt.Errorf("%s could not find a healthy referral code after validation: %w", label, lastErr)
}

func excludeReferralForRun(excluded map[string]bool, username string) {
	if excluded == nil {
		return
	}
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" {
		return
	}
	excluded[username] = true
}

func (e *Engine) validateReferralCodeSession(ctx context.Context, referral referralSelection) (bool, bool, error) {
	username := strings.TrimSpace(referral.Username)
	if username == "" {
		return false, false, fmt.Errorf("empty referral username")
	}
	accounts, err := e.store.LoadAccounts()
	if err != nil {
		return false, false, err
	}
	sessions, err := e.store.LoadNormalSessions()
	if err != nil {
		return false, false, err
	}
	accounts, accountIndex, session, err := e.ensureAccountFromQuery(accounts, sessions, username)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no stored account/session matched") {
			logger.Manager(fmt.Sprintf("Friend code %s has no stored session to validate; using it without local health cache.", username))
			return true, false, nil
		}
		return false, false, err
	}
	account := accounts[accountIndex]
	if friendCodeHealthCacheHealthy(session, e.now()) {
		return true, false, nil
	}
	refreshed, healthy, err := e.flows.RefreshSessionWithoutCredits(ctx, session)
	if err != nil || !healthy {
		permanent := isDefiniteSessionBan(err, healthy)
		return false, permanent, fmt.Errorf("referrer session refresh failed for %s: %s", firstNonEmptyString(account.Email, account.Username, username), sessionAuditError(err, healthy))
	}
	if err := validateRefreshedSession(session, refreshed); err != nil {
		return false, false, fmt.Errorf("referrer session refresh invalid for %s: %w", firstNonEmptyString(account.Email, account.Username, username), err)
	}
	now := e.now()
	if refreshed.Metadata == nil {
		refreshed.Metadata = map[string]any{}
	}
	refreshed.Metadata["friend_code_health_status"] = "healthy"
	refreshed.Metadata["friend_code_health_checked_at"] = now.Format(time.RFC3339)
	refreshed.ID = session.ID
	refreshed.AccountID = account.ID
	refreshed.CreatedAt = ensureTime(refreshed.CreatedAt, session.CreatedAt)
	refreshed.UpdatedAt = now
	sessions = upsertSession(sessions, refreshed)
	if err := e.store.SaveNormalSessions(sessions); err != nil {
		return false, false, err
	}
	if referral.MasterID != "" {
		if masterSessions, loadErr := e.store.LoadMasterSessions(); loadErr == nil {
			masterSessions = upsertSession(masterSessions, refreshed)
			_ = e.store.SaveMasterSessions(masterSessions)
		}
	}
	logger.Manager(fmt.Sprintf("Friend code %s health checked successfully; cached for 1h.", username))
	return true, false, nil
}

func friendCodeHealthCacheHealthy(session SessionRecord, now time.Time) bool {
	if !strings.EqualFold(metadataValue(session.Metadata, "friend_code_health_status"), "healthy") {
		return false
	}
	checkedAt, err := time.Parse(time.RFC3339, metadataValue(session.Metadata, "friend_code_health_checked_at"))
	if err != nil {
		return false
	}
	return now.Sub(checkedAt) < friendCodeHealthTTL
}

func (e *Engine) markReferralSessionInvalid(referral referralSelection, cause error) error {
	if err := removeFriendCode(e.cfg.Engine.FriendCodesPath, referral.Username); err != nil {
		return fmt.Errorf("remove friend code %s after failed health check: %w", referral.Username, err)
	}
	if referral.MasterID == "" {
		return nil
	}
	masters, err := e.store.LoadMasters()
	if err != nil {
		return err
	}
	index := findMasterIndex(masters, referral.MasterID)
	if index < 0 {
		return nil
	}
	now := e.now()
	masters[index].State = MasterStateDormant
	if cause != nil {
		masters[index].LastError = cause.Error()
	}
	masters[index].UpdatedAt = now
	if err := e.store.SaveMasters(masters); err != nil {
		return err
	}
	logger.Manager(fmt.Sprintf("Friend code %s removed after stored referrer session failed health check.", referral.Username))
	return nil
}
