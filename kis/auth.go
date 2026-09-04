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

// IssueToken explicitly obtains an OAuth token; Read automatically caches it
// when no TokenProvider was configured.
func (c *Client) IssueToken(ctx context.Context) (OAuthToken, error) { return c.issueToken(ctx) }
func (c *Client) issueToken(ctx context.Context) (OAuthToken, error) {
	var result OAuthToken
	err := c.send(ctx, "POST", "/oauth2/tokenP", "", nil, mustJSON(oauthRequest{"client_credentials", c.appKey, c.appSecret}), false, &result)
	if err != nil || result.AccessToken == "" {
		return OAuthToken{}, tokenError(err)
	}
	return result, nil
}
func tokenError(err error) error {
	if err != nil {
		return err
	}
	return &APIError{Message: "token response invalid"}
}

// IssueApprovalKey is a dedicated authentication-protocol POST, not an order API.
func (c *Client) IssueApprovalKey(ctx context.Context) (ApprovalKey, error) {
	var result ApprovalKey
	err := c.send(ctx, "POST", "/oauth2/Approval", "", nil, mustJSON(approvalRequest{"client_credentials", c.appKey, c.appSecret}), false, &result)
	if err != nil || result.ApprovalKey == "" {
		return ApprovalKey{}, tokenError(err)
	}
	return result, nil
}
