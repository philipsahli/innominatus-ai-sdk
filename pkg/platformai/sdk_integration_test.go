package platformai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/codemapping"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/rag"
)

const (
	testModel        = "test-model"
	testAnthropicKey = "test-anthropic-key"
	testEmbedModel   = "test-embed-model"
	testEmbedKey     = "test-embed-key"
)

// anthropicRequest is what the fake Anthropic server records per call.
type anthropicRequest struct {
	Path   string
	APIKey string
	Body   struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
}

type fakeAnthropic struct {
	mu       sync.Mutex
	requests []anthropicRequest
}

func (f *fakeAnthropic) last(t *testing.T) anthropicRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("fake Anthropic server received no request")
	}
	return f.requests[len(f.requests)-1]
}

// newFakeAnthropic records each request and lets respond write the reply.
func newFakeAnthropic(t *testing.T, respond func(w http.ResponseWriter)) (*httptest.Server, *fakeAnthropic) {
	t.Helper()
	fake := &fakeAnthropic{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := anthropicRequest{Path: r.URL.Path, APIKey: r.Header.Get("x-api-key")}
		if err := json.NewDecoder(r.Body).Decode(&rec.Body); err != nil {
			t.Errorf("decode Anthropic request: %v", err)
		}
		fake.mu.Lock()
		fake.requests = append(fake.requests, rec)
		fake.mu.Unlock()
		respond(w)
	}))
	t.Cleanup(server.Close)
	return server, fake
}

func messageJSON(t *testing.T, stopReason string, content ...map[string]interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]interface{}{
		"id":          "msg_test",
		"type":        "message",
		"role":        "assistant",
		"model":       testModel,
		"content":     content,
		"stop_reason": stopReason,
		"usage":       map[string]int{"input_tokens": 10, "output_tokens": 20},
	})
	if err != nil {
		t.Fatalf("marshal canned message: %v", err)
	}
	return b
}

func replyText(t *testing.T, text string) func(http.ResponseWriter) {
	body := messageJSON(t, "end_turn", map[string]interface{}{"type": "text", "text": text})
	return func(w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(body)
	}
}

func newSDK(t *testing.T, anthropicURL string, ragCfg *rag.Config) *platformai.SDK {
	t.Helper()
	sdk, err := platformai.New(context.Background(), &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   testAnthropicKey,
			Model:    testModel,
			BaseURL:  anthropicURL,
		},
		RAG: ragCfg,
	})
	if err != nil {
		t.Fatalf("platformai.New: %v", err)
	}
	return sdk
}

func cannedConfig(name, runtime, framework string) string {
	return `{
  "service": {"name": "` + name + `", "template": "api", "runtime": "` + runtime + `", "framework": "` + framework + `", "port": 8080},
  "resources": {"cpu": "500m", "memory": "512Mi", "scaling": {"min_replicas": 2, "max_replicas": 5, "target_cpu_percent": 70}},
  "database": {"type": "postgresql", "version": "16", "storage": "10Gi"},
  "cache": {"type": "redis", "version": "7", "memory": "256Mi"},
  "monitoring": {"metrics": true, "logs": true, "traces": false},
  "security": {"health_check": {"path": "/health", "port": 8080}}
}`
}

