package model

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// embedServer answers like Ollama's /api/embed or OpenAI's /embeddings:
// one 3-dimensional vector per input, whose first value is the input's
// length, and records every request body.
func embedServer(t *testing.T, openAI bool) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		inputs, _ := body["input"].([]any)
		w.Header().Set("Content-Type", "application/json")
		if openAI {
			assert.Equal(t, "/v1/embeddings", r.URL.Path)
			assert.Equal(t, "Bearer key-must-not-leak", r.Header.Get("Authorization"))
			type item struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			}
			var data []item
			// Reverse order: the client must sort by index.
			for k := len(inputs) - 1; k >= 0; k-- {
				data = append(data, item{k, []float32{float32(len(inputs[k].(string))), 0, 1}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]any{"prompt_tokens": 7 * len(inputs)}})
			return
		}
		assert.Equal(t, "/api/embed", r.URL.Path)
		var vecs [][]float32
		for _, in := range inputs {
			vecs = append(vecs, []float32{float32(len(in.(string))), 0, 1})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": vecs, "prompt_eval_count": 5 * len(inputs)})
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestEmbedders_VectorsInOrderAndBatched(t *testing.T) {
	texts := make([]string, embedBatch+3)
	for k := range texts {
		texts[k] = strings.Repeat("x", k+1)
	}
	cases := map[string]struct {
		openAI bool
		new    func(url string) Embedder
		perIn  int64
	}{
		"ollama": {false, func(url string) Embedder { return NewOllamaEmbedder(url, "nomic-embed-text") }, 5},
		"openai": {true, func(url string) Embedder { return NewOpenAIEmbedder("key-must-not-leak", url+"/v1", "text-embedding-3-small") }, 7},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv, got := embedServer(t, tc.openAI)
			emb, err := tc.new(srv.URL).Embed(context.Background(), texts)
			require.NoError(t, err)
			require.Len(t, emb.Vectors, len(texts))
			for k, v := range emb.Vectors {
				assert.Equal(t, float32(k+1), v[0], "vector %d is not the one for text %d", k, k)
			}
			assert.Len(t, *got, 2, "%d texts go in two requests of at most %d", len(texts), embedBatch)
			assert.Equal(t, tc.perIn*int64(len(texts)), emb.InputTokens)
		})
	}
}

func TestEmbedders_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"model \"nope\" not found, try pulling it first"}`))
	}))
	t.Cleanup(srv.Close)
	_, err := NewOllamaEmbedder(srv.URL, "nope").Embed(context.Background(), []string{"a"})
	require.Error(t, err)
	assert.Equal(t, NotFound, AsCallError(err).Class)

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[[1,2,3]]}`))
	}))
	t.Cleanup(short.Close)
	_, err = NewOllamaEmbedder(short.URL, "m").Embed(context.Background(), []string{"a", "b"})
	require.Error(t, err, "one vector for two texts")
	assert.Equal(t, Bad, AsCallError(err).Class)
}

// A secret in a document is removed before the provider sees it, the
// audit rows carry provider, model, input tokens and the redaction count,
// and the call is counted.
func TestInstrumentEmbedder_RedactsAuditsAndCounts(t *testing.T) {
	srv, got := embedServer(t, false)
	a := &recordingAudit{}
	ok0 := series(t, `innominatus_ai_model_calls_total{component="embed_test",provider="ollama",outcome="ok"}`)
	red0 := series(t, `innominatus_ai_prompt_redactions_total{component="embed_test"}`)
	e := InstrumentEmbedder(NewOllamaEmbedder(srv.URL, "nomic-embed-text"), "embed_test", WithAudit(a))

	emb, err := e.Embed(context.Background(), []string{"connect with password=hunter2-very-secret", "plain text"})
	require.NoError(t, err)
	require.Len(t, emb.Vectors, 2)
	assert.Equal(t, 1, emb.Redactions)

	sent, _ := json.Marshal((*got)[0]["input"])
	assert.NotContains(t, string(sent), "hunter2")
	require.Len(t, a.started, 1)
	require.Len(t, a.ended, 1)
	end := a.ended[0]
	assert.Equal(t, "ollama", end.Provider)
	assert.Equal(t, "nomic-embed-text", end.Model)
	assert.Equal(t, int64(10), end.TokensIn)
	assert.Zero(t, end.TokensOut)
	assert.Equal(t, 1, end.RedactionCount)
	assert.Equal(t, "ok", end.Outcome)
	assert.Len(t, end.PromptHash, 64)
	assert.Equal(t, int64(1), series(t, `innominatus_ai_model_calls_total{component="embed_test",provider="ollama",outcome="ok"}`)-ok0)
	assert.Equal(t, int64(1), series(t, `innominatus_ai_prompt_redactions_total{component="embed_test"}`)-red0)
}

