package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"codex-companion/internal/account"
)

const tokenURL = "https://auth.openai.com/oauth/token"
const clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

// tokenResponse is response from refresh token exchange.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// ExchangeRefreshToken exchanges a refresh token for an access token.
func ExchangeRefreshToken(ctx context.Context, rt string) (string, time.Duration, error) {
	payload := map[string]string{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": rt,
		"scope":         "openid profile email",
	}
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(buf))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("unexpected status: %s", resp.Status)
	}
	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", 0, err
	}
	return tr.AccessToken, time.Duration(tr.ExpiresIn) * time.Second, nil
}

// Refresh updates access token if it's expiring soon.
func Refresh(ctx context.Context, mgr *account.Manager, a *account.Account) error {
	if a.Type != account.ChatGPTAccount {
		return nil
	}
	if time.Until(a.TokenExpiresAt) > time.Minute {
		return nil
	}
	token, expiresIn, err := ExchangeRefreshToken(ctx, a.RefreshToken)
	if err != nil {
		return err
	}
	a.AccessToken = token
	a.TokenExpiresAt = time.Now().Add(expiresIn)
	return mgr.Update(ctx, a)
}