func TestSDK_CodeMappingAnalyze(t *testing.T) {
	tests := []struct {
		repo          string
		wantLanguage  string
		wantFramework string
		wantRuntime   string
	}{
		{"sample-go-repo", "go", "gin", "go1.21"},
		{"sample-node-repo", "nodejs", "express", "node20"},
		{"sample-python-repo", "python", "fastapi", "python3.11"},
	}
	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			// The model's reply arrives wrapped in a markdown fence, as Claude often does.
			reply := "```json\n" + cannedConfig(tt.repo, tt.wantRuntime, tt.wantFramework) + "\n```"
			server, fake := newFakeAnthropic(t, replyText(t, reply))
			sdk := newSDK(t, server.URL, nil)

			res, err := sdk.CodeMapping().Analyze(context.Background(), codemapping.AnalyzeRequest{
				RepoPath: filepath.Join("..", "..", "testdata", tt.repo),
			})
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}

			if res.Analysis.PrimaryLanguage != tt.wantLanguage {
				t.Errorf("PrimaryLanguage = %q, want %q", res.Analysis.PrimaryLanguage, tt.wantLanguage)
			}
			if res.Analysis.DetectedFramework != tt.wantFramework {
				t.Errorf("DetectedFramework = %q, want %q", res.Analysis.DetectedFramework, tt.wantFramework)
			}
			if res.Config == nil {
				t.Fatal("Config is nil")
			}
			if res.Config.Service.Name != tt.repo || res.Config.Service.Runtime != tt.wantRuntime {
				t.Errorf("Config.Service = %+v, want name %q runtime %q", res.Config.Service, tt.repo, tt.wantRuntime)
			}
			if res.Config.Database == nil || res.Config.Database.Type != "postgresql" {
				t.Errorf("Config.Database = %+v, want postgresql", res.Config.Database)
			}
			if len(res.Recommendations) == 0 {
				t.Error("Recommendations is empty")
			}

			req := fake.last(t)
			if req.Path != "/v1/messages" {
				t.Errorf("request path = %q, want /v1/messages", req.Path)
			}
			if req.APIKey != testAnthropicKey {
				t.Errorf("x-api-key = %q, want the configured key", req.APIKey)
			}
			if req.Body.Model != testModel {
				t.Errorf("model = %q, want %q", req.Body.Model, testModel)
			}
			// Only the order-independent prompt lines are checked; the
			// dependency list is built from a map.
			if len(req.Body.Messages) != 1 {
				t.Fatalf("messages = %d, want 1", len(req.Body.Messages))
			}
			var prompt string
			if err := json.Unmarshal(req.Body.Messages[0].Content, &prompt); err != nil {
				t.Fatalf("user content is not a string: %v", err)
			}
			if !strings.Contains(prompt, "Primary Language: "+tt.wantLanguage) {
				t.Errorf("prompt does not name language %q", tt.wantLanguage)
			}
		})
	}
}

