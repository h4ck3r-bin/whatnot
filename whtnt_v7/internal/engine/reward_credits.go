package engine

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

func (e *Engine) handleRewardCreditUpdate(ctx context.Context, account Account, before SessionRecord, after SessionRecord) {
	if errText := metadataValue(after.Metadata, "reward_credit_check_error"); errText != "" {
		logger.ManagerError(fmt.Sprintf("Reward credits check failed for %s: %s", firstNonEmptyString(account.Email, account.Username, account.ID), errText))
		return
	}
	newAmount, ok := metadataIntValue(after.Metadata, "reward_balance_amount_safe")
	if !ok {
		return
	}
	currency := firstNonEmptyString(metadataValue(after.Metadata, "reward_balance_currency"), "credits")
	oldAmount, hadOldAmount := trustedStoredRewardCreditAmount(before.Metadata)
	if after.Metadata != nil {
		after.Metadata["reward_balance_last_seen_amount_safe"] = newAmount
		after.Metadata["reward_balance_last_seen_currency"] = currency
		after.Metadata["reward_balance_last_logged_amount_safe"] = newAmount
		after.Metadata["reward_balance_last_logged_currency"] = currency
	}
	if !hadOldAmount || newAmount <= oldAmount {
		return
	}
	delta := newAmount - oldAmount
	logger.Manager(fmt.Sprintf("Reward credits increased for %s: +%s (now %s).", firstNonEmptyString(account.Email, account.Username, account.ID), rewardCreditLine(delta, currency), rewardCreditLine(newAmount, currency)))
	if err := e.webhook.SendRewardCreditUpdate(ctx, account, oldAmount, newAmount, currency); err != nil {
		e.sendErrorWebhook(ctx, "Reward credit webhook failed", err, map[string]string{
			"Account": account.Email,
		})
	}
}

func trustedStoredRewardCreditAmount(metadata map[string]any) (int, bool) {
	if amount, ok := metadataIntValue(metadata, "reward_balance_last_seen_amount_safe"); ok {
		return amount, true
	}
	if metadataValue(metadata, "reward_credit_checked_at") != "" {
		if amount, ok := metadataIntValue(metadata, "reward_balance_amount_safe"); ok {
			return amount, true
		}
	}
	if amount, ok := metadataIntValue(metadata, "reward_balance_amount_safe"); ok && amount > 0 {
		return amount, true
	}
	if amount, ok := metadataIntValue(metadata, "referral_amount"); ok && amount > 0 {
		return amount, true
	}
	return 0, false
}

func initializeRewardCreditBaseline(session *SessionRecord) {
	if session == nil {
		return
	}
	amount := rewardAmountSafeFromSession(*session)
	if amount <= 0 {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	if _, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe"); !ok {
		session.Metadata["reward_balance_amount_safe"] = amount
	}
	if strings.TrimSpace(metadataValue(session.Metadata, "reward_balance_currency")) == "" {
		session.Metadata["reward_balance_currency"] = "GBP"
	}
	if _, ok := metadataIntValue(session.Metadata, "reward_balance_last_seen_amount_safe"); !ok {
		session.Metadata["reward_balance_last_seen_amount_safe"] = amount
		session.Metadata["reward_balance_last_seen_currency"] = metadataValue(session.Metadata, "reward_balance_currency")
	}
	if _, ok := metadataIntValue(session.Metadata, "reward_balance_last_logged_amount_safe"); !ok {
		session.Metadata["reward_balance_last_logged_amount_safe"] = amount
		session.Metadata["reward_balance_last_logged_currency"] = metadataValue(session.Metadata, "reward_balance_currency")
	}
}

func (e *Engine) sendHighRewardWebhookIfNeeded(ctx context.Context, account Account, session *SessionRecord) bool {
	if session == nil {
		return false
	}
	amount := rewardAmountSafeFromSession(*session)
	if amount <= 1000 {
		return false
	}
	if session.Metadata == nil {
		session.Metadata = map[string]any{}
	}
	amountKey := strconv.Itoa(amount)
	if metadataValue(session.Metadata, "high_reward_webhook_amount_safe") == amountKey {
		return false
	}
	if !e.webhook.Enabled() {
		return false
	}
	currency := firstNonEmptyString(metadataValue(session.Metadata, "reward_balance_currency"), "GBP")
	if err := e.webhook.SendHighReward(ctx, account, amount, currency); err != nil {
		e.sendErrorWebhook(ctx, "High reward webhook failed", err, map[string]string{
			"Account": account.Email,
		})
		return false
	}
	session.Metadata["high_reward_webhook_amount_safe"] = amountKey
	session.Metadata["high_reward_webhook_sent_at"] = e.now().Format(time.RFC3339)
	logger.Manager(fmt.Sprintf("High reward webhook sent for %s: %s.", firstNonEmptyString(account.Email, account.Username, account.ID), rewardCreditLine(amount, currency)))
	return true
}

func storedRewardCreditAmount(metadata map[string]any) (int, bool) {
	known := []string{
		"reward_balance_last_seen_amount_safe",
		"reward_balance_amount_safe",
		"referral_amount",
	}
	best := 0
	found := false
	for _, key := range known {
		amount, ok := metadataIntValue(metadata, key)
		if !ok {
			continue
		}
		if !found || amount > best {
			best = amount
		}
		found = true
	}
	return best, found
}

func metadataIntValue(metadata map[string]any, key string) (int, bool) {
	if metadata == nil {
		return 0, false
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch value := raw.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case jsonNumber:
		parsed, err := value.Int64()
		return int(parsed), err == nil
	case string:
		if strings.TrimSpace(value) == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		return parsed, err == nil
	default:
		parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed, err == nil
	}
}

type jsonNumber interface {
	Int64() (int64, error)
}

func rewardCreditLine(amount int, currency string) string {
	currency = firstNonEmptyString(currency, "credits")
	return fmt.Sprintf("%.2f %s", float64(amount)/100, currency)
}

func sessionCreditLabel(session SessionRecord) string {
	amount, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe")
	if !ok {
		return ""
	}
	return rewardCreditLine(amount, metadataValue(session.Metadata, "reward_balance_currency"))
}

func metadataCreditLabel(metadata map[string]any) string {
	amount, ok := metadataIntValue(metadata, "reward_balance_amount_safe")
	if !ok {
		return ""
	}
	return rewardCreditLine(amount, metadataValue(metadata, "reward_balance_currency"))
}

func accountMentionLabel(account Account, credits string) string {
	label := firstNonEmptyString(account.Username, account.Email, account.ID)
	if credits == "" {
		return label
	}
	return fmt.Sprintf("%s (%s)", label, credits)
}

func addSessionCreditMetadata(metadata map[string]any, session SessionRecord) {
	if metadata == nil {
		return
	}
	amount, ok := metadataIntValue(session.Metadata, "reward_balance_amount_safe")
	if !ok {
		return
	}
	metadata["reward_balance_amount_safe"] = amount
	metadata["reward_balance_currency"] = metadataValue(session.Metadata, "reward_balance_currency")
}
