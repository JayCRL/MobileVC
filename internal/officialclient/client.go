package officialclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mobilevc/internal/logx"
)

const nodeIDFile = ".mobilevc/node_id"

type Client struct {
	ServerURL    string
	AccessToken  string
	RefreshToken string
	NodeID       string
	NodeName     string
	NodeVersion  string
	StunHost     string
	TurnPort     string
	TurnUser     string
	TurnPass     string
	httpClient   *http.Client
	cancel       context.CancelFunc
}

type NodeInfo struct {
	NodeID   string `json:"nodeId"`
	Name     string `json:"name"`
	Version  string `json:"version"`
	StunHost string `json:"stunHost"`
	TurnPort string `json:"turnPort"`
	TurnUser string `json:"turnUser"`
	TurnPass string `json:"turnPass"`
}

func NewClient(serverURL, accessToken, refreshToken, nodeID, nodeName, version, stunHost, turnPort, turnUser, turnPass string) *Client {
	// Load persisted node ID if available
	if nodeID == "" {
		nodeID = loadNodeID()
	}
	return &Client{
		ServerURL:    serverURL,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		NodeID:       nodeID,
		NodeName:     nodeName,
		NodeVersion:  version,
		StunHost:     stunHost,
		TurnPort:     turnPort,
		TurnUser:     turnUser,
		TurnPass:     turnPass,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Register(ctx context.Context) error {
	body := NodeInfo{
		NodeID:   c.NodeID,
		Name:     c.NodeName,
		Version:  c.NodeVersion,
		StunHost: c.StunHost,
		TurnPort: c.TurnPort,
		TurnUser: c.TurnUser,
		TurnPass: c.TurnPass,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		c.ServerURL+"/api/nodes", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && c.RefreshToken != "" {
		if err := c.refreshAccessToken(ctx); err != nil {
			return fmt.Errorf("token refresh failed: %w", err)
		}
		return c.Register(ctx)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register node failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err == nil && result.Node.ID != "" {
		c.NodeID = result.Node.ID
		saveNodeID(c.NodeID)
	}

	logx.Info("official", "node registered: id=%s name=%s", c.NodeID, c.NodeName)
	return nil
}

func (c *Client) Deregister(ctx context.Context) error {
	if c.NodeID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, "DELETE",
		c.ServerURL+"/api/nodes/"+c.NodeID, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deregister node: %w", err)
	}
	defer resp.Body.Close()

	logx.Info("official", "node deregistered: id=%s", c.NodeID)
	return nil
}

func (c *Client) Heartbeat(ctx context.Context) error {
	if c.NodeID == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]string{"nodeId": c.NodeID})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.ServerURL+"/api/nodes/heartbeat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && c.RefreshToken != "" {
		if err := c.refreshAccessToken(ctx); err != nil {
			return err
		}
		return c.Heartbeat(ctx)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat failed %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) refreshAccessToken(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{"refreshToken": c.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.ServerURL+"/api/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	c.AccessToken = result.AccessToken
	c.RefreshToken = result.RefreshToken
	logx.Info("official", "access token refreshed")
	return nil
}

func (c *Client) StartHeartbeatLoop(ctx context.Context, interval time.Duration) {
	ctx, c.cancel = context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := c.Heartbeat(ctx); err != nil {
					logx.Error("official", "heartbeat error: %v", err)
				}
			}
		}
	}()
}

func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

func ResolveNodeName() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// Persist / load node ID across restarts.
func nodeIDPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, nodeIDFile)
}

func loadNodeID() string {
	data, err := os.ReadFile(nodeIDPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func saveNodeID(id string) {
	os.WriteFile(nodeIDPath(), []byte(id), 0o600)
}

// Credentials for official server login.
const credsFile = ".mobilevc/credentials.json"

type Credentials struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func credsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, credsFile)
}

func SaveCredentials(accessToken, refreshToken string) {
	creds := Credentials{AccessToken: accessToken, RefreshToken: refreshToken}
	data, err := json.Marshal(creds)
	if err != nil {
		return
	}
	os.WriteFile(credsPath(), data, 0o600)
}

func LoadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(credsPath())
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func ClearCredentials() {
	os.Remove(credsPath())
}

// LoginResult contains tokens returned from email/password login.
type LoginResult struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	UserID       string
}

// LoginWithEmail authenticates with the official server using email/password.
func LoginWithEmail(ctx context.Context, serverURL, email, password string) (*LoginResult, error) {
	body, _ := json.Marshal(map[string]string{
		"email":    email,
		"password": password,
	})
	req, err := http.NewRequestWithContext(ctx, "POST",
		serverURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("login failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
		User         struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("login response decode: %w", err)
	}

	logx.Info("official", "login successful: user_id=%s", result.User.ID)
	return &LoginResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		UserID:       result.User.ID,
	}, nil
}
