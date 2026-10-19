package whtnt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	reviewSessionFileName = "session.json"
	defaultReviewRatings  = `[{"questionId":"1","value":5,"explanation":""},{"questionId":"2","value":5},{"questionId":"3","value":5},{"questionId":"4","value":5}]`
)

type pendingReviewItem struct {
	Listing struct {
		Title string `json:"title"`
	} `json:"listing"`
}

type pendingReview struct {
	ShipmentID     string              `json:"shipmentId"`
	SellerUsername string              `json:"sellerUsername"`
	OrderItems     []pendingReviewItem `json:"orderItems"`
}

func (m *Module) getPendingReviews() ([]pendingReview, error) {
	res, err := m.sendMobileCheckoutRequest(
		"GetPendingReviews",
		map[string]any{"newOnly": true},
		`query GetPendingReviews($newOnly: Boolean!) @attribution(owner: "selling") { getPendingReviews(newOnly: $newOnly) { __typename ...PendingReviewFragment } }
fragment ProfileImage on UserImageNode @attribution(owner: "app-platform") { __typename id bucket key url }
fragment Money on Money @attribution(owner: "app-platform") { __typename amountSafe currency }
fragment PendingReviewFragment on PendingReview @attribution(owner: "selling") { __typename shipmentId sellerUsername seller { __typename id profileImage { __typename ...ProfileImage } } purchasedAt orderItems { __typename id price { __typename ...Money } order { __typename id isReviewPending } listing { __typename id title conditionName images { __typename key bucket url } } } }`,
	)
	if err != nil {
		return nil, fmt.Errorf("get pending reviews: %w", err)
	}

	type pendingReviewsResponse struct {
		Data struct {
			GetPendingReviews []pendingReview `json:"getPendingReviews"`
		} `json:"data"`
	}

	var dto pendingReviewsResponse
	if err := res.JSON(&dto); err != nil {
		return nil, fmt.Errorf("parse pending reviews response: %w", err)
	}

	return dto.Data.GetPendingReviews, nil
}

func (m *Module) submitReviewWithRatings(shipmentID string, text string, ratings []int) (ReviewSubmitSnapshot, error) {
	checkpointKey := "review:SubmitReview:" + strings.TrimSpace(shipmentID)
	if m.checkpointDone(checkpointKey) {
		m.log.Review("Review already submitted for shipment checkpoint.")
		return m.reviewSubmitSnapshot(), nil
	}

	m.PrepareReviewSubmission()
	m.sleepDelay("before_review_submit")

	ratingsJSON := defaultReviewRatings
	ratingValues := normalizeReviewRatings(ratings)
	if strings.TrimSpace(text) != "" || len(ratings) > 0 {
		payload := make([]map[string]any, 0, 4)
		for i := 0; i < 4; i++ {
			item := map[string]any{
				"questionId": fmt.Sprintf("%d", i+1),
				"value":      ratingValues[i],
			}
			if i == 0 {
				item["explanation"] = strings.TrimSpace(text)
			}
			payload = append(payload, item)
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return ReviewSubmitSnapshot{}, err
		}
		ratingsJSON = string(encoded)
	}

	res, err := m.sendMobileCheckoutRequest(
		"SubmitReview",
		map[string]any{
			"shipmentId": shipmentID,
			"ratings":    ratingsJSON,
		},
		`mutation SubmitReview($shipmentId: ID!, $ratings: JSONString!) @attribution(owner: "selling") { submitReview(shipmentId: $shipmentId, ratings: $ratings) { __typename success shipment { __typename id orderItems { __typename id order { __typename id isReviewPending prettyStatus } } } } }`,
	)
	if err != nil {
		return ReviewSubmitSnapshot{}, fmt.Errorf("submit review: %w", err)
	}

	type submitReviewResponse struct {
		Data struct {
			SubmitReview struct {
				Success bool `json:"success"`
			} `json:"submitReview"`
		} `json:"data"`
	}

	var dto submitReviewResponse
	if err := res.JSON(&dto); err != nil {
		return ReviewSubmitSnapshot{}, fmt.Errorf("parse submit review response: %w", err)
	}
	if !dto.Data.SubmitReview.Success {
		return ReviewSubmitSnapshot{}, fmt.Errorf("submit review was not successful for shipment %s: %s", shipmentID, m.describeSubmitReviewFailure(shipmentID, res.String()))
	}

	m.markCheckpointDone(checkpointKey)
	m.log.Review("Review submitted successfully.")
	m.sleepDelay("after_review_submit")
	return m.reviewSubmitSnapshot(), nil
}

func (m *Module) reviewSubmitSnapshot() ReviewSubmitSnapshot {
	snapshot := ReviewSubmitSnapshot{
		NewUsername:      strings.TrimSpace(m.reviewPreflightUsername),
		AccountsFollowed: m.reviewPreflightFollowed,
	}
	if sessionSnapshot, err := m.SessionSnapshot(); err == nil {
		snapshot.Session = sessionSnapshot
	}
	return snapshot
}

func (m *Module) describeSubmitReviewFailure(shipmentID string, body string) string {
	pending, err := m.getPendingReviews()
	if err != nil {
		return shortResponse(body) + "; pending-review recheck failed: " + err.Error()
	}
	for _, review := range pending {
		if strings.TrimSpace(review.ShipmentID) == strings.TrimSpace(shipmentID) {
			return shortResponse(body) + "; shipment is still pending review, so Whatnot rejected the submitted rating/text payload without a public error"
		}
	}
	return shortResponse(body) + "; shipment is no longer returned by GetPendingReviews"
}

func normalizeReviewRatings(ratings []int) []int {
	out := []int{5, 5, 5, 5}
	for i := 0; i < len(out) && i < len(ratings); i++ {
		value := ratings[i]
		if value < 1 {
			value = 1
		}
		if value > 5 {
			value = 5
		}
		out[i] = value
	}
	return out
}

func extractJWTClaim(token, claim string) string {
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

	value, ok := claims[claim].(string)
	if !ok {
		return ""
	}

	return value
}

func normalizeCookiePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	return path
}
