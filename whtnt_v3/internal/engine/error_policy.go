package engine

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

func shouldSendTaskErrorWebhook(err error) bool {
	if err == nil {
		return false
	}
	if isCheckoutErrorWebhookWorthy(err) {
		return true
	}
	if isTerminalTaskError(err) {
		return true
	}
	if isNoisyOperatorError(err) {
		return false
	}
	return true
}

func shouldSendErrorWebhookNotification(title string, err error) bool {
	if err == nil {
		return false
	}
	if isCheckoutErrorWebhookWorthy(err) {
		return true
	}
	if isTerminalTaskError(err) {
		return true
	}
	if strings.Contains(strings.ToLower(title), "webhook failed") {
		return true
	}
	return !isNoisyOperatorError(err)
}

func taskErrorWebhookTitle(err error) string {
	switch checkoutErrorCategory(err) {
	case "card_declined":
		return "Card declined"
	case "product_unavailable":
		return "Product unavailable"
	case "price_changed":
		return "Product price changed"
	case "proxy_dead":
		return "Proxy/network error"
	case "checkout":
		return "Checkout error"
	}
	if isTerminalTaskError(err) {
		return "Run stopped"
	}
	return "Task failed"
}

func isCheckoutErrorWebhookWorthy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	checkoutSignals := []string{
		"card declined",
		"placeorder",
		"place order",
		"quote cart",
		"quotecart",
		"out of stock",
		"listing out of stock",
		"not enough quantity",
		"product unavailable",
		"listing unavailable",
		"price changed",
		"above maxprice",
		"maxprice is 0",
		"proxy unauthorized",
		"proxy authentication",
		"proxy-authenticate",
		"proxy tunnel",
		"failed to dial",
		"connection reset",
		"no such host",
	}
	for _, value := range checkoutSignals {
		if strings.Contains(message, value) {
			return true
		}
	}
	return isStripeForbiddenError(message)
}

func checkoutErrorCategory(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "card declined"):
		return "card_declined"
	case strings.Contains(message, "out of stock"),
		strings.Contains(message, "not enough quantity"),
		strings.Contains(message, "product unavailable"),
		strings.Contains(message, "listing unavailable"):
		return "product_unavailable"
	case strings.Contains(message, "price changed"),
		strings.Contains(message, "above maxprice"),
		strings.Contains(message, "maxprice is 0"):
		return "price_changed"
	case strings.Contains(message, "proxy tunnel"),
		strings.Contains(message, "proxy unauthorized"),
		strings.Contains(message, "proxy authentication"),
		strings.Contains(message, "proxy-authenticate"),
		strings.Contains(message, "407"),
		strings.Contains(message, "failed to dial"),
		strings.Contains(message, "dial tcp"),
		strings.Contains(message, "connection reset"),
		strings.Contains(message, "no such host"),
		strings.Contains(message, "timeout"):
		return "proxy_dead"
	case isCheckoutErrorWebhookWorthy(err):
		return "checkout"
	default:
		return ""
	}
}

func isTransientSessionRefreshError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	transient := []string{
		"410 gone",
		"proxy tunnel",
		"proxy unauthorized",
		"proxy authentication",
		"proxy-authenticate",
		"407",
		"failed to dial",
		"dial tcp",
		"timeout",
		"context deadline exceeded",
		"tls",
		"dns",
		"no such host",
		"connection reset",
		"connection refused",
		"eof",
		"temporary",
		"recover missing client",
		"client session is not available",
		"session is closed",
		"session closed",
		"use of closed network connection",
	}
	for _, value := range transient {
		if strings.Contains(message, value) {
			return true
		}
	}
	return false
}

func errorWebhookDedupeKey(title string, err error, fields map[string]string) string {
	category := checkoutErrorCategory(err)
	if category == "" {
		category = strings.ToLower(strings.TrimSpace(title))
	}
	parts := []string{category, normalizeErrorForDedupe(err)}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := fields[key]
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "task" || key == "account" || key == "email" {
			continue
		}
		parts = append(parts, key+"="+strings.TrimSpace(value))
	}
	hash := sha1.Sum([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(hash[:])
}

func normalizeErrorForDedupe(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	message = strings.Join(strings.Fields(message), " ")
	if checkoutErrorCategory(err) == "price_changed" {
		return message
	}
	replacements := []string{
		"task ", "account ", "row ",
	}
	for _, prefix := range replacements {
		message = stripDynamicAfter(message, prefix)
	}
	return message
}

func stripDynamicAfter(message string, marker string) string {
	index := strings.Index(message, marker)
	if index < 0 {
		return message
	}
	return message[:index] + marker
}

func isStripeForbiddenError(message string) bool {
	if !strings.Contains(message, "stripe") {
		return false
	}
	return strings.Contains(message, "403") || strings.Contains(message, "forbidden")
}

func isNoisyOperatorError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	noisy := []string{
		"kasada",
		"not found for queued task",
		"card declined",
		"proxy unauthorized",
		"proxy authentication",
		"proxy-authenticate",
		"proxy tunnel",
		"failed to dial",
		"timeout",
		"eof",
		"context canceled",
		"context cancelled",
	}
	for _, value := range noisy {
		if strings.Contains(message, value) {
			return true
		}
	}
	return false
}

func isTerminalTaskError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	terminal := []string{
		"no_balance",
		"insufficient balance",
		"bad_key",
		"invalid api key",
		"unauthorized sms",
		"auth failed",
	}
	for _, value := range terminal {
		if strings.Contains(message, value) {
			return true
		}
	}
	return false
}

func isRunStoppingError(err error) bool {
	return isTerminalTaskError(err)
}
