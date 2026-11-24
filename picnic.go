// Package picnic is an unofficial client for the API behind the Picnic
// grocery delivery app. It covers signing in (including the SMS second
// factor), searching for products, and reading and adding to the basket.
//
// Picnic does not publish this API. The endpoints, headers and login flow
// follow python-picnic-api2 (https://github.com/codesalatdev/python-picnic-api),
// and can change whenever Picnic updates its app.
package picnic

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
)

const (
	defaultCountry = "NL"
	apiVersion     = "15"
	authHeader     = "x-picnic-auth"

	// The app identifiers python-picnic-api2 sends; some endpoints refuse
	// requests without them.
	agent    = "30100;1.206.1-#15408"
	deviceID = "598F770380CA54B6"
	clientID = 30100

	maxBody = 16 << 20
)

// ErrUnauthorized is returned (wrapped in an *Error) when Picnic rejects the
// credentials or the auth token. A client holding a rejected token must sign
// in again.
var ErrUnauthorized = errors.New("picnic: unauthorized")

// ErrSecondFactorRequired is returned by Login when Picnic wants a one-time
// code: call RequestCode, then VerifyCode with the code the user received.
var ErrSecondFactorRequired = errors.New("picnic: second factor required")

// Error is a failed call: Picnic answered with an error, an unexpected
// status, or a body this package could not read.
type Error struct {
	Endpoint string // the path called, without query
	Status   int    // the HTTP status
	Code     string // Picnic's error code, if it sent one
	Message  string
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "picnic: %s: status %d", e.Endpoint, e.Status)
	if e.Code != "" {
		fmt.Fprintf(&b, ", %s", e.Code)
	}
	if e.Message != "" {
		fmt.Fprintf(&b, ": %s", e.Message)
	}
	return b.String()
}

// Is reports an authentication failure as ErrUnauthorized.
func (e *Error) Is(target error) bool {
	if target != ErrUnauthorized {
		return false
	}
	return e.Code == "AUTH_ERROR" || e.Code == "AUTH_INVALID_CRED" || e.Status == http.StatusUnauthorized
}

// Client calls the Picnic API. It holds the auth token, which Picnic may
// replace on any response; read it with Token after a call to persist it.
// A Client is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client

	mu    sync.Mutex
	token string
}

// Option configures a Client.
type Option func(*Client)

// WithCountry selects the storefront by country code ("NL", "DE", "BE").
// The default is NL.
func WithCountry(code string) Option {
	return func(c *Client) { c.baseURL = countryURL(code) }
}

// WithBaseURL replaces the API's base URL, including the version path, for
// example "https://storefront-prod.nl.picnicinternational.com/api/15".
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithToken starts the client signed in with a token from an earlier Login.
func WithToken(token string) Option {
	return func(c *Client) { c.token = token }
}

// WithHTTPClient replaces http.DefaultClient.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New returns a client. Without WithToken it must Login before other calls.
func New(opts ...Option) *Client {
	c := &Client{baseURL: countryURL(defaultCountry), http: http.DefaultClient}
	for _, o := range opts {
		o(c)
	}
	return c
}

func countryURL(code string) string {
	return fmt.Sprintf("https://storefront-prod.%s.picnicinternational.com/api/%s",
		strings.ToLower(code), apiVersion)
}

// Token returns the current auth token, "" before a Login.
func (c *Client) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// call sends one request and returns the status and body, keeping any new
// auth token Picnic sends back. path may include a query; endpoint is the
// path without it, for errors.
func (c *Client) call(ctx context.Context, method, path string, in any) (int, []byte, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "okhttp/4.9.0")
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("x-picnic-agent", agent)
	req.Header.Set("x-picnic-did", deviceID)
	if t := c.Token(); t != "" {
		req.Header.Set(authHeader, t)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("picnic: %s: %w", endpoint(path), err)
	}
	defer resp.Body.Close()
	if t := resp.Header.Get(authHeader); t != "" {
		c.mu.Lock()
		c.token = t
		c.mu.Unlock()
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("picnic: %s: %w", endpoint(path), err)
	}
	return resp.StatusCode, b, nil
}

// decode turns a response into out, or into an *Error when Picnic reported
// one, the status is not a success, or the body does not fit out. out may be
// nil when the body does not matter.
func decode(path string, status int, body []byte, out any) error {
	ep := endpoint(path)
	var env struct {
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		_ = json.Unmarshal(trimmed, &env) // a body that doesn't parse fails below
	}
	if env.Error != nil && (env.Error.Code != "" || status >= 300) {
		return &Error{Endpoint: ep, Status: status, Code: env.Error.Code, Message: env.Error.Message}
	}
	if status < 200 || status >= 300 {
		return &Error{Endpoint: ep, Status: status}
	}
	if out == nil {
		return nil
	}
	if len(trimmed) == 0 {
		return &Error{Endpoint: ep, Status: status, Message: "empty response"}
	}
	if err := json.Unmarshal(trimmed, out); err != nil {
		return &Error{Endpoint: ep, Status: status, Message: "unexpected response: " + err.Error()}
	}
	return nil
}

func endpoint(path string) string {
	if i := strings.IndexByte(path, '?'); i >= 0 {
		return path[:i]
	}
	return path
}

// cents is an amount in euro cents. Picnic sends some prices as integers
// and some as floats (1035.0).
type cents int

func (c *cents) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return err
	}
	*c = cents(int(f + 0.5))
	return nil
}
