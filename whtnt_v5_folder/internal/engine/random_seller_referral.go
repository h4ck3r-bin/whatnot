package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/logger"
)

const randomSellerReferralCode = "RANDOM_SELLER"
const randomSellerReferralPages = 100
const randomSellerFeedQuery = `query GetFeed($feedId:ID!$objectSize:Int$objectCursor:String$filters:[FilterInput]$sort:SortInput$withFilterAndSortOptions:Boolean!=false){feed:getFeedFromOnboardingOption(id:$feedId filters:$filters sort:$sort){id title filterAndSortOptions@include(if:$withFilterAndSortOptions){__typename}objects(first:$objectSize after:$objectCursor){edges{node{__typename ... on FeedEntity{object{__typename ... on LiveStream{id status categories user{id username __typename}} ... on PublicUserNode{id username __typename}}}}}pageInfo{endCursor hasNextPage __typename}__typename}__typename}}`

func isRandomSellerReferral(code string) bool {
	return strings.EqualFold(strings.TrimSpace(code), randomSellerReferralCode)
}

func (e *Engine) resolveRandomSellerReferral(ctx context.Context, excluded map[string]bool) (string, error) {
	usernames, err := e.randomSellerFeedUsernames(ctx, randomSellerReferralPages)
	if err != nil {
		return "", err
	}
	filtered := make([]string, 0, len(usernames))
	seen := map[string]bool{}
	for _, username := range usernames {
		username = strings.TrimSpace(username)
		key := strings.ToLower(username)
		if username == "" || key == strings.ToLower(randomSellerReferralCode) || seen[key] {
			continue
		}
		if excluded != nil && excluded[key] {
			continue
		}
		seen[key] = true
		filtered = append(filtered, username)
	}
	if len(filtered) == 0 {
		return "", fmt.Errorf("GetFeed returned no usable random sellers")
	}
	selected := filtered[rand.Intn(len(filtered))]
	logger.Manager(fmt.Sprintf("Using RANDOM_SELLER referral: %s.", selected))
	return selected, nil
}

func (e *Engine) randomSellerFeedUsernames(ctx context.Context, maxPages int) ([]string, error) {
	feedID := strings.TrimSpace(e.cfg.Engine.ReviewSellerFeedID)
	if feedID == "" {
		feedID = "CATEGORY_FEED_V2:TGl2ZXN0cmVhbVRhZ05vZGU6ODk5"
	}
	objectSize := e.cfg.Engine.ReviewSellerFeedObjectSize
	if objectSize <= 0 {
		objectSize = 120
	}
	if maxPages <= 0 {
		maxPages = randomSellerReferralPages
	}

	client := &http.Client{Timeout: 20 * time.Second}
	usernames := make([]string, 0, objectSize*minInt(maxPages, 10))
	seen := map[string]bool{}
	var cursor any
	for page := 0; page < maxPages; page++ {
		payload, nextCursor, hasNextPage, err := e.randomSellerFeedPage(ctx, client, feedID, cursor, objectSize)
		if err != nil {
			return nil, err
		}
		collectRandomSellerUsernames(payload, seen, &usernames)
		if !hasNextPage || strings.TrimSpace(nextCursor) == "" {
			break
		}
		cursor = nextCursor
	}
	return usernames, nil
}

func (e *Engine) randomSellerFeedPage(ctx context.Context, client *http.Client, feedID string, cursor any, objectSize int) (map[string]any, string, bool, error) {
	body, err := json.Marshal(map[string]any{
		"operationName": "GetFeed",
		"variables": map[string]any{
			"withFilterAndSortOptions": true,
			"feedId":                   feedID,
			"filters":                  nil,
			"objectCursor":             cursor,
			"objectSize":               objectSize,
			"sort":                     nil,
		},
		"query": randomSellerFeedQuery,
	})
	if err != nil {
		return nil, "", false, fmt.Errorf("marshal random seller GetFeed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.whatnot.com/services/graphql/?operationName=GetFeed&ssr=0", bytes.NewReader(body))
	if err != nil {
		return nil, "", false, err
	}
	e.applyRandomSellerFeedHeaders(req)

	res, err := client.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("random seller GetFeed request failed: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, "", false, fmt.Errorf("read random seller GetFeed response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", false, fmt.Errorf("random seller GetFeed returned %s: %s", res.Status, shortEngineResponse(string(raw)))
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", false, fmt.Errorf("parse random seller GetFeed response: %w", err)
	}
	objects := engineNestedMap(payload, "data", "feed", "objects")
	pageInfo := engineNestedMap(objects, "pageInfo")
	nextCursor := engineStringValue(pageInfo, "endCursor")
	hasNextPage, _ := pageInfo["hasNextPage"].(bool)
	return payload, nextCursor, hasNextPage, nil
}

func (e *Engine) applyRandomSellerFeedHeaders(req *http.Request) {
	ua := strings.TrimSpace(e.cfg.AppVersions.BrowserUserAgent)
	if ua == "" {
		ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
	}
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-language", "en-GB,en;q=0.9")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("origin", "https://www.whatnot.com")
	req.Header.Set("referer", "https://www.whatnot.com/category/pokemon")
	req.Header.Set("user-agent", ua)
	if secCHUA := strings.TrimSpace(e.cfg.AppVersions.BrowserSecChUa); secCHUA != "" {
		req.Header.Set("sec-ch-ua", secCHUA)
	}
	if platform := strings.TrimSpace(e.cfg.AppVersions.BrowserSecChUaPlatform); platform != "" {
		req.Header.Set("sec-ch-ua-platform", platform)
	}
	if appVersion := strings.TrimSpace(e.cfg.AppVersions.WebAppVersion); appVersion != "" {
		req.Header.Set("x-whatnot-app-version", appVersion)
	}
	req.Header.Set("x-whatnot-app", "whatnot-web")
	req.Header.Set("x-whatnot-app-context", "next-js/browser")
}

func collectRandomSellerUsernames(value any, seen map[string]bool, out *[]string) {
	switch typed := value.(type) {
	case map[string]any:
		if username := strings.TrimSpace(engineStringValue(typed, "username")); username != "" {
			key := strings.ToLower(username)
			if !seen[key] {
				seen[key] = true
				*out = append(*out, username)
			}
		}
		for _, child := range typed {
			collectRandomSellerUsernames(child, seen, out)
		}
	case []any:
		for _, child := range typed {
			collectRandomSellerUsernames(child, seen, out)
		}
	}
}

func engineNestedMap(value map[string]any, path ...string) map[string]any {
	current := value
	for _, key := range path {
		next, _ := current[key].(map[string]any)
		if next == nil {
			return map[string]any{}
		}
		current = next
	}
	return current
}

func engineStringValue(value map[string]any, key string) string {
	raw, ok := value[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func shortEngineResponse(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 500 {
		return raw
	}
	return raw[:500] + "..."
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
