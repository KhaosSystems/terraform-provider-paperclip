// Package client is a small, dependency-free HTTP client for the Paperclip
// REST API. It only covers the endpoints the Terraform provider needs.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTimeout      = 60 * time.Second
	defaultMaxRetries   = 3
	defaultRetryWaitMin = 500 * time.Millisecond
	defaultRetryWaitMax = 10 * time.Second
	maxErrorBodyBytes   = 64 * 1024
	maxResponseBytes    = 32 * 1024 * 1024
)

// Config configures a Client.
type Config struct {
	// Endpoint is the base URL of the Paperclip instance, for example
	// "http://localhost:3100". A trailing "/api" segment is accepted and
	// ignored.
	Endpoint string
	// APIKey is a board API key sent as a bearer token. It may be empty for
	// instances that run in the "local_trusted" deployment mode.
	APIKey string
	// UserAgent is sent with every request.
	UserAgent string
	// HTTPClient overrides the default HTTP client.
	HTTPClient *http.Client
	// MaxRetries is the number of retries for transient failures. A negative
	// value disables retries; zero selects the default.
	MaxRetries int
	// RetryWaitMin and RetryWaitMax bound the exponential backoff.
	RetryWaitMin time.Duration
	RetryWaitMax time.Duration
}

// Client talks to a single Paperclip instance.
type Client struct {
	baseURL      *url.URL
	apiKey       string
	userAgent    string
	httpClient   *http.Client
	maxRetries   int
	retryWaitMin time.Duration
	retryWaitMax time.Duration
}

// New validates cfg and returns a ready to use Client.
func New(cfg Config) (*Client, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("paperclip endpoint must not be empty")
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parsing paperclip endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("paperclip endpoint %q must use the http or https scheme", endpoint)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("paperclip endpoint %q must include a host", endpoint)
	}

	// Accept both "http://host" and "http://host/api" and normalise to the
	// former so that request paths can always start with "/api".
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/api")
	u.RawQuery = ""
	u.Fragment = ""

	c := &Client{
		baseURL:      u,
		apiKey:       strings.TrimSpace(cfg.APIKey),
		userAgent:    cfg.UserAgent,
		httpClient:   cfg.HTTPClient,
		maxRetries:   cfg.MaxRetries,
		retryWaitMin: cfg.RetryWaitMin,
		retryWaitMax: cfg.RetryWaitMax,
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultTimeout}
	}
	if c.userAgent == "" {
		c.userAgent = "terraform-provider-paperclip"
	}
	switch {
	case c.maxRetries == 0:
		c.maxRetries = defaultMaxRetries
	case c.maxRetries < 0:
		c.maxRetries = 0
	}
	if c.retryWaitMin <= 0 {
		c.retryWaitMin = defaultRetryWaitMin
	}
	if c.retryWaitMax < c.retryWaitMin {
		c.retryWaitMax = max(defaultRetryWaitMax, c.retryWaitMin)
	}

	return c, nil
}

// Endpoint returns the normalised base URL of the instance.
func (c *Client) Endpoint() string {
	return c.baseURL.String()
}

// APIError is returned for every non-2xx response.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Message    string
	Details    json.RawMessage
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if len(e.Details) > 0 && string(e.Details) != "null" {
		return fmt.Sprintf("paperclip API %s %s: %d %s (details: %s)", e.Method, e.Path, e.StatusCode, msg, e.Details)
	}
	return fmt.Sprintf("paperclip API %s %s: %d %s", e.Method, e.Path, e.StatusCode, msg)
}

// IsNotFound reports whether err is an APIError with a 404 status.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// apiPath builds an escaped request path below "/api" from raw segments.
func apiPath(segments ...string) string {
	var b strings.Builder
	b.WriteString("/api")
	for _, s := range segments {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	return b.String()
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

func (c *Client) patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, body, out)
}

func (c *Client) delete(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// do sends a JSON request and decodes a JSON response into out (when non-nil).
// Transient failures are retried with exponential backoff.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding %s %s request body: %w", method, path, err)
		}
	}

	target := *c.baseURL
	rawPath, rawQuery, _ := strings.Cut(path, "?")
	target.RawPath = strings.TrimRight(c.baseURL.EscapedPath(), "/") + rawPath
	unescaped, err := url.PathUnescape(target.RawPath)
	if err != nil {
		return fmt.Errorf("building %s %s request URL: %w", method, path, err)
	}
	target.Path = unescaped
	target.RawQuery = rawQuery

	var lastErr error
	for attempt := 0; ; attempt++ {
		retryAfter, retryable, err := c.attempt(ctx, method, path, target.String(), payload, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt >= c.maxRetries {
			return lastErr
		}

		wait := c.backoff(attempt)
		if retryAfter > 0 {
			wait = min(retryAfter, c.retryWaitMax)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w (last error: %w)", ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

// attempt performs a single HTTP round trip. It reports whether a failure may
// be retried and any server-requested delay.
func (c *Client) attempt(ctx context.Context, method, path, target string, payload []byte, out any) (time.Duration, bool, error) {
	var reqBody io.Reader
	if payload != nil {
		reqBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reqBody)
	if err != nil {
		return 0, false, fmt.Errorf("building %s %s request: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, false, ctx.Err()
		}
		// A transport error on a non-idempotent request may have been
		// processed by the server, so only safe methods are retried.
		return 0, isIdempotent(method), fmt.Errorf("sending %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil || resp.StatusCode == http.StatusNoContent {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
			return 0, false, nil
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return 0, isIdempotent(method), fmt.Errorf("reading %s %s response: %w", method, path, err)
		}
		if err := json.Unmarshal(data, out); err != nil {
			return 0, false, fmt.Errorf("decoding %s %s response: %w", method, path, err)
		}
		return 0, false, nil
	}

	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	apiErr := &APIError{StatusCode: resp.StatusCode, Method: method, Path: path}
	var envelope struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details"`
	}
	if json.Unmarshal(data, &envelope) == nil {
		var msg string
		if json.Unmarshal(envelope.Error, &msg) == nil {
			apiErr.Message = msg
		} else if len(envelope.Error) > 0 {
			apiErr.Message = string(envelope.Error)
		}
		if apiErr.Message == "" {
			apiErr.Message = envelope.Message
		}
		apiErr.Details = envelope.Details
	}
	if apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(string(data))
	}

	return parseRetryAfter(resp.Header.Get("Retry-After")), isRetryableStatus(method, resp.StatusCode), apiErr
}

func (c *Client) backoff(attempt int) time.Duration {
	wait := c.retryWaitMin << min(attempt, 16)
	if wait <= 0 || wait > c.retryWaitMax {
		wait = c.retryWaitMax
	}
	// Full jitter in the upper half keeps retries from synchronising.
	half := wait / 2
	if half <= 0 {
		return wait
	}
	return half + rand.N(half+1)
}

func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isRetryableStatus(method string, status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		// The server did not process the request.
		return true
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return isIdempotent(method)
	default:
		return false
	}
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
