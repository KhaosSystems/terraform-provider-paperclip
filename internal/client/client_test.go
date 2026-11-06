package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a client for an httptest server with fast retries.
func newTestClient(t *testing.T, handler http.Handler, mutate ...func(*Config)) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := Config{
		Endpoint:     server.URL,
		RetryWaitMin: time.Millisecond,
		RetryWaitMax: 5 * time.Millisecond,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error: %s", err)
	}
	return c
}

func TestNew(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		endpoint string
		want     string
		wantErr  string
	}{
		"plain":            {endpoint: "http://localhost:3100", want: "http://localhost:3100"},
		"trailing slash":   {endpoint: "http://localhost:3100/", want: "http://localhost:3100"},
		"api suffix":       {endpoint: "https://paperclip.example.com/api", want: "https://paperclip.example.com"},
		"api suffix slash": {endpoint: "https://paperclip.example.com/api/", want: "https://paperclip.example.com"},
		"sub path":         {endpoint: "https://example.com/paperclip/", want: "https://example.com/paperclip"},
		"query is dropped": {endpoint: "http://localhost:3100?x=1#frag", want: "http://localhost:3100"},
		"surrounding space": {
			endpoint: "  http://localhost:3100  ", want: "http://localhost:3100",
		},
		"empty":       {endpoint: "  ", wantErr: "must not be empty"},
		"no scheme":   {endpoint: "localhost:3100", wantErr: "http or https"},
		"bad scheme":  {endpoint: "ftp://localhost", wantErr: "http or https"},
		"no host":     {endpoint: "http://", wantErr: "must include a host"},
		"unparseable": {endpoint: "http://[::1", wantErr: "parsing paperclip endpoint"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := New(Config{Endpoint: tc.endpoint})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("New() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("New() unexpected error: %s", err)
			}
			if got := c.Endpoint(); got != tc.want {
				t.Errorf("Endpoint() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNew_retryDefaults(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   int
		want int
	}{
		"zero selects the default": {0, defaultMaxRetries},
		"negative disables":        {-1, 0},
		"explicit":                 {7, 7},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c, err := New(Config{Endpoint: "http://localhost", MaxRetries: tc.in})
			if err != nil {
				t.Fatal(err)
			}
			if c.maxRetries != tc.want {
				t.Errorf("maxRetries = %d, want %d", c.maxRetries, tc.want)
			}
		})
	}
}

