package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
	"github.com/qzydustin/nanoapi/provider"
	"github.com/qzydustin/nanoapi/storage"
	"github.com/qzydustin/nanoapi/token"
	"github.com/qzydustin/nanoapi/usage"
)

type testEnv struct {
	cfg      *config.Config
	tokenSvc *token.Service
	usageSvc *usage.Service
	selector *provider.Selector
	executor *execute.Executor
	router   http.Handler
	apiToken string
	tokenID  string
}

func newTestEnv(t *testing.T, upstreamHandler http.HandlerFunc) *testEnv {
	t.Helper()

	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)

	cfg := &config.Config{
		Server:  config.ServerConfig{Host: "0.0.0.0", Port: 8080},
		Logging: config.LoggingConfig{RequestDir: t.TempDir()},
		Storage: config.StorageConfig{Driver: "sqlite", DSN: ":memory:"},
		Tokens: []config.TokenConfig{
			{ID: "tok_default", Key: "nk_test_token"},
		},
		Providers: []config.ProviderConfig{
			{
				Name:     "mock-anthropic",
				Protocol: "anthropic_messages",
				BaseURL:  upstream.URL,
				APIKey:   "test-anthropic-key",
				Priority: 100,
				Models: map[string]config.ModelTargetConfig{
					"gpt-4o":      {Upstream: "claude-3-7-sonnet"},
					"gpt-4o-mini": {Upstream: "claude-3-7-sonnet"},
				},
			},
			{
				Name:     "mock-openai",
				Protocol: "openai_chat",
				BaseURL:  upstream.URL,
				APIKey:   "test-openai-key",
				Priority: 90,
				Models: map[string]config.ModelTargetConfig{
					"claude-sonnet": {Upstream: "gpt-4o"},
				},
			},
		},
	}

	db, err := storage.NewDB(cfg.Storage)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(&usage.UsageRecord{}); err != nil {
		t.Fatal(err)
	}

	tokenSvc := token.NewService(cfg.Tokens)
	usageStore := usage.NewSQLiteStore(db.Gorm)
	usageSvc := usage.NewService(usageStore)
	selector := provider.NewSelector(cfg.Providers)
	executor := execute.NewExecutor()
	router := NewRouter(tokenSvc, usageSvc, selector, executor, cfg.Logging)

	return &testEnv{
		cfg:      cfg,
		tokenSvc: tokenSvc,
		usageSvc: usageSvc,
		selector: selector,
		executor: executor,
		router:   router,
		apiToken: cfg.Tokens[0].Key,
		tokenID:  cfg.Tokens[0].ID,
	}
}

func requireUsageRecords(t *testing.T, env *testEnv, want int) []usage.UsageRecord {
	t.Helper()

	records, err := env.usageSvc.QueryUsage(env.tokenID, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query usage: %v", err)
	}
	if len(records) != want {
		t.Fatalf("usage records = %d, want %d", len(records), want)
	}
	return records
}

func TestAPIHealth(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v", body)
	}
}

func TestProxyOpenAIToAnthropicNonStream(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtureString(t, "anthropic_upstream_nonstream_response.json")))
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fixtureString(t, "openai_basic_request.json")))
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	records := requireUsageRecords(t, env, 1)
	if !records[0].Success {
		t.Errorf("usage success = false")
	}
	if records[0].TokenID != env.tokenID {
		t.Errorf("token_id = %q", records[0].TokenID)
	}
}

