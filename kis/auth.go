package kis

import "context"

type OAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	ExpiresAt   string `json:"access_token_token_expired"`
}
type ApprovalKey struct {
	ApprovalKey string `json:"approval_key"`
}

type oauthRequest struct {
	GrantType string `json:"grant_type"`
	AppKey    string `json:"appkey"`
	AppSecret string `json:"appsecret"`
}

type approvalRequest struct {
	GrantType string `json:"grant_type"`
	AppKey    string `json:"appkey"`
	SecretKey string `json:"secretkey"`
}

func (c *Client) IssueToken(ctx context.Context) (OAuthToken, error) {
	var result OAuthToken
	err := c.do(ctx, "POST", "/oauth2/tokenP", "", nil, mustJSON(oauthRequest{GrantType: "client_credentials", AppKey: c.appKey, AppSecret: c.appSecret}), "", &result)
	if err != nil {
		return OAuthToken{}, err
	}
	if result.AccessToken == "" {
		return OAuthToken{}, &APIError{Message: "empty access token"}
	}
	return result, nil
}
func (c *Client) IssueApprovalKey(ctx context.Context) (ApprovalKey, error) {
	var result ApprovalKey
	err := c.do(ctx, "POST", "/oauth2/Approval", "", nil, mustJSON(approvalRequest{GrantType: "client_credentials", AppKey: c.appKey, SecretKey: c.appSecret}), "", &result)
	return result, err
}