func TestClient_requestShape(t *testing.T) {
	t.Parallel()

	var got *http.Request
	var gotBody []byte
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"c1","name":"Acme","description":null,"status":"active"}`))
	}), func(cfg *Config) {
		cfg.APIKey = " board-key "
		cfg.UserAgent = "tf-test/1.0"
	})

	company, err := c.CreateCompany(context.Background(), CompanyCreate{Name: "Acme"})
	if err != nil {
		t.Fatalf("CreateCompany() error: %s", err)
	}
	if company.ID != "c1" || company.Name != "Acme" || company.Description != nil || company.Status != "active" {
		t.Errorf("unexpected company: %+v", company)
	}

	if got.Method != http.MethodPost || got.URL.Path != "/api/companies" {
		t.Errorf("request = %s %s, want POST /api/companies", got.Method, got.URL.Path)
	}
	for header, want := range map[string]string{
		"Authorization": "Bearer board-key",
		"User-Agent":    "tf-test/1.0",
		"Content-Type":  "application/json",
		"Accept":        "application/json",
	} {
		if v := got.Header.Get(header); v != want {
			t.Errorf("header %s = %q, want %q", header, v, want)
		}
	}
	// Nullable fields are sent as null, optional ones are left out.
	if want := `{"name":"Acme","description":null}`; string(gotBody) != want {
		t.Errorf("body = %s, want %s", gotBody, want)
	}
}

func TestClient_noAuthorizationWithoutKey(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("Authorization header must not be sent without an API key")
		}
		if _, ok := r.Header["Content-Type"]; ok {
			t.Error("Content-Type header must not be sent without a body")
		}
		_, _ = w.Write([]byte(`[]`))
	}))

	companies, err := c.ListCompanies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if companies == nil || len(companies) != 0 {
		t.Errorf("ListCompanies() = %v, want an empty list", companies)
	}
}

func TestClient_pathHandling(t *testing.T) {
	t.Parallel()

	var gotPath string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{}`))
	})

	t.Run("identifiers are escaped", func(t *testing.T) {
		c := newTestClient(t, handler)
		if _, err := c.GetAgent(context.Background(), "a/b c?d"); err != nil {
			t.Fatal(err)
		}
		if want := "/api/agents/a%2Fb%20c%3Fd"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}
	})

	t.Run("base path is kept", func(t *testing.T) {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		c, err := New(Config{Endpoint: server.URL + "/paperclip/api/"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.GetCompany(context.Background(), "c1"); err != nil {
			t.Fatal(err)
		}
		if want := "/paperclip/api/companies/c1"; gotPath != want {
			t.Errorf("path = %q, want %q", gotPath, want)
		}
	})
}

func TestClient_errors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		status      int
		body        string
		wantMessage string
		wantDetails string
		notFound    bool
	}{
		"string error": {
			status: http.StatusNotFound, body: `{"error":"Company not found"}`,
			wantMessage: "Company not found", notFound: true,
		},
		"error with details": {
			status: http.StatusBadRequest, body: `{"error":"Validation error","details":[{"path":["name"]}]}`,
			wantMessage: "Validation error", wantDetails: `[{"path":["name"]}]`,
		},
		"structured error": {
			status: http.StatusForbidden, body: `{"error":{"code":"cloud_managed"}}`,
			wantMessage: `{"code":"cloud_managed"}`,
		},
		"message field": {
			status: http.StatusUnauthorized, body: `{"message":"Unauthorized"}`,
			wantMessage: "Unauthorized",
		},
		"non-JSON body": {
			status: http.StatusInternalServerError, body: "boom\n",
			wantMessage: "boom",
		},
		"empty body": {
			status: http.StatusInternalServerError, body: "",
			wantMessage: "",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))

			_, err := c.GetCompany(context.Background(), "c1")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want an *APIError", err)
			}
			if apiErr.StatusCode != tc.status || apiErr.Method != http.MethodGet || apiErr.Path != "/api/companies/c1" {
				t.Errorf("unexpected error fields: %+v", apiErr)
			}
			if apiErr.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMessage)
			}
			if string(apiErr.Details) != tc.wantDetails {
				t.Errorf("Details = %s, want %s", apiErr.Details, tc.wantDetails)
			}
			if IsNotFound(err) != tc.notFound {
				t.Errorf("IsNotFound() = %t, want %t", IsNotFound(err), tc.notFound)
			}
			if !strings.Contains(err.Error(), "GET /api/companies/c1") {
				t.Errorf("Error() = %q, want it to name the request", err.Error())
			}
		})
	}
}

func TestIsNotFound_otherErrors(t *testing.T) {
	t.Parallel()
	if IsNotFound(nil) || IsNotFound(errors.New("boom")) {
		t.Error("IsNotFound must be false for nil and non-API errors")
	}
}

