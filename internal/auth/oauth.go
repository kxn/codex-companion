package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"codex-companion/internal/account"
	"codex-companion/internal/logger"
)

const tokenURL = "https://auth.openai.com/oauth/token"
const clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// tokenResponse is response from refresh token exchange.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeRefreshToken exchanges a refresh token for an access token and
// returns the new refresh token if rotation occurs.
func ExchangeRefreshToken(ctx context.Context, rt string) (string, string, time.Duration, error) {
	payload := map[string]string{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": rt,
		"scope":         "openid profile email",
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(buf))
	if err != nil {
		logger.Errorf("new token request: %v", err)
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Errorf("token request failed: %v", err)
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		logger.Errorf("token request unexpected status: %s", resp.Status)
		return "", "", 0, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		logger.Errorf("decode token response: %v", err)
		return "", "", 0, err
	}
	return tr.AccessToken, tr.RefreshToken, time.Duration(tr.ExpiresIn) * time.Second, nil
}

// Refresh updates access token if it's expiring soon.
func Refresh(ctx context.Context, mgr *account.Manager, a *account.Account) error {
	if a.Type != account.ChatGPTAccount {
		return nil
	}
	if time.Until(a.TokenExpiresAt) > time.Minute {
		return nil
	}
	token, rt, expiresIn, err := ExchangeRefreshToken(ctx, a.RefreshToken)
	if err != nil {
		logger.Errorf("exchange refresh token failed: %v", err)
		return err
	}
	a.AccessToken = token
	if rt != "" {
		a.RefreshToken = rt
	}
	a.TokenExpiresAt = time.Now().Add(expiresIn)
	return mgr.Update(ctx, a)
}