// A provider the allowlist refuses never sees a document, gets no audit
// row, and the guard reports the refusal.
func TestInstrumentEmbedder_RefusedProviderSendsNothing(t *testing.T) {
	srv, got := embedServer(t, false)
	a := &recordingAudit{}
	events := &recordedAudit{}
	g := GuardEmbedder(NewOllamaEmbedder(srv.URL, "nomic-embed-text"), ProviderAllowlist{}, srv.URL, false, events.report)
	e := InstrumentEmbedder(g, "embed_test", WithAudit(a))

	_, err := e.Embed(context.Background(), []string{"internal runbook"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderNotAllowed))
	assert.Empty(t, *got, "the provider got a request")
	assert.Empty(t, a.started)
	require.Len(t, events.events, 1)
	assert.Equal(t, ProviderRefused, events.events[0].Kind)
	assert.Equal(t, "embed_test", events.events[0].Component)
}

// A failed audit start row means no call.
func TestInstrumentEmbedder_AuditFailureStopsTheCall(t *testing.T) {
	srv, got := embedServer(t, false)
	e := InstrumentEmbedder(NewOllamaEmbedder(srv.URL, "m"), "embed_test", WithAudit(&recordingAudit{startErr: errors.New("db down")}))
	_, err := e.Embed(context.Background(), []string{"x"})
	require.Error(t, err)
	assert.True(t, AsCallError(err).AuditFailed)
	assert.Empty(t, *got)
}

// TestOllamaEmbedder_Live embeds two texts with a real Ollama. Set
// MODEL_LIVE_OLLAMA_EMBED to a pulled embedding model (nomic-embed-text).
func TestOllamaEmbedder_Live(t *testing.T) {
	name := os.Getenv("MODEL_LIVE_OLLAMA_EMBED")
	if name == "" {
		t.Skip("set MODEL_LIVE_OLLAMA_EMBED=<model> to call a local Ollama")
	}
	base := os.Getenv("OLLAMA_BASE_URL")
	if base == "" {
		base = DefaultOllamaBaseURL
	}
	a := &recordingAudit{}
	e := InstrumentEmbedder(NewOllamaEmbedder(base, name), "embed_live", WithAudit(a))
	emb, err := e.Embed(context.Background(), []string{"How do I deploy a postgres database?", "Provision a PostgreSQL instance", "The cat sleeps on the sofa."})
	require.NoError(t, err)
	require.Len(t, emb.Vectors, 3)
	sim := func(x, y []float32) float64 {
		var dot, nx, ny float64
		for k := range x {
			dot += float64(x[k] * y[k])
			nx += float64(x[k] * x[k])
			ny += float64(y[k] * y[k])
		}
		return dot / math.Sqrt(nx*ny)
	}
	near, far := sim(emb.Vectors[0], emb.Vectors[1]), sim(emb.Vectors[0], emb.Vectors[2])
	t.Logf("dims=%d tokens=%d near=%.3f far=%.3f", len(emb.Vectors[0]), emb.InputTokens, near, far)
	assert.Greater(t, near, far, "related texts must be closer than unrelated ones")
	require.Len(t, a.ended, 1)
	assert.Equal(t, "ok", a.ended[0].Outcome)
}
