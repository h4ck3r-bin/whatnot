package whtnt

import (
	"fmt"
	"strings"
)

const getUserIDByUsernameQuery = `query GetUserId($username:String,$excludeBanned:Boolean) @attribution(owner: "app-platform") { getUser(username:$username,excludeBanned:$excludeBanned){__typename id username} }`

type usernameAvailabilityResponse struct {
	Errors []any `json:"errors"`
	Data   struct {
		GetUser *struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"getUser"`
	} `json:"data"`
}

func (m *Module) usernameAvailableWeb(username string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("empty username")
	}
	res, err := m.sendGqlRequest("GetUserId", gqlBody[any]{
		OperationName: "GetUserId",
		Variables: map[string]any{
			"username":      username,
			"excludeBanned": true,
		},
		Query: getUserIDByUsernameQuery,
		Extensions: map[string]any{
			"clientLibrary": m.referralClientLibrary(),
		},
	})
	if err != nil {
		return false, err
	}
	var dto usernameAvailabilityResponse
	if err := res.JSON(&dto); err != nil {
		return false, err
	}
	if len(dto.Errors) > 0 {
		return false, fmt.Errorf("username lookup returned errors: %s", shortResponse(res.String()))
	}
	return dto.Data.GetUser == nil, nil
}

func (m *Module) usernameAvailableMobile(username string) (bool, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return false, fmt.Errorf("empty username")
	}
	res, err := m.sendMobileCheckoutRequest("GetUserId", map[string]any{
		"username":      username,
		"excludeBanned": true,
	}, getUserIDByUsernameQuery)
	if err != nil {
		return false, err
	}
	var dto usernameAvailabilityResponse
	if err := res.JSON(&dto); err != nil {
		return false, err
	}
	if len(dto.Errors) > 0 {
		return false, fmt.Errorf("username lookup returned errors: %s", shortResponse(res.String()))
	}
	return dto.Data.GetUser == nil, nil
}
