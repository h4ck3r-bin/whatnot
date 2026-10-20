package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

const (
	discordGreen  = 0x2ECC71
	discordBlue   = 0x3498DB
	discordPurple = 0x9B59B6
	discordGold   = 0xF1C40F
	discordRed    = 0xE74C3C
)

const errorWebhookDedupeTTL = 24 * time.Hour
const webhookSendAttempts = 5
const webhookDefaultRetryDelay = 2 * time.Second
const webhookMaxRetryDelay = 30 * time.Second

var webhookURLLocks sync.Map

type WebhookClient struct {
	URL string
}

type discordWebhookPayload struct {
	Content   string         `json:"content,omitempty"`
	Username  string         `json:"username,omitempty"`
	AvatarURL string         `json:"avatar_url,omitempty"`
	Embeds    []discordEmbed `json:"embeds,omitempty"`
}

type discordEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	Color       int                 `json:"color,omitempty"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Footer      *discordEmbedFooter `json:"footer,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbedFooter struct {
	Text string `json:"text,omitempty"`
}

func (w WebhookClient) Enabled() bool {
	url := strings.TrimSpace(w.URL)
	return url != "" && !strings.EqualFold(url, "hook") && !strings.EqualFold(url, "errorhook")
}

func (w WebhookClient) Send(ctx context.Context, message string) error {
	return w.sendPayload(ctx, discordWebhookPayload{
		Username: "Whatnot Engine",
		Content:  truncateDiscord(message, 1900),
	})
}

