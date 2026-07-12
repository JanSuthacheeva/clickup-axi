// Package clickup is the driven adapter for the ClickUp API v2: a thin
// HTTP client whose failures are always translated into APIError before
// they can reach agent-facing output.
package clickup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.clickup.com/api/v2"

// ErrNoAuth is the exact message of the missing-token failure; the CLI
// matches on it to render login guidance.
const ErrNoAuth = "not authenticated: CLICKUP_TOKEN is not set and no token is stored"

type Client struct {
	base          string
	token         string
	http          *http.Client
	dateCachePath string
	dateOnce      sync.Once
	dateLocation  *time.Location
}

// New builds a client against an explicit base URL, primarily for
// tests pointing at an httptest fake.
func New(base, token string, h *http.Client) *Client {
	return &Client{base: base, token: token, http: h}
}

func NewFromEnv() *Client {
	c := &Client{
		base:  defaultBaseURL,
		token: resolveToken(),
		http:  &http.Client{Timeout: 30 * time.Second},
	}
	if dir, err := os.UserConfigDir(); err == nil {
		c.dateCachePath = filepath.Join(dir, "clickup-axi", "timezone")
	}
	return c
}

// WithToken returns a client that authenticates with token but shares
// this client's base URL and transport, so a token can be validated
// before it is stored.
func (c *Client) WithToken(token string) *Client {
	return &Client{base: c.base, token: token, http: c.http, dateCachePath: c.dateCachePath}
}

// APIError is a translated ClickUp API failure; raw dependency messages
// never reach stdout directly.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

func (c *Client) do(method, path string, body any, out any) *APIError {
	if c.token == "" {
		return &APIError{Status: 0, Message: ErrNoAuth}
	}
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return &APIError{Status: 0, Message: "could not encode request body"}
		}
		reqBody = bytes.NewReader(b)
	}
	resp, apiErr := c.send(method, path, reqBody)
	if apiErr != nil {
		return apiErr
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return &APIError{Status: resp.StatusCode, Message: "ClickUp returned an unreadable response"}
		}
	}
	return nil
}

func (c *Client) send(method, path string, body io.Reader) (*http.Response, *APIError) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(method, c.base+path, body)
		if err != nil {
			return nil, &APIError{Status: 0, Message: "could not build request"}
		}
		req.Header.Set("Authorization", c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, &APIError{Status: 0, Message: translateTransportError(err)}
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 && body == nil {
			delay := 2 * time.Second
			if s := resp.Header.Get("Retry-After"); s != "" {
				if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 60 {
					delay = time.Duration(n) * time.Second
				}
			}
			resp.Body.Close()
			time.Sleep(delay)
			continue
		}
		if resp.StatusCode >= 400 {
			defer resp.Body.Close()
			return nil, translateHTTPError(resp)
		}
		return resp, nil
	}
}

// translateTransportError names the failure class without echoing the
// raw error, which carries the full request URL and dial internals
// that must never reach agent-facing output. Timeouts are the one
// class worth distinguishing: the agent's right move is a plain retry,
// while everything else points at connectivity.
func translateTransportError(err error) string {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr):
		return "could not resolve the ClickUp API host; check network access (DNS) and retry"
	case isTimeout(err):
		return "the ClickUp API did not respond in time; retry shortly"
	}
	return "could not reach the ClickUp API; check network access and retry"
}

func isTimeout(err error) bool {
	var netErr net.Error
	return (errors.As(err, &netErr) && netErr.Timeout()) ||
		errors.Is(err, context.DeadlineExceeded)
}

func translateHTTPError(resp *http.Response) *APIError {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return &APIError{Status: resp.StatusCode, Message: "ClickUp rejected the token (invalid or expired)"}
	case http.StatusNotFound:
		return &APIError{Status: resp.StatusCode, Message: "not found"}
	case http.StatusTooManyRequests:
		return &APIError{Status: resp.StatusCode, Message: "ClickUp rate limit hit (about 100 requests/minute); retry later"}
	}
	var body struct {
		Err   string `json:"err"`
		Ecode string `json:"ECODE"`
	}
	msg := fmt.Sprintf("ClickUp API request failed (HTTP %d)", resp.StatusCode)
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Err != "" {
		msg = fmt.Sprintf("ClickUp rejected the request: %s (HTTP %d)", body.Err, resp.StatusCode)
	}
	return &APIError{Status: resp.StatusCode, Message: msg}
}
