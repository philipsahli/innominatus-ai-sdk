package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data only: these recordings are written by the tests below, never
// by a live model. Real recordings come from a record run (docs:
// ai-provider-allowlist.md, "Recorded answers").

var replayReq = Request{
	System: "you plan stacks",
	Turns:  []Turn{{Role: "user", Text: "a web shop with a database"}},
	Schema: json.RawMessage(`{"type":"object"}`),
}

// writeRecording writes one recording for req (already redacted) under dir.
func writeRecording(t *testing.T, dir, version string, req Request, text string) string {
	t.Helper()
	rec := newRecording(version, req, "anthropic", "claude-test", Reply{Text: text, Stop: StopEnd, InputTokens: 120, OutputTokens: 30}, time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC))
	path, err := writeRecordingFile(dir, rec)
	require.NoError(t, err)
	return path
}

func TestReplay_Hit_ReturnsRecordedTextWithZeroTokens(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "compose-v1", replayReq, `{"resources":[]}`)
	m, err := NewReplay(dir)
	require.NoError(t, err)

	rep, err := m.Call(WithPromptVersion(context.Background(), "compose-v1"), replayReq)
	require.NoError(t, err)
	assert.Equal(t, `{"resources":[]}`, rep.Text)
	assert.Equal(t, StopEnd, rep.Stop)
	assert.Zero(t, rep.InputTokens, "a replayed call spends no tokens")
	assert.Zero(t, rep.OutputTokens)

	provider, model := m.Name()
	assert.Equal(t, ProviderReplay, provider)
	assert.Equal(t, "claude-test", model)
}

func TestReplay_Miss_IsFinalNotFoundNamingTheKey(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "compose-v1", replayReq, `{}`)
	m, err := NewReplay(dir)
	require.NoError(t, err)

	other := replayReq
	other.Turns = []Turn{{Role: "user", Text: "something nobody recorded"}}
	_, err = m.Call(WithPromptVersion(context.Background(), "compose-v1"), other)
	require.Error(t, err)
	ce := AsCallError(err)
	assert.Equal(t, NotFound, ce.Class, "NotFound is final: no retry")
	assert.Contains(t, ce.Msg, "no recorded answer for this request (prompt compose-v1, hash "+promptHash(other)[:12])
	assert.Contains(t, ce.Msg, "starter requests only")

	// Same request, other prompt version: also a miss.
	_, err = m.Call(WithPromptVersion(context.Background(), "compose-v2"), replayReq)
	assert.Equal(t, NotFound, AsCallError(err).Class)
}

// Recordings hold text turns only: a request with tools or tool blocks
// (the chat assistant's loop) is refused as Bad by Instrument, never
// answered from a text recording, and record mode passes it through
// unrecorded.
func TestReplay_ToolRequestIsRefusedAndNotRecorded(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "compose-v1", replayReq, `{}`)
	backend, err := NewReplay(dir)
	require.NoError(t, err)
	m := Instrument(backend, "replay_test")
	ctx := WithPromptVersion(context.Background(), "compose-v1")

	withTools := replayReq
	withTools.Tools = []ToolDef{{Name: "list_apps"}}
	withBlocks := replayReq
	withBlocks.Turns = []Turn{{Role: "user", Text: "a web shop with a database", Blocks: []Block{{Kind: BlockToolResult, ID: "t1", Content: "[]"}}}}
	for _, req := range []Request{withTools, withBlocks} {
		_, err := m.Call(ctx, req)
		require.Error(t, err)
		assert.Equal(t, Bad, AsCallError(err).Class)
	}

	recDir := t.TempDir()
	rep, err := newRecorder(stubModel{rep: Reply{Text: "ok"}}, recDir).Call(ctx, withTools)
	require.NoError(t, err)
	assert.Equal(t, "ok", rep.Text)
	entries, err := os.ReadDir(recDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestReplay_BadRecordingRefusesTheStart(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"not JSON": func(t *testing.T, dir string) {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "compose-v1"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "compose-v1", strings.Repeat("a", 64)+".json"), []byte("{"), 0o600))
		},
		"wrong v": func(t *testing.T, dir string) {
			p := writeRecording(t, dir, "compose-v1", replayReq, `{}`)
			rewrite(t, p, func(r map[string]any) { r["v"] = 2 })
		},
		"hash does not match the file name": func(t *testing.T, dir string) {
			p := writeRecording(t, dir, "compose-v1", replayReq, `{}`)
			require.NoError(t, os.Rename(p, filepath.Join(filepath.Dir(p), strings.Repeat("b", 64)+".json")))
		},
		"version does not match the folder": func(t *testing.T, dir string) {
			p := writeRecording(t, dir, "compose-v1", replayReq, `{}`)
			rewrite(t, p, func(r map[string]any) { r["prompt_version"] = "compose-v9" })
		},
		"request edited after recording": func(t *testing.T, dir string) {
			p := writeRecording(t, dir, "compose-v1", replayReq, `{}`)
			rewrite(t, p, func(r map[string]any) { r["request"].(map[string]any)["system"] = "edited" })
		},
		"empty folder": func(*testing.T, string) {},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			setup(t, dir)
			_, err := NewReplay(dir)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "replay dir")
		})
	}
}

func rewrite(t *testing.T, path string, edit func(map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var r map[string]any
	require.NoError(t, json.Unmarshal(b, &r))
	edit(r)
	b, err = json.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))
}

