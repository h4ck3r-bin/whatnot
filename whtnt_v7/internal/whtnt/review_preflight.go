package whtnt

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"rnldo.dev/wnshell/internal/appconfig"
)

func (m *Module) PrepareReviewSubmission() {
	if m == nil {
		return
	}
	m.reviewPreflightUsername = ""
	m.reviewPreflightFollowed = 0
	m.sleepDelay("before_review_username_update")
	if err := m.updateReviewUsername(); err != nil {
		m.log.ManagerNotice("Review username preflight skipped: " + err.Error())
	}
	if err := m.followRandomReviewSellers(); err != nil {
		m.log.ManagerNotice("Review seller follow preflight skipped: " + err.Error())
	}
}

func (m *Module) updateReviewUsername() error {
	if err := m.syncCurrentAccountUsername("review username preflight"); err != nil {
		return err
	}
	if checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName) {
		m.usernameAdequate = true
		m.usernameAdequateCheckedAt = time.Now().UTC()
		m.autosaveSessionIfChanged("review username already adequate")
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
		if available, err := m.usernameAvailableMobile(username); err == nil && !available {
			lastErr = fmt.Errorf("username %s is already taken", username)
			continue
		}
		m.log.Manager(fmt.Sprintf("Review username preflight: updating username to %s.", username))
		if err := m.updateUsernameMobile(username); err != nil {
			lastErr = err
			if isUsernameCooldownError(err) {
				break
			}
			continue
		}
		m.log.Manager("Review username preflight complete.")
		m.reviewPreflightUsername = username
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no username attempts were made")
	}
	return lastErr
}

func (m *Module) updateUsernameMobile(username string) error {
	if err := m.updateUsernameMobileRaw(username); err != nil {
		return err
	}
	m.sleepDelay("after_review_username_update")
	return nil
}

func (m *Module) updateUsernameMobileRaw(username string) error {
	res, err := m.sendMobileCheckoutRequest("UpdateUsername", map[string]any{
		"username": username,
		"surface":  userUpdateSurface,
	}, "mutation UpdateUsername($username:String!$surface:UserUpdateSurface){updateUsername(username:$username surface:$surface){user{id username __typename}__typename}}")
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
		return m.confirmUsernameAfterAmbiguousUpdate(username, fmt.Errorf("parse update username response: %w", err))
	}
	if len(dto.Errors) > 0 {
		return m.confirmUsernameAfterAmbiguousUpdate(username, fmt.Errorf("update username returned errors: %s", shortResponse(res.String())))
	}
	if !strings.EqualFold(strings.TrimSpace(dto.Data.UpdateUsername.User.Username), username) {
		return m.confirmUsernameAfterAmbiguousUpdate(username, fmt.Errorf("update username did not confirm %s", username))
	}
	m.setConfirmedAccountUsername(username, "UpdateUsername")
	return nil
}

func (m *Module) syncCurrentAccountUsername(reason string) error {
	username, firstName, lastName, err := m.currentAccountIdentityMobile()
	if err != nil {
		return fmt.Errorf("check current username: %w", err)
	}
	changed := false
	if strings.TrimSpace(username) != "" && !strings.EqualFold(strings.TrimSpace(m.account.username), username) {
		m.account.username = strings.TrimSpace(username)
		changed = true
	}
	if strings.TrimSpace(firstName) != "" && !strings.EqualFold(strings.TrimSpace(m.account.firstName), firstName) {
		m.account.firstName = strings.TrimSpace(firstName)
		changed = true
	}
	if strings.TrimSpace(lastName) != "" && !strings.EqualFold(strings.TrimSpace(m.account.lastName), lastName) {
		m.account.lastName = strings.TrimSpace(lastName)
		changed = true
	}
	m.usernameAdequate = checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName)
	m.usernameAdequateCheckedAt = time.Now().UTC()
	if changed {
		m.autosaveSessionIfChanged(reason)
	}
	return nil
}

func (m *Module) currentAccountIdentityMobile() (string, string, string, error) {
	res, err := m.sendMobileCheckoutRequest(
		"CurrentUser",
		nil,
		`query CurrentUser @attribution(owner: "app-platform") { me { __typename id username firstName lastName } }`,
	)
	if err != nil {
		return "", "", "", err
	}
	var payload struct {
		Errors any `json:"errors"`
		Data   struct {
			Me struct {
				Username  string `json:"username"`
				FirstName string `json:"firstName"`
				LastName  string `json:"lastName"`
			} `json:"me"`
		} `json:"data"`
	}
	if err := res.JSON(&payload); err != nil {
		return "", "", "", err
	}
	if payload.Errors != nil {
		return "", "", "", fmt.Errorf("CurrentUser graphql errors: %s", shortResponse(res.String()))
	}
	username := strings.TrimSpace(payload.Data.Me.Username)
	if username == "" {
		return "", "", "", fmt.Errorf("CurrentUser returned empty username")
	}
	return username, strings.TrimSpace(payload.Data.Me.FirstName), strings.TrimSpace(payload.Data.Me.LastName), nil
}

