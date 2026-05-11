package auth

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GitHubOAuth struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	httpClient   *http.Client
}

type GitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

type GitHubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func NewGitHubOAuth(clientID, clientSecret, redirectURI string) *GitHubOAuth {
	return &GitHubOAuth{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				ForceAttemptHTTP2: false,
				MaxIdleConns:      1,
				IdleConnTimeout:   10 * time.Second,
			},
		},
	}
}

func (g *GitHubOAuth) AuthorizeURL(state string) string {
	return "https://github.com/login/oauth/authorize?" +
		"client_id=" + url.QueryEscape(g.ClientID) +
		"&redirect_uri=" + url.QueryEscape(g.RedirectURI) +
		"&state=" + url.QueryEscape(state) +
		"&scope=user:email"
}

func (g *GitHubOAuth) ExchangeCode(code string) (*GitHubUser, error) {
	// Step 1: exchange code for access token
	tokenURL := "https://github.com/login/oauth/access_token"
	body := fmt.Sprintf("client_id=%s&client_secret=%s&code=%s&redirect_uri=%s",
		url.QueryEscape(g.ClientID),
		url.QueryEscape(g.ClientSecret),
		url.QueryEscape(code),
		url.QueryEscape(g.RedirectURI),
	)

	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MobileVC-OfficialServer/1.0")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("github oauth error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("empty access token from github")
	}

	// Step 2: fetch user info
	ghUser, err := g.fetchUser(tokenResp.AccessToken)
	if err != nil {
		return nil, err
	}

	// Step 3: fetch primary email if not returned in user
	if ghUser.Email == "" {
		ghUser.Email, _ = g.fetchPrimaryEmail(tokenResp.AccessToken)
	}

	return ghUser, nil
}

func (g *GitHubOAuth) fetchUser(accessToken string) (*GitHubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error %d: %s", resp.StatusCode, string(body))
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &user, nil
}

func (g *GitHubOAuth) fetchPrimaryEmail(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var emails []GitHubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	// fallback: first verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}
	return "", nil
}