func TestProxyAnthropicToOpenAINonStream(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected upstream path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtureString(t, "openai_upstream_nonstream_response.json")))
	})

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(fixtureString(t, "anthropic_basic_request.json")))
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestProxyMissingToken(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fixtureString(t, "openai_basic_request.json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 401 {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestProxyModelNotFoundRecordsFailure(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fixtureString(t, "openai_unknown_model_request.json")))
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	records := requireUsageRecords(t, env, 1)
	if records[0].Success {
		t.Errorf("failed request should record success=false")
	}
}

func TestAPIUsageAndLogs(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtureString(t, "anthropic_upstream_nonstream_response.json")))
	})

	usageSvc := env.usageSvc
	now := time.Now()
	usageSvc.RecordUsage(&usage.UsageRecord{
		ID:                       uuid.New().String(),
		TokenID:                  env.tokenID,
		Timestamp:                now,
		ClientProtocol:           "openai_chat",
		UpstreamProtocol:         "anthropic_messages",
		ClientModel:              "gpt-4o",
		UpstreamModel:            "claude-3-7-sonnet",
		InputTokens:              10,
		OutputTokens:             5,
		CacheCreationInputTokens: 2,
		CacheReadInputTokens:     3,
		TotalTokens:              15,
		Success:                  true,
	})

	req := httptest.NewRequest("GET", "/api/usage", nil)
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("usage status = %d, body = %s", w.Code, w.Body.String())
	}
	var summaryResp struct {
		TokenID string `json:"token_id"`
		Summary struct {
			RequestCount             int64 `json:"request_count"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			TotalTokens              int64 `json:"total_tokens"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &summaryResp); err != nil {
		t.Fatalf("decode usage response: %v", err)
	}
	if summaryResp.TokenID != env.tokenID {
		t.Errorf("token_id = %q", summaryResp.TokenID)
	}
	if summaryResp.Summary.RequestCount != 1 {
		t.Errorf("request_count = %d", summaryResp.Summary.RequestCount)
	}
	if summaryResp.Summary.CacheCreationInputTokens != 2 {
		t.Errorf("cache_creation_input_tokens = %d", summaryResp.Summary.CacheCreationInputTokens)
	}
	if summaryResp.Summary.CacheReadInputTokens != 3 {
		t.Errorf("cache_read_input_tokens = %d", summaryResp.Summary.CacheReadInputTokens)
	}

	req = httptest.NewRequest("GET", "/api/logs", nil)
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	w = httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("logs status = %d, body = %s", w.Code, w.Body.String())
	}
	var logsResp struct {
		TokenID string              `json:"token_id"`
		Records []usage.UsageRecord `json:"records"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &logsResp); err != nil {
		t.Fatalf("decode logs response: %v", err)
	}
	if logsResp.TokenID != env.tokenID {
		t.Errorf("token_id = %q", logsResp.TokenID)
	}
	if len(logsResp.Records) != 1 {
		t.Fatalf("records = %d", len(logsResp.Records))
	}
	if logsResp.Records[0].CacheCreationInputTokens != 2 {
		t.Errorf("cache_creation_input_tokens = %d", logsResp.Records[0].CacheCreationInputTokens)
	}
	if logsResp.Records[0].CacheReadInputTokens != 3 {
		t.Errorf("cache_read_input_tokens = %d", logsResp.Records[0].CacheReadInputTokens)
	}
}

func TestAPIUsageIsScopedToCurrentToken(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {})

	env.usageSvc.RecordUsage(&usage.UsageRecord{
		ID:          uuid.New().String(),
		TokenID:     "other-token",
		Timestamp:   time.Now(),
		TotalTokens: 99,
		Success:     true,
	})

	req := httptest.NewRequest("GET", "/api/logs", nil)
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("logs status = %d", w.Code)
	}
	var resp struct {
		Records []usage.UsageRecord `json:"records"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode logs response: %v", err)
	}
	if len(resp.Records) != 0 {
		t.Fatalf("records = %d, want 0", len(resp.Records))
	}
}

func TestProxyForcedStreamAggregation(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, fixtureString(t, "anthropic_aggregate_stream.sse"))
		if flusher != nil {
			flusher.Flush()
		}
	})

	env.cfg.Providers[0].ForceStream = true
	env.selector = provider.NewSelector(env.cfg.Providers)
	env.router = NewRouter(env.tokenSvc, env.usageSvc, env.selector, env.executor, env.cfg.Logging)

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fixtureString(t, "openai_basic_request.json")))
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestProxyReasoningMapping(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request: %v", err)
		}
		if !strings.Contains(string(body), `"thinking":{"type":"adaptive"}`) {
			t.Errorf("missing thinking mapping: %s", string(body))
		}
		if !strings.Contains(string(body), `"output_config":{"effort":"high"}`) {
			t.Errorf("missing effort mapping: %s", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(fixtureString(t, "anthropic_reasoning_response.json")))
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(fixtureString(t, "openai_reasoning_request.json")))
	req.Header.Set("Authorization", "Bearer "+env.apiToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}