func (m *Module) confirmUsernameAfterAmbiguousUpdate(username string, cause error) error {
	liveUsername, firstName, lastName, err := m.currentAccountIdentityMobile()
	if err == nil && strings.EqualFold(strings.TrimSpace(liveUsername), strings.TrimSpace(username)) {
		if strings.TrimSpace(firstName) != "" {
			m.account.firstName = strings.TrimSpace(firstName)
		}
		if strings.TrimSpace(lastName) != "" {
			m.account.lastName = strings.TrimSpace(lastName)
		}
		m.setConfirmedAccountUsername(username, "UpdateUsername verified")
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w; username verification failed: %s", cause, err.Error())
	}
	if strings.TrimSpace(liveUsername) != "" && !strings.EqualFold(strings.TrimSpace(m.account.username), liveUsername) {
		m.account.username = strings.TrimSpace(liveUsername)
		m.usernameAdequate = checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName)
		m.usernameAdequateCheckedAt = time.Now().UTC()
		m.autosaveSessionIfChanged("UpdateUsername verification mismatch")
	}
	return cause
}

func (m *Module) setConfirmedAccountUsername(username string, reason string) {
	m.account.username = strings.TrimSpace(username)
	m.usernameAdequate = checkoutUsernameAdequate(m.account.username, m.account.firstName, m.account.lastName)
	m.usernameAdequateCheckedAt = time.Now().UTC()
	m.autosaveSessionIfChanged(reason)
}

func (m *Module) followRandomReviewSellers() error {
	cfg := m.config
	if cfg == nil {
		loaded, err := appconfig.Load()
		if err != nil {
			return err
		}
		cfg = loaded
		m.config = loaded
	}
	minFollows := cfg.Engine.ReviewPreflightMinFollows
	maxFollows := cfg.Engine.ReviewPreflightMaxFollows
	if minFollows <= 0 || maxFollows <= 0 {
		return nil
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
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("GetFeed returned no seller IDs")
	}
	if len(ids) > target {
		ids = ids[:target]
	}
	m.log.Manager(fmt.Sprintf("Review follow preflight: following %d seller(s).", len(ids)))
	if err := m.bulkFollowReviewSellerIDs(ids); err != nil {
		return err
	}
	m.reviewPreflightFollowed = len(ids)
	m.log.Manager("Review follow preflight complete.")
	return nil
}

const getFeedSellerQuery = `query GetFeed($feedId:ID!$objectSize:Int$objectCursor:String$filters:[FilterInput]$sort:SortInput$withFilterAndSortOptions:Boolean!=false){feed:getFeedFromOnboardingOption(id:$feedId filters:$filters sort:$sort){id title filterAndSortOptions@include(if:$withFilterAndSortOptions){__typename}objects(first:$objectSize after:$objectCursor){edges{node{__typename ... on FeedEntity{object{__typename ... on LiveStream{id status livestreamCategories{id label __typename}categories user{id username __typename}} ... on PublicUserNode{id username __typename}}}}}pageInfo{endCursor hasNextPage __typename}__typename}__typename}}`

func (m *Module) reviewSellerEntityIDs(cfg *appconfig.Config, limit int) ([]string, error) {
	ids, err := m.reviewSellerEntityIDsWithFilter(cfg, limit, true)
	if err != nil {
		return nil, err
	}
	if len(ids) < limit && strings.TrimSpace(cfg.Engine.ReviewSellerFeedFilter) != "" {
		extra, err := m.reviewSellerEntityIDsWithFilter(cfg, limit, false)
		if err != nil {
			return nil, err
		}
		ids = appendUniqueReviewSellerIDs(ids, extra, limit)
	}
	return ids, nil
}

func (m *Module) reviewSellerEntityIDsWithFilter(cfg *appconfig.Config, limit int, useConfiguredFilter bool) ([]string, error) {
	feedID := strings.TrimSpace(cfg.Engine.ReviewSellerFeedID)
	if feedID == "" {
		feedID = "CATEGORY_FEED_V2:TGl2ZXN0cmVhbVRhZ05vZGU6ODk5"
	}
	objectSize := cfg.Engine.ReviewSellerFeedObjectSize
	if objectSize < limit {
		objectSize = limit
	}
	if objectSize <= 0 {
		objectSize = 120
	}
	maxPages := cfg.Engine.ReviewSellerFeedMaxPages
	if maxPages <= 0 {
		maxPages = 15
	}
	var filters any
	if filter := strings.TrimSpace(cfg.Engine.ReviewSellerFeedFilter); useConfiguredFilter && filter != "" {
		filters = []map[string]any{{
			"id":            filter,
			"selectedValue": nil,
		}}
	}
	seen := map[string]bool{}
	ids := make([]string, 0, limit)
	var cursor any
	for page := 0; page < maxPages && len(ids) < limit; page++ {
		payload, nextCursor, hasNextPage, err := m.reviewSellerFeedPage(feedID, filters, cursor, objectSize)
		if err != nil {
			return nil, err
		}
		pageIDs := make([]string, 0, objectSize)
		collectReviewSellerIDs(payload, seen, &pageIDs, objectSize)
		rand.Shuffle(len(pageIDs), func(i, j int) {
			pageIDs[i], pageIDs[j] = pageIDs[j], pageIDs[i]
		})
		ids = appendUniqueReviewSellerIDs(ids, pageIDs, limit)
		m.sleepDelay("after_review_feed_lookup")
		if !hasNextPage || strings.TrimSpace(nextCursor) == "" {
			break
		}
		cursor = nextCursor
	}
	return ids, nil
}