func TestSDK_CodeMappingAnalyze_Errors(t *testing.T) {
	tests := []struct {
		name    string
		respond func(t *testing.T) func(http.ResponseWriter)
	}{
		{"rate limited", func(t *testing.T) func(http.ResponseWriter) {
			return func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`))
			}
		}},
		{"non-JSON HTTP body", func(t *testing.T) func(http.ResponseWriter) {
			return func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("<html>bad gateway</html>"))
			}
		}},
		{"model reply is not config JSON", func(t *testing.T) func(http.ResponseWriter) {
			return replyText(t, "Sorry, I cannot produce a configuration for this repository.")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, _ := newFakeAnthropic(t, tt.respond(t))
			sdk := newSDK(t, server.URL, nil)

			res, err := sdk.CodeMapping().Analyze(context.Background(), codemapping.AnalyzeRequest{
				RepoPath: filepath.Join("..", "..", "testdata", "sample-go-repo"),
			})
			if err == nil {
				t.Fatalf("Analyze returned no error, result %+v", res)
			}
			if res != nil {
				t.Errorf("Analyze result = %+v, want nil on error", res)
			}
		})
	}
}

func TestSDK_LLMGenerateWithTools(t *testing.T) {
	body := messageJSON(t, "tool_use",
		map[string]interface{}{"type": "text", "text": "Looking up the weather."},
		map[string]interface{}{"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": map[string]interface{}{"city": "Bern"}},
	)
	server, fake := newFakeAnthropic(t, func(w http.ResponseWriter) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write(body)
	})
	sdk := newSDK(t, server.URL, nil)

	resp, err := sdk.LLM().GenerateWithTools(context.Background(), llm.GenerateWithToolsRequest{
		Messages: []llm.Message{{Role: "user", Content: []llm.ContentBlock{{Type: "text", Text: "Weather in Bern?"}}}},
		Tools: []llm.Tool{{
			Name:        "get_weather",
			Description: "Current weather for a city",
			InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}},
		}},
	})
	if err != nil {
		t.Fatalf("GenerateWithTools: %v", err)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.ToolUses) != 1 {
		t.Fatalf("ToolUses = %d, want 1", len(resp.ToolUses))
	}
	tu := resp.ToolUses[0]
	if tu.ID != "toolu_1" || tu.Name != "get_weather" {
		t.Errorf("ToolUse = %+v, want id toolu_1, name get_weather", tu)
	}
	if got := tu.Input["city"]; got != "Bern" {
		t.Errorf("ToolUse.Input[city] = %v, want Bern", got)
	}
	if got := fake.last(t).Body.Model; got != testModel {
		t.Errorf("model = %q, want %q", got, testModel)
	}
}

// keywordEmbedding gives each topic its own axis, so the document that
// shares the query's topic scores highest and no two documents tie.
func keywordEmbedding(text string) []float32 {
	v := []float32{0.01, 0.01, 0.01, 0.01}
	lower := strings.ToLower(text)
	for i, kw := range []string{"kubernetes", "postgres", "redis", "incident"} {
		if strings.Contains(lower, kw) {
			v[i] = 1
		}
	}
	return v
}

func newFakeEmbeddings(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("embedding path = %q, want /v1/embeddings", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testEmbedKey {
			t.Errorf("Authorization header does not carry the configured key")
		}
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode embedding request: %v", err)
		}
		if req.Model != testEmbedModel {
			t.Errorf("embedding model = %q, want %q", req.Model, testEmbedModel)
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		}
		data := make([]item, len(req.Input))
		for i, in := range req.Input {
			data[i] = item{Index: i, Embedding: keywordEmbedding(in)}
		}
		w.Header().Set("content-type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{"data": data}); err != nil {
			t.Errorf("encode embedding response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func ragConfig(baseURL string) *rag.Config {
	return &rag.Config{EmbeddingProvider: "openai", APIKey: testEmbedKey, Model: testEmbedModel, BaseURL: baseURL}
}

func TestSDK_RAG(t *testing.T) {
	ctx := context.Background()
	sdk := newSDK(t, "http://127.0.0.1:0", ragConfig(newFakeEmbeddings(t).URL))
	kb := sdk.RAG()
	if kb == nil {
		t.Fatal("RAG() is nil although Config.RAG is set")
	}

	err := kb.AddDocuments(ctx, []rag.Document{
		{ID: "doc-k8s", Content: "Deploying services on Kubernetes", Metadata: map[string]string{"title": "K8s guide"}},
		{ID: "doc-db", Content: "Tuning PostgreSQL connection pools"},
		{ID: "doc-cache", Content: "Redis eviction policies"},
	})
	if err != nil {
		t.Fatalf("AddDocuments: %v", err)
	}
	if err := kb.AddDocument(ctx, "doc-incident", "Incident response runbook", nil); err != nil {
		t.Fatalf("AddDocument: %v", err)
	}
	if n, err := kb.Count(ctx); err != nil || n != 4 {
		t.Fatalf("Count = %d, %v; want 4, nil", n, err)
	}

	retrieveTests := []struct {
		query string
		want  string
	}{
		{"how do I deploy to kubernetes?", "doc-k8s"},
		{"postgres is slow", "doc-db"},
		{"redis memory full", "doc-cache"},
		{"who handles an incident?", "doc-incident"},
	}
	for _, tt := range retrieveTests {
		t.Run("Retrieve/"+tt.want, func(t *testing.T) {
			resp, err := kb.Retrieve(ctx, rag.RetrieveRequest{Query: tt.query, TopK: 4})
			if err != nil {
				t.Fatalf("Retrieve: %v", err)
			}
			if len(resp.Results) == 0 {
				t.Fatal("Retrieve returned no results")
			}
			if got := resp.Results[0].Document.ID; got != tt.want {
				t.Errorf("top result = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("Query", func(t *testing.T) {
		got, err := kb.Query(ctx, "kubernetes rollout", 1)
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if !strings.Contains(got, "Deploying services on Kubernetes") || !strings.Contains(got, "Title: K8s guide") {
			t.Errorf("Query context does not contain the Kubernetes document:\n%s", got)
		}
	})
}

func TestSDK_RAG_EmbeddingError(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"upstream down"}}`, http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)

	ctx := context.Background()
	kb := newSDK(t, "http://127.0.0.1:0", ragConfig(failing.URL)).RAG()

	calls := []struct {
		name string
		call func() error
	}{
		{"AddDocument", func() error { return kb.AddDocument(ctx, "a", "text", nil) }},
		{"AddDocuments", func() error { return kb.AddDocuments(ctx, []rag.Document{{ID: "a", Content: "text"}}) }},
		{"Retrieve", func() error { _, err := kb.Retrieve(ctx, rag.RetrieveRequest{Query: "q"}); return err }},
		{"Query", func() error { _, err := kb.Query(ctx, "q", 1); return err }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatal("expected embedding endpoint error, got nil")
			}
			if !strings.Contains(err.Error(), "500") {
				t.Errorf("error %q does not carry the endpoint status", err)
			}
		})
	}
}