// The audit trail sees provider replay, the recorded model, the same
// prompt hash as the file name, 0 tokens and outcome ok; a miss is
// not_found.
func TestReplay_Instrumented_AuditRows(t *testing.T) {
	dir := t.TempDir()
	path := writeRecording(t, dir, "compose-v1", replayReq, `{"ok":true}`)
	backend, err := NewReplay(dir)
	require.NoError(t, err)
	a := &recordingAudit{}
	m := Instrument(backend, "instrument_test", WithAudit(a))
	ctx := WithPromptVersion(context.Background(), "compose-v1")

	_, err = m.Call(ctx, replayReq)
	require.NoError(t, err)
	require.Len(t, a.ended, 1)
	e := a.ended[0]
	assert.Equal(t, ProviderReplay, e.Provider)
	assert.Equal(t, "claude-test", e.Model)
	assert.Equal(t, strings.TrimSuffix(filepath.Base(path), ".json"), e.PromptHash)
	assert.Equal(t, "ok", e.Outcome)
	assert.Zero(t, e.TokensIn)
	assert.Zero(t, e.TokensOut)

	other := replayReq
	other.Turns = []Turn{{Role: "user", Text: "not recorded"}}
	_, err = m.Call(ctx, other)
	require.Error(t, err)
	require.Len(t, a.ended, 2)
	assert.Equal(t, "not_found", a.ended[1].Outcome)
}

// A secret in the request and one the answer echoes are both redacted in
// the file; the file name is the hash of the redacted request, the one the
// audit row carries.
func TestRecord_WritesRedactedRequestAndAnswer(t *testing.T) {
	const secret = "token=abcdefghijklmnop0123"
	dir := t.TempDir()
	live := stubModel{rep: Reply{Text: `{"note":"` + secret + `"}`, InputTokens: 7, OutputTokens: 3}}
	a := &recordingAudit{}
	m := Instrument(Guard(newRecorder(live, dir), ProviderAllowlist{}, "", true, nil), "instrument_test", WithAudit(a))
	req := Request{System: "plan", Turns: []Turn{{Role: "user", Text: "deploy with " + secret}}}

	rep, err := m.Call(WithPromptVersion(context.Background(), "compose-v1"), req)
	require.NoError(t, err)
	assert.Equal(t, int64(7), rep.InputTokens, "a record run spends real tokens")

	require.Len(t, a.started, 1)
	path := filepath.Join(dir, "compose-v1", a.started[0].PromptHash+".json")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "abcdefghijklmnop0123")

	var rec recording
	require.NoError(t, json.Unmarshal(b, &rec))
	assert.Equal(t, 1, rec.V)
	assert.Equal(t, "stub", rec.Provider)
	assert.Equal(t, int64(7), rec.TokensIn, "the recorded counts stay in the file")
	assert.Contains(t, rec.Request.Turns[0].Text, "deploy with")

	// The file replays.
	rp, err := NewReplay(dir)
	require.NoError(t, err)
	got, err := rp.Call(WithPromptVersion(context.Background(), "compose-v1"), mustRedact(req))
	require.NoError(t, err)
	assert.Equal(t, rec.Text, got.Text)
}

func mustRedact(req Request) Request {
	r, _ := RedactRequest(req)
	return r
}

func TestRecord_SkipsFailedAndCutAnswers(t *testing.T) {
	dir := t.TempDir()
	for _, live := range []Model{
		stubModel{err: &CallError{Class: Busy, Msg: "overloaded"}},
		stubModel{rep: Reply{Text: "{", Stop: StopMaxTokens}},
	} {
		_, _ = newRecorder(live, dir).Call(WithPromptVersion(context.Background(), "compose-v1"), replayReq)
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestRecord_RefusesAPathInThePromptVersion(t *testing.T) {
	dir := t.TempDir()
	live := stubModel{rep: Reply{Text: "{}"}}
	_, err := newRecorder(live, dir).Call(WithPromptVersion(context.Background(), "../escape"), replayReq)
	require.Error(t, err)
	_, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "escape"))
	assert.True(t, os.IsNotExist(statErr))
}

// Replay writes no ai.provider.unchecked_used event: that event means a
// prompt went to a provider whose contract nobody checked.
func TestGuard_ReplayWritesNoUncheckedEvent(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "compose-v1", replayReq, `{}`)
	rp, err := NewReplay(dir)
	require.NoError(t, err)
	log := &recordedAudit{}
	g := Guard(rp, ProviderAllowlist{}, "", true, log.report)
	_, err = g.Call(WithPromptVersion(context.Background(), "compose-v1"), replayReq)
	require.NoError(t, err)
	assert.Empty(t, log.events)
}

// Round trip: record against an OpenAI-compatible fake (NewRecorder +
// Instrument), then replay the folder with no network; the audit
// prompt_hash names the file both times.
func TestReplay_RoundTrip(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"plan\":1}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":2}}`))
	}))
	defer srv.Close()
	dir := t.TempDir()
	ctx := WithPromptVersion(context.Background(), "compose-v1")

	live := NewRecorder(NewOpenAI("", srv.URL, "fake-model"), dir)
	a1 := &recordingAudit{}
	_, err := Instrument(live, "instrument_test", WithAudit(a1)).Call(ctx, replayReq)
	require.NoError(t, err)
	require.Equal(t, int32(1), hits.Load())

	rp, err := NewReplay(dir)
	require.NoError(t, err)
	a2 := &recordingAudit{}
	rep, err := Instrument(rp, "instrument_test", WithAudit(a2)).Call(ctx, replayReq)
	require.NoError(t, err)
	assert.Equal(t, `{"plan":1}`, rep.Text)
	assert.Equal(t, int32(1), hits.Load(), "replay never reaches the network")
	assert.Equal(t, a1.started[0].PromptHash, a2.started[0].PromptHash)
	_, err = os.Stat(filepath.Join(dir, "compose-v1", a2.started[0].PromptHash+".json"))
	require.NoError(t, err)
	assert.Equal(t, "fake-model", a2.started[0].Model)
}
