// Package tvdb is the client for TheTVDB's v4 API. It implements service.CatalogSource.
package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/StreamNext/Backend/internal/service"
)

const (
	defaultBaseURL = "https://api4.thetvdb.com/v4"
	// A login token lasts a month; renew well before it can expire.
	tokenLifetime = 25 * 24 * time.Hour
	maxBody       = 32 << 20
)

type Config struct {
	APIKey  string // empty disables the client: every call answers service.ErrSourceDisabled
	BaseURL string // empty means TheTVDB's real API; tests and the CI stub point it elsewhere
}

type Client struct {
	apiKey, baseURL string
	http            *http.Client
	now             func() time.Time

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

func New(cfg Config, h *http.Client, now func() time.Time) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	return &Client{apiKey: cfg.APIKey, baseURL: base, http: h, now: now}
}

// Raw returns the body of a GET, with the same login, retry and error handling as every other call. The
// fixture recorder uses it to save real responses.
func (c *Client) Raw(ctx context.Context, path string) ([]byte, error) {
	if c.apiKey == "" {
		return nil, service.ErrSourceDisabled
	}
	body, err := c.do(ctx, path, false)
	if errors.Is(err, errUnauthorized) { // the token was rejected: log in again, once
		body, err = c.do(ctx, path, true)
	}
	if errors.Is(err, errUnauthorized) {
		return nil, fmt.Errorf("%w: TheTVDB rejected our login", service.ErrSourceUnavailable)
	}
	return body, err
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	body, err := c.Raw(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: unreadable response for %s: %v", service.ErrSourceUnavailable, path, err)
	}
	return nil
}

var errUnauthorized = errors.New("unauthorized")

func (c *Client) do(ctx context.Context, path string, renew bool) ([]byte, error) {
	token, err := c.authToken(ctx, renew)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", service.ErrSourceUnavailable, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	return c.send(req, path)
}

func (c *Client) send(req *http.Request, what string) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", service.ErrSourceUnavailable, what, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errUnauthorized
	case resp.StatusCode == http.StatusNotFound:
		return nil, service.ErrSourceNotFound
	case resp.StatusCode != http.StatusOK:
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("%w: %s answered %d: %s", service.ErrSourceUnavailable, what, resp.StatusCode, bytes.TrimSpace(detail))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%w: reading %s: %v", service.ErrSourceUnavailable, what, err)
	}
	return body, nil
}

// authToken returns the login token, logging in first if there is none, it is old, or renew is set.
func (c *Client) authToken(ctx context.Context, renew bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !renew && c.token != "" && c.now().Sub(c.tokenAt) < tokenLifetime {
		return c.token, nil
	}
	payload, _ := json.Marshal(map[string]string{"apikey": c.apiKey})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/login", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("%w: %v", service.ErrSourceUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := c.send(req, "login")
	if errors.Is(err, errUnauthorized) {
		return "", fmt.Errorf("%w: TheTVDB rejected the API key", service.ErrSourceUnavailable)
	}
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Data.Token == "" {
		return "", fmt.Errorf("%w: login returned no token", service.ErrSourceUnavailable)
	}
	c.token, c.tokenAt = out.Data.Token, c.now()
	return c.token, nil
}
