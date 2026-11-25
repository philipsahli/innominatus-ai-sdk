package model

import (
	"context"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveStub counts its calls; it stands for a live backend that runs
// tools (anthropic) under the recorder.
type liveStub struct{ calls int }

func (s *liveStub) Call(context.Context, Request) (Reply, error) {
	s.calls++
	return Reply{Text: "ok", InputTokens: 10, OutputTokens: 2}, nil
}
func (s *liveStub) Name() (string, string) { return ProviderAnthropic, "claude-test" }

// A tool request to a backend that cannot run tools is refused before the
// audit start row: Bad, outcome tools_unsupported, no trail row. The same
// backend's text request is still audited.
func TestInstrument_ToolRequestToToolessBackend_NoAuditRow(t *testing.T) {
	dir := t.TempDir()
	writeRecording(t, dir, "compose-v1", replayReq, `{"ok":true}`)
	replayBackend, err := NewReplay(dir)
	require.NoError(t, err)
	// Guarded like Config.Backend wraps it, so Instrument looks through
	// the guard.
	guardedReplay := Guard(replayBackend, ProviderAllowlist{}, "", true, nil)
	creds := credentials.NewStaticCredentialsProvider("AKID", "secret", "")
	backends := map[string]Model{
		ProviderReplay:  guardedReplay,
		ProviderBedrock: NewBedrock(creds, "eu-central-1", "http://127.0.0.1:1", "eu.anthropic.claude-test"),
		ProviderCopilot: NewCopilot("", "/nonexistent/copilot", "auto"),
	}
	withTools := replayReq
	withTools.Tools = []ToolDef{{Name: "list_apps"}}
	withBlocks := replayReq
	withBlocks.Turns = []Turn{{Role: "user", Text: "x", Blocks: []Block{{Kind: BlockToolResult, ID: "t1", Content: "[]"}}}}
	ctx := WithPromptVersion(context.Background(), "compose-v1")

	for provider, backend := range backends {
		t.Run(provider, func(t *testing.T) {
			a := &recordingAudit{}
			m := Instrument(backend, "tools_test", WithAudit(a))
			sample := `innominatus_ai_model_calls_total{component="tools_test",provider="` + provider + `",outcome="tools_unsupported"}`
			before := series(t, sample)
			for _, req := range []Request{withTools, withBlocks} {
				_, err := m.Call(ctx, req)
				require.Error(t, err)
				ce := AsCallError(err)
				assert.Equal(t, Bad, ce.Class, "same class as before: callers' handling does not change")
				assert.Equal(t, FailureModel, Classify(err).Kind)
				assert.Contains(t, ce.Msg, "the "+provider+" backend does not support tool use")
			}
			assert.Empty(t, a.started, "no CallStarted for a refused tool request")
			assert.Empty(t, a.ended)
			assert.Equal(t, int64(2), series(t, sample)-before)
		})
	}

	// A text request to the same replay backend is still audited.
	a := &recordingAudit{}
	_, err = Instrument(guardedReplay, "tools_test", WithAudit(a)).Call(ctx, replayReq)
	require.NoError(t, err)
	require.Len(t, a.started, 1)
	require.Len(t, a.ended, 1)
	assert.Equal(t, "ok", a.ended[0].Outcome)
}

// Record mode: the recorder answers for its live backend, so a tool request
// passes Instrument, reaches the live model, is audited and writes no file.
func TestInstrument_RecordMode_ToolRequestGoesLiveUnrecorded(t *testing.T) {
	live := &liveStub{}
	recDir := t.TempDir()
	a := &recordingAudit{}
	m := Instrument(Guard(newRecorder(live, recDir), ProviderAllowlist{}, "", true, nil), "tools_test", WithAudit(a))
	withTools := replayReq
	withTools.Tools = []ToolDef{{Name: "list_apps"}}

	rep, err := m.Call(WithPromptVersion(context.Background(), "compose-v1"), withTools)
	require.NoError(t, err)
	assert.Equal(t, "ok", rep.Text)
	assert.Equal(t, 1, live.calls)
	assert.Len(t, a.started, 1)
	entries, err := os.ReadDir(recDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// A caller that skips Instrument (Redacting only) gets the same refusal.
func TestRedacting_ToolRequestToToolessBackend_Refused(t *testing.T) {
	creds := credentials.NewStaticCredentialsProvider("AKID", "secret", "")
	m := Redacting(NewBedrock(creds, "eu-central-1", "http://127.0.0.1:1", "eu.anthropic.claude-test"))
	req := replayReq
	req.Tools = []ToolDef{{Name: "list_apps"}}
	_, err := m.Call(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, Bad, AsCallError(err).Class)
	assert.False(t, SupportsTools(m))
	assert.True(t, SupportsTools(stubModel{}), "a Model without ToolCapable runs tools")
}
