package model_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

// TestModels_RefuseRedirects: a redirect from the configured base URL must
// not carry the prompt (or the key) to another host.
func TestModels_RefuseRedirects(t *testing.T) {
	var hit atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Add(1) }))
	t.Cleanup(other.Close)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirect.Close)

	for _, m := range []model.Model{model.NewAnthropic(apiKey, redirect.URL, claudeM), model.NewOllama(redirect.URL, ollamaM), model.NewOpenAI(openAIKey, redirect.URL, gptM), newBedrock(redirect.URL)} {
		provider, _ := m.Name()
		_, err := m.Call(context.Background(), model.Request{Turns: []model.Turn{{Role: "user", Text: "hi"}}, Schema: sizingSchema, MaxTokens: 10})
		require.Error(t, err, provider)
		ce := model.AsCallError(err)
		assert.Equal(t, model.Bad, ce.Class, "%s: a redirect is final, not busy: %v", provider, ce)
		assert.NotContains(t, ce.Error(), apiKey)
	}
	assert.Zero(t, hit.Load(), "the redirect target got no request")
}