func (m *Module) reviewSellerFeedPage(feedID string, filters any, cursor any, objectSize int) (map[string]any, string, bool, error) {
	res, err := m.sendMobileCheckoutRequest("GetFeed", map[string]any{
		"withFilterAndSortOptions": true,
		"feedId":                   feedID,
		"filters":                  filters,
		"objectCursor":             cursor,
		"objectSize":               objectSize,
		"sort":                     nil,
	}, getFeedSellerQuery)
	if err != nil {
		return nil, "", false, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(res.String()), &payload); err != nil {
		return nil, "", false, err
	}
	objects, _ := nestedMap(payload, "data", "feed", "objects")
	pageInfo, _ := nestedMap(objects, "pageInfo")
	nextCursor := stringValue(pageInfo, "endCursor")
	hasNextPage, _ := pageInfo["hasNextPage"].(bool)
	return payload, nextCursor, hasNextPage, nil
}

func appendUniqueReviewSellerIDs(ids []string, candidates []string, limit int) []string {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			seen[id] = true
		}
	}
	for _, id := range candidates {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) >= limit {
			break
		}
	}
	return ids
}

func (m *Module) bulkFollowReviewSellerIDs(ids []string) error {
	return m.bulkFollowSellerIDsWithDelay(ids, "between_review_follows")
}

func (m *Module) bulkFollowSellerIDsWithDelay(ids []string, delayName string) error {
	for i, id := range ids {
		res, err := m.sendMobileCheckoutRequest("FollowUser", map[string]any{
			"active": true,
			"id":     normalizeReviewSellerUserID(id),
		}, "mutation FollowUser($id:ID!$active:Boolean){followUser(id:$id active:$active){success user{id isFollowing isFollower __typename}__typename}}")
		if err != nil {
			return err
		}
		if err := verifyFollowUserSuccess(res.String()); err != nil {
			return err
		}
		if i < len(ids)-1 {
			m.sleepDelay(delayName)
		}
	}
	return nil
}

func verifyFollowUserSuccess(raw string) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return fmt.Errorf("parse FollowUser response: %w", err)
	}
	if errorsRaw, ok := payload["errors"].([]any); ok && len(errorsRaw) > 0 {
		return fmt.Errorf("FollowUser returned errors: %s", shortResponse(raw))
	}
	result, ok := nestedMap(payload, "data", "followUser")
	if !ok {
		return fmt.Errorf("FollowUser response missing result: %s", shortResponse(raw))
	}
	if success, _ := result["success"].(bool); !success {
		return fmt.Errorf("FollowUser success=false: %s", shortResponse(raw))
	}
	user, ok := result["user"].(map[string]any)
	if !ok {
		return fmt.Errorf("FollowUser response missing user: %s", shortResponse(raw))
	}
	if following, _ := user["isFollowing"].(bool); !following {
		return fmt.Errorf("FollowUser did not confirm isFollowing=true: %s", shortResponse(raw))
	}
	return nil
}

func collectReviewSellerIDs(value any, seen map[string]bool, ids *[]string, limit int) {
	if len(*ids) >= limit {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if strings.EqualFold(stringValue(typed, "__typename"), "PublicUserNode") {
			id := strings.TrimSpace(stringValue(typed, "id"))
			if id != "" && !seen[id] {
				seen[id] = true
				*ids = append(*ids, id)
				if len(*ids) >= limit {
					return
				}
			}
		}
		if user, ok := typed["user"].(map[string]any); ok {
			id := strings.TrimSpace(stringValue(user, "id"))
			if id != "" && !seen[id] {
				seen[id] = true
				*ids = append(*ids, id)
				if len(*ids) >= limit {
					return
				}
			}
		}
		for _, child := range typed {
			collectReviewSellerIDs(child, seen, ids, limit)
			if len(*ids) >= limit {
				return
			}
		}
	case []any:
		for _, child := range typed {
			collectReviewSellerIDs(child, seen, ids, limit)
			if len(*ids) >= limit {
				return
			}
		}
	}
}

func normalizeReviewSellerUserID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return id
}

func isUsernameCooldownError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "changing username is allowed once per day")
}
