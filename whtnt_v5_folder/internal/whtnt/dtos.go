package whtnt

type SuccessLoginResponse struct {
	UserID      int `json:"user_id"`
	AccessToken struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	} `json:"access_token"`
	RefreshToken struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	} `json:"refresh_token"`
	Appsid string `json:"appsid"`
}

type referralMoneyDto struct {
	Typename   string `json:"__typename"`
	AmountSafe int    `json:"amountSafe"`
	Currency   string `json:"currency"`
}

type revealReferralCreditDto struct {
	Data struct {
		RevealReferralCredit *struct {
			Typename string `json:"__typename"`
			Error    string `json:"error"`
			Reward   *struct {
				Typename string `json:"__typename"`
				Amount   any    `json:"amount"`
				Currency string `json:"currency"`
			} `json:"reward"`
		} `json:"revealReferralCredit"`
	} `json:"data"`
}