func (w WebhookClient) SendCheckout(ctx context.Context, account Account, order Order) error {
	liveUsername := firstNonEmptyString(metadataValue(order.Metadata, "account_username"), account.Username)
	accountForLabel := account
	if strings.TrimSpace(liveUsername) != "" {
		accountForLabel.Username = liveUsername
	}
	fields := []discordEmbedField{
		{Name: "Account", Value: accountLabelWithCredits(accountForLabel, metadataCreditLabel(order.Metadata)), Inline: true},
		{Name: "Username", Value: emptyDash(liveUsername), Inline: true},
		{Name: "Order", Value: orderLabel(order), Inline: true},
		{Name: "Seller", Value: emptyDash(metadataValue(order.Metadata, "seller_username")), Inline: true},
		{Name: "Items", Value: metadataValueDefault(order.Metadata, "items", "Unknown"), Inline: false},
		{Name: "Total", Value: formatMoneyLine(metadataValueDefault(order.Metadata, "total", "Unknown")), Inline: true},
		{Name: "Referral", Value: emptyDash(account.MasterUsernameUsed), Inline: true},
	}
	if address := metadataValue(order.Metadata, "shipping_address_full"); address != "" {
		fields = append(fields, discordEmbedField{Name: "Shipping address", Value: address, Inline: false})
	}
	if lastName := metadataValue(order.Metadata, "shipping_last_name"); lastName != "" {
		fields = append(fields, discordEmbedField{Name: "Shipping name", Value: lastName, Inline: true})
	}
	if card := metadataValue(order.Metadata, "card_hidden_discord"); card != "" {
		fields = append(fields, discordEmbedField{Name: "Card", Value: card, Inline: true})
	} else if last4 := metadataValue(order.Metadata, "card_last4"); last4 != "" {
		fields = append(fields, discordEmbedField{Name: "Card", Value: "||**** **** **** " + last4 + "||", Inline: true})
	}
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Checkout successful",
		Description: "A checkout completed and the order has been saved.",
		Color:       discordGreen,
		Fields:      fields,
		Footer:      &discordEmbedFooter{Text: "WhatBot"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendRewardCreditUpdate(ctx context.Context, account Account, oldAmount int, newAmount int, currency string) error {
	delta := newAmount - oldAmount
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Reward credits increased",
		Description: "A refreshed account has more reward credit than the last saved balance.",
		Color:       discordGold,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountLabelWithCredits(account, rewardCreditLine(newAmount, currency)), Inline: true},
			{Name: "Previous", Value: rewardCreditLine(oldAmount, currency), Inline: true},
			{Name: "Current", Value: rewardCreditLine(newAmount, currency), Inline: true},
			{Name: "Increase", Value: rewardCreditLine(delta, currency), Inline: true},
			{Name: "Referral", Value: emptyDash(account.MasterUsernameUsed), Inline: true},
		},
		Footer:    &discordEmbedFooter{Text: "Reward credit discovery"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendHighReward(ctx context.Context, account Account, amount int, currency string) error {
	reward := rewardCreditLine(amount, firstNonEmptyString(currency, "GBP"))
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "High reward found",
		Description: reward,
		Color:       discordGold,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountMentionLabel(account, reward), Inline: true},
			{Name: "Email", Value: emptyDash(account.Email), Inline: true},
			{Name: "Username", Value: emptyDash(account.Username), Inline: true},
			{Name: "Referral", Value: emptyDash(account.MasterUsernameUsed), Inline: true},
		},
		Footer:    &discordEmbedFooter{Text: "Reward discovery"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendFriendCodeCreditSummary(ctx context.Context, totalCredits int, currency string, counts map[int]int, summary FriendCodeCreditSummary) error {
	currency = firstNonEmptyString(currency, "GBP")
	lines := []string{
		"Total credits: " + summaryCreditLine(totalCredits, currency),
	}
	amounts := make([]int, 0, len(counts))
	for amount := range counts {
		amounts = append(amounts, amount)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(amounts)))
	for _, amount := range amounts {
		lines = append(lines, fmt.Sprintf("%s: %d", summaryCreditLine(amount, currency), counts[amount]))
	}
	if len(amounts) == 0 {
		lines = append(lines, "No proven credit balances found.")
	}
	lines = append(lines, fmt.Sprintf("Banned: %d", summary.Banned))
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Friend code credit summary",
		Description: strings.Join(lines, "\n"),
		Color:       discordGold,
		Fields: []discordEmbedField{
			{Name: "Checked", Value: strconv.Itoa(summary.Checked), Inline: true},
			{Name: "Healthy", Value: strconv.Itoa(summary.Healthy), Inline: true},
			{Name: "Updated", Value: strconv.Itoa(summary.Updated), Inline: true},
			{Name: "Failed", Value: strconv.Itoa(summary.Failed), Inline: true},
			{Name: "Missing", Value: strconv.Itoa(summary.Missing), Inline: true},
			{Name: "Skipped", Value: strconv.Itoa(summary.Skipped), Inline: true},
		},
		Footer:    &discordEmbedFooter{Text: "Check Friend Code Credits"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func summaryCreditLine(amount int, currency string) string {
	if strings.EqualFold(strings.TrimSpace(currency), "GBP") || strings.TrimSpace(currency) == "" {
		if amount%100 == 0 {
			return fmt.Sprintf("\u00a3%d", amount/100)
		}
		return fmt.Sprintf("\u00a3%.2f", float64(amount)/100)
	}
	return rewardCreditLine(amount, currency)
}

func (w WebhookClient) SendUpdateAccountsSummary(ctx context.Context, counts map[string]int) error {
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		status = strings.TrimSpace(status)
		if status != "" {
			statuses = append(statuses, status)
		}
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		if counts[statuses[i]] != counts[statuses[j]] {
			return counts[statuses[i]] > counts[statuses[j]]
		}
		return strings.ToLower(statuses[i]) < strings.ToLower(statuses[j])
	})
	lines := make([]string, 0, len(statuses))
	for _, status := range statuses {
		lines = append(lines, fmt.Sprintf("%s: %d", status, counts[status]))
	}
	if len(lines) == 0 {
		lines = append(lines, "No accounts checked: 0")
	}
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Update Accounts summary",
		Description: strings.Join(lines, "\n"),
		Color:       discordBlue,
		Footer:      &discordEmbedFooter{Text: "Update Accounts"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendOngoingOrdersSummary(ctx context.Context, summary OngoingOrdersExportSummary) error {
	lines := []string{}
	if len(summary.Statuses) == 0 {
		lines = append(lines, "No freshly checked orders found in scope.")
	} else {
		for _, status := range summary.Statuses {
			lines = append(lines, fmt.Sprintf("%s: %d", status.Status, status.Count))
		}
	}
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Ongoing orders summary",
		Description: strings.Join(lines, "\n"),
		Color:       discordBlue,
		Fields: []discordEmbedField{
			{Name: "Ongoing exported", Value: strconv.Itoa(summary.Exported), Inline: true},
			{Name: "Done/skipped", Value: strconv.Itoa(summary.Skipped), Inline: true},
			{Name: "CSV", Value: emptyDash(summary.Path), Inline: false},
		},
		Footer:    &discordEmbedFooter{Text: "Export Ongoing Orders"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendTracking(ctx context.Context, account Account, order Order, metadata map[string]any) error {
	fields := []discordEmbedField{
		{Name: "Account", Value: accountLabelWithCredits(account, metadataCreditLabel(order.Metadata)), Inline: true},
		{Name: "Order", Value: firstMetadataString(metadata, "display_id", order.ExternalOrderID, order.ID), Inline: true},
		{Name: "Seller", Value: emptyDash(metadataValue(metadata, "seller_username")), Inline: true},
		{Name: "Tracking", Value: emptyDash(order.TrackingNumber), Inline: true},
		{Name: "Service", Value: metadataValueDefault(metadata, "shipping_service", "Unknown"), Inline: true},
		{Name: "Items", Value: metadataValueDefault(metadata, "items", "Unknown"), Inline: false},
	}
	if value := metadataValue(metadata, "tracking_url"); value != "" {
		fields = append(fields, discordEmbedField{Name: "Tracking URL", Value: value, Inline: false})
	}
	if value := metadataValue(metadata, "total"); value != "" {
		fields = append(fields, discordEmbedField{Name: "Total", Value: formatMoneyLine(value), Inline: true})
	}
	embed := discordEmbed{
		Title:       "Tracking available",
		Description: "Tracking was found and has been marked as sent.",
		Color:       discordBlue,
		Fields:      fields,
		Footer:      &discordEmbedFooter{Text: "Tracking is only sent once per order"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if err := w.sendEmbed(ctx, embed); err == nil {
		return nil
	} else {
		logger.ManagerError("Tracking embed failed; retrying as plain text: " + err.Error())
		return w.sendTrackingPlain(ctx, account, order, metadata)
	}
}

func (w WebhookClient) sendTrackingPlain(ctx context.Context, account Account, order Order, metadata map[string]any) error {
	lines := []string{
		"Tracking available",
		"Account: " + strings.ReplaceAll(accountLabelWithCredits(account, metadataCreditLabel(order.Metadata)), "\n", " | "),
		"Order: " + firstMetadataString(metadata, "display_id", order.ExternalOrderID, order.ID),
		"Seller: " + emptyDash(metadataValue(metadata, "seller_username")),
		"Tracking: " + emptyDash(order.TrackingNumber),
	}
	if value := metadataValue(metadata, "tracking_url"); value != "" {
		lines = append(lines, "Tracking URL: "+value)
	}
	if value := metadataValue(metadata, "items"); value != "" {
		lines = append(lines, "Items: "+value)
	}
	return w.sendPayload(ctx, discordWebhookPayload{
		Username: "Whatnot Engine",
		Content:  truncateDiscord(strings.Join(lines, "\n"), 1900),
	})
}

func (w WebhookClient) SendPendingReview(ctx context.Context, account Account, order Order) error {
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Pending review found",
		Description: "An order is ready for manual review.",
		Color:       discordGold,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountLabelWithCredits(account, metadataCreditLabel(order.Metadata)), Inline: true},
			{Name: "Order", Value: orderLabel(order), Inline: true},
			{Name: "Seller", Value: emptyDash(metadataValue(order.Metadata, "seller_username")), Inline: true},
			{Name: "Items", Value: metadataValueDefault(order.Metadata, "items", "Unknown"), Inline: false},
			{Name: "Review ID", Value: emptyDash(order.ReviewReferenceID), Inline: true},
		},
		Footer:    &discordEmbedFooter{Text: "Use Review Mode to submit"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendReview(ctx context.Context, account Account, order Order, reviewText string) error {
	explanation := strings.TrimSpace(reviewText)
	if explanation == "" {
		explanation = "No text"
	}
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Review submitted",
		Description: "A 5-star review was submitted and the order is complete.",
		Color:       discordPurple,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountLabelWithCredits(account, metadataCreditLabel(order.Metadata)), Inline: true},
			{Name: "Order", Value: orderLabel(order), Inline: true},
			{Name: "Seller", Value: emptyDash(metadataValue(order.Metadata, "seller_username")), Inline: true},
			{Name: "Rating", Value: "5 stars", Inline: true},
			{Name: "New username", Value: emptyDash(metadataValue(order.Metadata, "review_new_username")), Inline: true},
			{Name: "Accounts followed", Value: emptyDash(metadataValue(order.Metadata, "review_accounts_followed")), Inline: true},
			{Name: "Review text", Value: truncateDiscord(explanation, 1000), Inline: false},
		},
		Footer:    &discordEmbedFooter{Text: "Normal account will not be reused"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendMessageDiscovery(ctx context.Context, account Account, message AccountMessageSummary) error {
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Unread message found",
		Description: "An account has a new or unread conversation.",
		Color:       discordBlue,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountLabelWithCredits(account, message.AccountCredits), Inline: true},
			{Name: "From", Value: emptyDash(firstNonEmptyString(message.Username, message.DisplayName)), Inline: true},
			{Name: "Linked order", Value: emptyDash(message.LinkedOrder), Inline: true},
			{Name: "Item", Value: emptyDash(message.LinkedItem), Inline: false},
			{Name: "Preview", Value: truncateDiscord(emptyDash(message.Preview), 1000), Inline: false},
		},
		Footer:    &discordEmbedFooter{Text: "Account update discovery"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendNotificationDiscovery(ctx context.Context, account Account, notification AccountNotification) error {
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Unread notification found",
		Description: "An account has a new notification.",
		Color:       discordGold,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountLabelWithCredits(account, notification.AccountCredits), Inline: true},
			{Name: "Type", Value: emptyDash(notification.ActivityType), Inline: true},
			{Name: "Actor", Value: emptyDash(notification.PrimaryActor), Inline: true},
			{Name: "Listing", Value: emptyDash(notification.RelatedListing), Inline: true},
			{Name: "Message", Value: truncateDiscord(emptyDash(notification.Message), 1000), Inline: false},
		},
		Footer:    &discordEmbedFooter{Text: "Account update discovery"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendOrderStatusChange(ctx context.Context, account Account, before Order, after Order) error {
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Order status changed",
		Description: "An account order has a new status.",
		Color:       discordBlue,
		Fields: []discordEmbedField{
			{Name: "Account", Value: accountLabelWithCredits(account, metadataCreditLabel(after.Metadata)), Inline: true},
			{Name: "Order", Value: orderLabel(after), Inline: true},
			{Name: "Seller", Value: emptyDash(metadataValue(after.Metadata, "seller_username")), Inline: true},
			{Name: "Previous", Value: emptyDash(orderStatusLabel(before)), Inline: true},
			{Name: "Current", Value: emptyDash(orderStatusLabel(after)), Inline: true},
			{Name: "Items", Value: metadataValueDefault(after.Metadata, "items", "Unknown"), Inline: false},
		},
		Footer:    &discordEmbedFooter{Text: "Account update discovery"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendMasterReady(ctx context.Context, master MasterAccount) error {
	return w.sendEmbed(ctx, discordEmbed{
		Title:       "Master ready",
		Description: "A Master has finished aging and is ready for Master Mode.",
		Color:       discordGold,
		Fields: []discordEmbedField{
			{Name: "Username", Value: emptyDash(master.Username), Inline: true},
			{Name: "Email", Value: emptyDash(master.Email), Inline: true},
			{Name: "Signup Count", Value: fmt.Sprintf("%d / %d", master.SignupCount, master.SignupCap), Inline: true},
			{Name: "State", Value: string(master.State), Inline: true},
		},
		Footer:    &discordEmbedFooter{Text: "Master lifecycle"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func (w WebhookClient) SendError(ctx context.Context, title string, err error, fields map[string]string) error {
	if err == nil {
		return nil
	}
	embedFields := []discordEmbedField{
		{Name: "Error", Value: truncateDiscord(err.Error(), 1000), Inline: false},
	}
	for name, value := range fields {
		embedFields = append(embedFields, discordEmbedField{
			Name:   name,
			Value:  emptyDash(value),
			Inline: true,
		})
	}
	return w.sendEmbed(ctx, discordEmbed{
		Title:       firstNonEmptyString(title, "Engine error"),
		Description: "The engine caught an error that needs attention.",
		Color:       discordRed,
		Fields:      embedFields,
		Footer:      &discordEmbedFooter{Text: "Error webhook"},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	})
}

func (e *Engine) sendErrorWebhook(ctx context.Context, title string, err error, fields map[string]string) {
	if err == nil {
		return
	}
	if !shouldSendErrorWebhookNotification(title, err) {
		return
	}
	key := errorWebhookDedupeKey(title, err, fields)
	allowed, dedupeErr := e.shouldSendDedupedErrorWebhook(key, e.now())
	if dedupeErr != nil {
		logger.ManagerError("Error webhook dedupe failed: " + dedupeErr.Error())
	} else if !allowed {
		logger.Manager("Error webhook suppressed for duplicate within 24h: " + firstNonEmptyString(title, "Engine error"))
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if sendErr := e.errorWebhook.SendError(ctx, title, err, fields); sendErr != nil {
		logger.ManagerError("Error webhook failed: " + sendErr.Error())
	}
}

func (e *Engine) shouldSendDedupedErrorWebhook(key string, now time.Time) (bool, error) {
	if strings.TrimSpace(key) == "" {
		return true, nil
	}
	seen := map[string]time.Time{}
	if err := e.store.LoadBackendJSON("error_webhook_dedupe", &seen); err != nil {
		return true, err
	}
	cutoff := now.Add(-errorWebhookDedupeTTL)
	for existingKey, ts := range seen {
		if ts.Before(cutoff) {
			delete(seen, existingKey)
		}
	}
	if ts, ok := seen[key]; ok && now.Sub(ts) < errorWebhookDedupeTTL {
		return false, nil
	}
	seen[key] = now
	return true, e.store.SaveBackendJSON("error_webhook_dedupe", seen)
}

func (w WebhookClient) sendEmbed(ctx context.Context, embed discordEmbed) error {
	return w.sendPayload(ctx, discordWebhookPayload{
		Username: "Whatnot Engine",
		Embeds:   []discordEmbed{sanitizeEmbed(embed)},
	})
}

func (w WebhookClient) sendPayload(ctx context.Context, payload discordWebhookPayload) error {
	url := strings.TrimSpace(w.URL)
	if url == "" || strings.EqualFold(url, "hook") || strings.EqualFold(url, "errorhook") {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	lockValue, _ := webhookURLLocks.LoadOrStore(url, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	client := http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= webhookSendAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("content-type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt == webhookSendAttempts || !webhookErrorRetryable(err) {
				return err
			}
			if sleepErr := sleepWebhookRetry(ctx, webhookBackoffDelay(attempt)); sleepErr != nil {
				return sleepErr
			}
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(res.Body, 4000))
		_ = res.Body.Close()
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("discord webhook returned %s: %s", res.Status, strings.TrimSpace(string(body)))
		if attempt == webhookSendAttempts || !webhookStatusRetryable(res.StatusCode) {
			return lastErr
		}
		delay := webhookRetryAfter(res, body, attempt)
		if sleepErr := sleepWebhookRetry(ctx, delay); sleepErr != nil {
			return sleepErr
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("discord webhook failed after %d attempts", webhookSendAttempts)
}

func webhookStatusRetryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}

func webhookErrorRetryable(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "timeout") ||
		strings.Contains(text, "connection reset") ||
		strings.Contains(text, "connection refused") ||
		strings.Contains(text, "temporary") ||
		strings.Contains(text, "eof")
}

func webhookRetryAfter(res *http.Response, body []byte, attempt int) time.Duration {
	if res != nil {
		for _, header := range []string{"Retry-After", "X-RateLimit-Reset-After"} {
			if delay := parseDiscordRetryAfter(res.Header.Get(header)); delay > 0 {
				return clampWebhookRetryDelay(delay)
			}
		}
	}
	var dto struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if len(body) > 0 && json.Unmarshal(body, &dto) == nil && dto.RetryAfter > 0 {
		return clampWebhookRetryDelay(time.Duration(dto.RetryAfter * float64(time.Second)))
	}
	return webhookBackoffDelay(attempt)
}

func parseDiscordRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

func webhookBackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(attempt) * webhookDefaultRetryDelay
	return clampWebhookRetryDelay(delay)
}

func clampWebhookRetryDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return webhookDefaultRetryDelay
	}
	if delay > webhookMaxRetryDelay {
		return webhookMaxRetryDelay
	}
	return delay
}

func sleepWebhookRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func sanitizeEmbed(embed discordEmbed) discordEmbed {
	embed.Title = truncateDiscord(embed.Title, 256)
	embed.Description = truncateDiscord(embed.Description, 4096)
	for i := range embed.Fields {
		embed.Fields[i].Name = truncateDiscord(emptyDash(embed.Fields[i].Name), 256)
		embed.Fields[i].Value = truncateDiscord(emptyDash(embed.Fields[i].Value), 1024)
	}
	return embed
}

func accountLabel(account Account) string {
	if strings.TrimSpace(account.Username) != "" {
		return fmt.Sprintf("%s\n%s", account.Username, account.Email)
	}
	return emptyDash(account.Email)
}

func accountLabelWithCredits(account Account, credits string) string {
	credits = strings.TrimSpace(credits)
	if credits == "" {
		return accountLabel(account)
	}
	if strings.TrimSpace(account.Username) != "" {
		return fmt.Sprintf("%s (%s)\n%s", account.Username, credits, account.Email)
	}
	return fmt.Sprintf("%s (%s)", emptyDash(account.Email), credits)
}

func orderLabel(order Order) string {
	return firstNonEmptyString(
		metadataValue(order.Metadata, "display_id"),
		order.ExternalOrderID,
		order.ID,
	)
}

func orderStatusLabel(order Order) string {
	return firstNonEmptyString(metadataValue(order.Metadata, "pretty_status"), metadataValue(order.Metadata, "overview_status"), string(order.State))
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if cleaned := cleanMetadataString(value); cleaned != "" {
			return cleaned
		}
	}
	return "-"
}

func metadataValueDefault(metadata map[string]any, key string, fallback string) string {
	if value := metadataValue(metadata, key); value != "" {
		return value
	}
	return fallback
}

func emptyDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func safeDiscordURLField(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if len(value) <= 900 {
		return value
	}
	return truncateDiscord(value, 900)
}

func truncateDiscord(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}