func TestClient_retries(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		method    string
		status    int
		wantCalls int32
		wantErr   bool
	}{
		// 429 and 503 mean the request was not processed: always retried.
		"GET 503 is retried":    {http.MethodGet, http.StatusServiceUnavailable, 3, false},
		"POST 503 is retried":   {http.MethodPost, http.StatusServiceUnavailable, 3, false},
		"PATCH 429 is retried":  {http.MethodPatch, http.StatusTooManyRequests, 3, false},
		"DELETE 429 is retried": {http.MethodDelete, http.StatusTooManyRequests, 3, false},
		// 502 and 504 may hide a processed request: idempotent methods only.
		"GET 502 is retried":       {http.MethodGet, http.StatusBadGateway, 3, false},
		"DELETE 504 is retried":    {http.MethodDelete, http.StatusGatewayTimeout, 3, false},
		"POST 502 is not retried":  {http.MethodPost, http.StatusBadGateway, 1, true},
		"PATCH 504 is not retried": {http.MethodPatch, http.StatusGatewayTimeout, 1, true},
		// Everything else is a definitive answer.
		"GET 500 is not retried":  {http.MethodGet, http.StatusInternalServerError, 1, true},
		"GET 404 is not retried":  {http.MethodGet, http.StatusNotFound, 1, true},
		"POST 400 is not retried": {http.MethodPost, http.StatusBadRequest, 1, true},
		"POST 409 is not retried": {http.MethodPost, http.StatusConflict, 1, true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				// Fail twice, then succeed.
				if calls.Add(1) <= 2 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"error":"try again"}`))
					return
				}
				_, _ = w.Write([]byte(`{}`))
			}))

			err := c.do(context.Background(), tc.method, "/api/test", nil, nil)
			if (err != nil) != tc.wantErr {
				t.Errorf("error = %v, wantErr %t", err, tc.wantErr)
			}
			if got := calls.Load(); got != tc.wantCalls {
				t.Errorf("calls = %d, want %d", got, tc.wantCalls)
			}
		})
	}
}

func TestClient_retryLimit(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	t.Run("gives up after max retries", func(t *testing.T) {
		calls.Store(0)
		c := newTestClient(t, handler, func(cfg *Config) { cfg.MaxRetries = 2 })
		_, err := c.GetCompany(context.Background(), "c1")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("error = %v, want a 503 APIError", err)
		}
		if got := calls.Load(); got != 3 {
			t.Errorf("calls = %d, want 3 (one attempt and two retries)", got)
		}
	})

	t.Run("retries can be disabled", func(t *testing.T) {
		calls.Store(0)
		c := newTestClient(t, handler, func(cfg *Config) { cfg.MaxRetries = -1 })
		if _, err := c.GetCompany(context.Background(), "c1"); err == nil {
			t.Fatal("expected an error")
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("calls = %d, want 1", got)
		}
	})
}

func TestClient_retryHonoursContext(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}), func(cfg *Config) {
		cfg.RetryWaitMin = time.Minute
		cfg.RetryWaitMax = time.Minute
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.GetCompany(ctx, "c1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("error = %v, want it to wrap the last API error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("waited %s, the context should have cut the backoff short", elapsed)
	}
}

func TestClient_transportErrors(t *testing.T) {
	t.Parallel()

	// A server that is closed straight away refuses connections.
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	server.Close()

	c, err := New(Config{Endpoint: endpoint, RetryWaitMin: time.Millisecond, RetryWaitMax: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := c.GetCompany(context.Background(), "c1"); err == nil || !strings.Contains(err.Error(), "sending GET") {
		t.Errorf("GET error = %v, want a transport error", err)
	}
	if _, err := c.CreateCompany(context.Background(), CompanyCreate{Name: "x"}); err == nil || !strings.Contains(err.Error(), "sending POST") {
		t.Errorf("POST error = %v, want a transport error", err)
	}
}

func TestClient_invalidResponseJSON(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))

	if _, err := c.GetCompany(context.Background(), "c1"); err == nil || !strings.Contains(err.Error(), "decoding GET") {
		t.Errorf("error = %v, want a decoding error", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("empty = %s, want 0", got)
	}
	if got := parseRetryAfter("7"); got != 7*time.Second {
		t.Errorf("seconds = %s, want 7s", got)
	}
	if got := parseRetryAfter("-3"); got != 0 {
		t.Errorf("negative = %s, want 0", got)
	}
	if got := parseRetryAfter("soon"); got != 0 {
		t.Errorf("garbage = %s, want 0", got)
	}
	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 || got > time.Hour {
		t.Errorf("HTTP date = %s, want a positive duration up to 1h", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("past HTTP date = %s, want 0", got)
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()

	c, err := New(Config{Endpoint: "http://localhost", RetryWaitMin: 100 * time.Millisecond, RetryWaitMax: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 40 {
		got := c.backoff(attempt)
		if got <= 0 || got > time.Second {
			t.Fatalf("backoff(%d) = %s, want within (0, 1s]", attempt, got)
		}
	}
	if got := c.backoff(0); got < 50*time.Millisecond || got > 100*time.Millisecond {
		t.Errorf("backoff(0) = %s, want within [50ms, 100ms]", got)
	}
}

// recorded captures one request made against recordingServer.
type recorded struct {
	Method string
	Path   string
	Body   string
}

// recordingClient returns a client whose requests are recorded and answered
// with a fixed JSON response.
func recordingClient(t *testing.T, response string) (*Client, *[]recorded) {
	t.Helper()
	var calls []recorded
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, recorded{Method: r.Method, Path: r.URL.Path, Body: string(body)})
		_, _ = w.Write([]byte(response))
	}))
	return c, &calls
}

func assertCall(t *testing.T, calls *[]recorded, method, path, body string) {
	t.Helper()
	if len(*calls) == 0 {
		t.Fatal("no request was made")
	}
	got := (*calls)[len(*calls)-1]
	if got.Method != method || got.Path != path {
		t.Errorf("request = %s %s, want %s %s", got.Method, got.Path, method, path)
	}
	if body == "" {
		if got.Body != "" {
			t.Errorf("body = %s, want none", got.Body)
		}
		return
	}
	var gotJSON, wantJSON any
	if err := json.Unmarshal([]byte(got.Body), &gotJSON); err != nil {
		t.Fatalf("request body is not JSON: %s", got.Body)
	}
	if err := json.Unmarshal([]byte(body), &wantJSON); err != nil {
		t.Fatalf("expected body is not JSON: %s", body)
	}
	gotNorm, _ := json.Marshal(gotJSON)
	wantNorm, _ := json.Marshal(wantJSON)
	if string(gotNorm) != string(wantNorm) {
		t.Errorf("body = %s, want %s", gotNorm, wantNorm)
	}
}

func ptr[T any](v T) *T { return &v }

func TestCompanyEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const response = `{"id":"c1","name":"Acme","description":"Films","status":"active","issuePrefix":"ACM",
		"budgetMonthlyCents":500,"spentMonthlyCents":20,"requireBoardApprovalForNewAgents":true,
		"createdAt":"2026-01-01T00:00:00.000Z","updatedAt":"2026-01-02T00:00:00.000Z","unknownField":1}`
	c, calls := recordingClient(t, response)

	company, err := c.GetCompany(ctx, "c1")
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodGet, "/api/companies/c1", "")
	want := Company{
		ID: "c1", Name: "Acme", Description: ptr("Films"), Status: "active", IssuePrefix: "ACM",
		BudgetMonthlyCents: 500, SpentMonthlyCents: 20, RequireBoardApprovalForNewAgents: true,
		CreatedAt: "2026-01-01T00:00:00.000Z", UpdatedAt: "2026-01-02T00:00:00.000Z",
	}
	if company.ID != want.ID || company.Name != want.Name || *company.Description != *want.Description ||
		company.Status != want.Status || company.IssuePrefix != want.IssuePrefix ||
		company.BudgetMonthlyCents != want.BudgetMonthlyCents || company.SpentMonthlyCents != want.SpentMonthlyCents ||
		company.RequireBoardApprovalForNewAgents != want.RequireBoardApprovalForNewAgents ||
		company.CreatedAt != want.CreatedAt || company.UpdatedAt != want.UpdatedAt {
		t.Errorf("GetCompany() = %+v, want %+v", company, want)
	}

	if _, err := c.CreateCompany(ctx, CompanyCreate{Name: "Acme", Description: ptr("Films"), BudgetMonthlyCents: ptr(int64(0))}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/companies", `{"name":"Acme","description":"Films","budgetMonthlyCents":0}`)

	if _, err := c.UpdateCompany(ctx, "c1", CompanyUpdate{Name: ptr("New"), RequireBoardApprovalForNewAgents: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	// A nil description is sent as null so that it is cleared; false is kept.
	assertCall(t, calls, http.MethodPatch, "/api/companies/c1",
		`{"name":"New","description":null,"requireBoardApprovalForNewAgents":false}`)

	if err := c.DeleteCompany(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodDelete, "/api/companies/c1", "")
}

func TestAgentEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const response = `{"id":"a1","companyId":"c1","name":"Producer","urlKey":"producer","role":"pm","title":null,
		"status":"idle","reportsTo":"a0","capabilities":null,"adapterType":"process",
		"adapterConfig":{"command":"echo","env":{"TOKEN":"***REDACTED***"}},"runtimeConfig":{},
		"budgetMonthlyCents":0,"spentMonthlyCents":0,"metadata":null}`
	c, calls := recordingClient(t, response)

	agent, err := c.GetAgent(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodGet, "/api/agents/a1", "")
	if agent.ID != "a1" || agent.CompanyID != "c1" || agent.URLKey != "producer" || agent.Role != "pm" ||
		agent.Title != nil || agent.ReportsTo == nil || *agent.ReportsTo != "a0" || agent.AdapterType != "process" ||
		agent.Metadata != nil || agent.AdapterConfig["command"] != "echo" {
		t.Errorf("unexpected agent: %+v", agent)
	}
	if env, _ := agent.AdapterConfig["env"].(map[string]any); env["TOKEN"] != RedactedValue {
		t.Errorf("env = %v, want the redacted placeholder", env)
	}

	_, err = c.CreateAgent(ctx, "c1", AgentCreate{Name: "Producer", AdapterType: "process"})
	if err != nil {
		t.Fatal(err)
	}
	// Unset settings are left to the server; nullable fields are explicit.
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/agents",
		`{"name":"Producer","title":null,"reportsTo":null,"capabilities":null,"adapterType":"process","metadata":null}`)

	_, err = c.CreateAgent(ctx, "c1", AgentCreate{
		Name: "Producer", Role: ptr("pm"), AdapterType: "process",
		AdapterConfig: map[string]any{}, RuntimeConfig: map[string]any{"heartbeat": map[string]any{"enabled": true}},
		BudgetMonthlyCents: ptr(int64(0)), Metadata: map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// An empty but non-nil config is sent, so it can be used to clear settings.
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/agents",
		`{"name":"Producer","role":"pm","title":null,"reportsTo":null,"capabilities":null,"adapterType":"process",
		  "adapterConfig":{},"runtimeConfig":{"heartbeat":{"enabled":true}},"budgetMonthlyCents":0,"metadata":{"k":"v"}}`)

	_, err = c.UpdateAgent(ctx, "a1", AgentUpdate{Name: ptr("Producer"), Title: ptr("EP")})
	if err != nil {
		t.Fatal(err)
	}
	// Settings that did not change are not sent at all.
	assertCall(t, calls, http.MethodPatch, "/api/agents/a1",
		`{"name":"Producer","title":"EP","reportsTo":null,"capabilities":null,"metadata":null}`)

	_, err = c.UpdateAgent(ctx, "a1", AgentUpdate{AdapterConfig: map[string]any{}, ReplaceAdapterConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPatch, "/api/agents/a1",
		`{"title":null,"reportsTo":null,"capabilities":null,"adapterConfig":{},"metadata":null,"replaceAdapterConfig":true}`)

	if _, err := c.ListAgents(ctx, "c1"); err == nil {
		t.Error("ListAgents() must fail when the response is not a list")
	}
	assertCall(t, calls, http.MethodGet, "/api/companies/c1/agents", "")

	if err := c.DeleteAgent(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodDelete, "/api/agents/a1", "")
}

func TestGoalEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	c, calls := recordingClient(t, `{"id":"g1","companyId":"c1","title":"Ship","description":null,
		"level":"company","status":"active","parentId":null,"ownerAgentId":"a1"}`)

	goal, err := c.GetGoal(ctx, "g1")
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodGet, "/api/goals/g1", "")
	if goal.ID != "g1" || goal.Level != "company" || goal.ParentID != nil || goal.OwnerAgentID == nil || *goal.OwnerAgentID != "a1" {
		t.Errorf("unexpected goal: %+v", goal)
	}

	if _, err := c.CreateGoal(ctx, "c1", GoalWrite{Title: "Ship"}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/goals",
		`{"title":"Ship","description":null,"parentId":null,"ownerAgentId":null}`)

	if _, err := c.UpdateGoal(ctx, "g1", GoalWrite{Title: "Ship", Level: ptr("team"), Status: ptr("active"), ParentID: ptr("g0")}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPatch, "/api/goals/g1",
		`{"title":"Ship","description":null,"level":"team","status":"active","parentId":"g0","ownerAgentId":null}`)

	if err := c.DeleteGoal(ctx, "g1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodDelete, "/api/goals/g1", "")
}

func TestProjectEndpoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	c, calls := recordingClient(t, `{"id":"p1","companyId":"c1","urlKey":"film","name":"Film","description":null,
		"status":"backlog","goalIds":["g1","g2"],"leadAgentId":null,"targetDate":"2027-03-31","color":"#6366f1"}`)

	project, err := c.GetProject(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodGet, "/api/projects/p1", "")
	if project.ID != "p1" || project.URLKey != "film" || len(project.GoalIDs) != 2 ||
		project.TargetDate == nil || *project.TargetDate != "2027-03-31" || project.LeadAgentID != nil {
		t.Errorf("unexpected project: %+v", project)
	}

	if _, err := c.CreateProject(ctx, "c1", ProjectWrite{Name: "Film"}); err != nil {
		t.Fatal(err)
	}
	// goalIds is always a list, never null, so that links can be removed.
	assertCall(t, calls, http.MethodPost, "/api/companies/c1/projects",
		`{"name":"Film","description":null,"goalIds":[],"leadAgentId":null,"targetDate":null}`)

	if _, err := c.UpdateProject(ctx, "p1", ProjectWrite{Name: "Film", Status: ptr("planned"), GoalIDs: []string{"g1"}, Color: ptr("#fff")}); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodPatch, "/api/projects/p1",
		`{"name":"Film","description":null,"status":"planned","goalIds":["g1"],"leadAgentId":null,"targetDate":null,"color":"#fff"}`)

	if err := c.DeleteProject(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	assertCall(t, calls, http.MethodDelete, "/api/projects/p1", "")
}
