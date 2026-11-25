package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseAnthropic answers the streaming Messages API with pieces as text
// deltas, and records the request body.
func sseAnthropic(t *testing.T, pieces ...string) (*httptest.Server, *[]byte) {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		send := func(event string, data any) {
			b, _ := json.Marshal(data)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		}
		send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-test", "content": []any{},
			"stop_reason": nil, "usage": map[string]any{"input_tokens": 12, "output_tokens": 0},
		}})
		send("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
		for _, p := range pieces {
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": p}})
		}
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
		send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 5}})
		send("message_stop", map[string]any{"type": "message_stop"})
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

func TestAnthropicStream_DeltasInOrderAndWholeReply(t *testing.T) {
	srv, body := sseAnthropic(t, "Hello", ", ", "world")
	var got []string
	rep, err := CallStreaming(context.Background(), NewAnthropic("k", srv.URL, "claude-test"),
		Request{Turns: []Turn{{Role: "user", Text: "hi"}}, MaxTokens: 64}, func(s string) { got = append(got, s) })
	require.NoError(t, err)
	assert.Equal(t, []string{"Hello", ", ", "world"}, got)
	assert.Equal(t, "Hello, world", rep.Text)
	assert.Equal(t, StopEnd, rep.Stop)
	assert.Equal(t, int64(12), rep.InputTokens)
	assert.Equal(t, int64(5), rep.OutputTokens)
	assert.Contains(t, string(*body), `"stream":true`)
}

// An error status is an error, not an empty answer: the CLI's old client
// read a 401 body as a stream of nothing.
func TestAnthropicStream_ErrorStatusIsAnError(t *testing.T) {
	for status, class := range map[int]Class{http.StatusUnauthorized: Refused, http.StatusTooManyRequests: Busy, http.StatusInternalServerError: Busy} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"type":"error","error":{"type":"x","message":"nope"}}`))
			}))
			t.Cleanup(srv.Close)
			called := false
			_, err := CallStreaming(context.Background(), NewAnthropic("k", srv.URL, "m"), Request{Turns: []Turn{{Role: "user", Text: "hi"}}, MaxTokens: 8}, func(string) { called = true })
			require.Error(t, err)
			assert.Equal(t, class, AsCallError(err).Class)
			assert.False(t, called)
		})
	}
}

// A streamed call through Instrument is redacted, audited and counted like
// Call.
func TestInstrumentStream_RedactsAndAudits(t *testing.T) {
	srv, body := sseAnthropic(t, "ok")
	a := &recordingAudit{}
	ok0 := series(t, `innominatus_ai_model_calls_total{component="stream_test",provider="anthropic",outcome="ok"}`)
	m := Instrument(NewAnthropic("k", srv.URL, "claude-test"), "stream_test", WithAudit(a))
	var got strings.Builder
	rep, err := CallStreaming(context.Background(), m, Request{Turns: []Turn{{Role: "user", Text: "deploy with password=hunter2-very-secret"}}, MaxTokens: 8}, func(s string) { got.WriteString(s) })
	require.NoError(t, err)
	assert.Equal(t, "ok", got.String())
	assert.Equal(t, 1, rep.Redactions)
	assert.NotContains(t, string(*body), "hunter2")
	require.Len(t, a.ended, 1)
	assert.Equal(t, int64(12), a.ended[0].TokensIn)
	assert.Equal(t, "ok", a.ended[0].Outcome)
	assert.Equal(t, int64(1), series(t, `innominatus_ai_model_calls_total{component="stream_test",provider="anthropic",outcome="ok"}`)-ok0)
}

// A refused provider streams nothing and sends nothing.
func TestGuardStream_RefusedSendsNothing(t *testing.T) {
	srv, body := sseAnthropic(t, "never")
	m := Guard(NewAnthropic("k", srv.URL, "claude-test"), ProviderAllowlist{}, srv.URL, false, nil)
	_, err := CallStreaming(context.Background(), m, Request{Turns: []Turn{{Role: "user", Text: "hi"}}}, func(string) { t.Error("streamed text from a refused provider") })
	require.ErrorIs(t, err, ErrProviderNotAllowed)
	assert.Empty(t, *body)
}

// A backend that cannot stream hands its whole answer to onText once.
func TestCallStreaming_FallsBackToCall(t *testing.T) {
	var got []string
	rep, err := CallStreaming(context.Background(), stubModel{rep: Reply{Text: "whole"}}, Request{}, func(s string) { got = append(got, s) })
	require.NoError(t, err)
	assert.Equal(t, []string{"whole"}, got)
	assert.Equal(t, "whole", rep.Text)
}

// An error event in the middle of the stream is an error, after the text
// that came before it.
func TestAnthropicStream_ErrorEventMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"m\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"part\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n")
	}))
	t.Cleanup(srv.Close)
	var got string
	_, err := CallStreaming(context.Background(), NewAnthropic("k", srv.URL, "m"), Request{Turns: []Turn{{Role: "user", Text: "hi"}}, MaxTokens: 8}, func(s string) { got += s })
	require.Error(t, err)
	assert.Equal(t, "part", got)
	assert.Equal(t, Busy, AsCallError(err).Class, "overloaded is busy, not final")
	assert.Equal(t, "Overloaded", AsCallError(err).Msg)
}
