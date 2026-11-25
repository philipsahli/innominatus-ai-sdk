package model

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/redact/redacttest"
)

// loadCorpus returns the secret corpus with the "<cut>" markers removed.
func loadCorpus(t *testing.T) []redacttest.Entry {
	t.Helper()
	entries, err := redacttest.Corpus()
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	return entries
}

// A tool result is where a secret enters the chat's prompt (resources,
// workflow logs read as the user). Every corpus secret placed in one is
// gone before the backend sees the request, and counted.
func TestRedactRequest_ToolResultFromCorpus(t *testing.T) {
	for _, e := range loadCorpus(t) {
		t.Run(e.Kind, func(t *testing.T) {
			req := Request{Turns: []Turn{
				{Role: "user", Text: "why did workflow 42 fail?"},
				{Role: "assistant", Blocks: []Block{{Kind: BlockToolUse, ID: "t1", Name: "get_workflow_logs", Input: json.RawMessage(`{"id":42}`)}}},
				{Role: "user", Blocks: []Block{{Kind: BlockToolResult, ID: "t1", Content: "step 3 failed:\n" + e.Text}}},
			}}
			out, n := RedactRequest(req)
			got := out.Turns[2].Blocks[0].Content
			for _, s := range e.Secrets {
				assert.NotContains(t, got, s)
			}
			assert.Positive(t, n)
			assert.Contains(t, got, "step 3 failed:", "only the secret goes")
			assert.Contains(t, req.Turns[2].Blocks[0].Content, e.Secrets[0], "the caller's copy (kept for grounding) is not changed")
		})
	}
}

// A tool input is re-sent on every later iteration: its string values are
// redacted too, its shape stays valid JSON.
func TestRedactRequest_ToolUseInputStrings(t *testing.T) {
	req := Request{Turns: []Turn{{Role: "assistant", Blocks: []Block{{
		Kind: BlockToolUse, ID: "t1", Name: "search",
		Input: json.RawMessage(`{"query":"password=hunter2-very-secret","limit":5,"nested":{"k":["AKIAIOSFODNN7EXAMPLE"]}}`),
	}}}}}
	out, n := RedactRequest(req)
	in := out.Turns[0].Blocks[0].Input
	assert.True(t, json.Valid(in), string(in))
	assert.NotContains(t, string(in), "hunter2-very-secret")
	assert.NotContains(t, string(in), "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, string(in), `"limit":5`)
	assert.Equal(t, 2, n)
}

func TestRedactRequest_ToolDefinitionsStay(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"token":{"type":"string","description":"token=abcdef0123456789 example"}}}`)
	out, _ := RedactRequest(Request{Tools: []ToolDef{{Name: "t", Description: "d", InputSchema: schema}}})
	assert.Equal(t, string(schema), string(out.Tools[0].InputSchema), "tool definitions are the server's own, like the schema")
}

// Callers that send text only (ai_jobs, compose) keep their prompt hash:
// a turn without blocks encodes as before.
func TestPromptHash_TextOnlyUnchanged(t *testing.T) {
	b, err := json.Marshal(Turn{Role: "user", Text: "hi"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"Role":"user","Text":"hi"}`, string(b))

	withTools := Request{Turns: []Turn{{Role: "user", Text: "hi"}}, Tools: []ToolDef{{Name: "list_apps"}}}
	assert.NotEqual(t, promptHash(Request{Turns: withTools.Turns}), promptHash(withTools), "the tools are part of what was sent")
}

// One audited row pair per model call: a tool loop of three calls writes
// six rows, all with the trace ID of the turn.
func TestInstrument_ToolLoop_RowPairPerCall(t *testing.T) {
	a := &recordingAudit{}
	m := Instrument(&toolLoopModel{}, "chat", WithAudit(a))
	req := Request{Tools: []ToolDef{{Name: "list_apps"}}, Turns: []Turn{{Role: "user", Text: "list my apps"}}}
	for i := 0; i < 3; i++ {
		rep, err := m.Call(context.Background(), req)
		require.NoError(t, err)
		req.Turns = append(req.Turns, Turn{Role: "assistant", Blocks: toolUseBlocks(rep.ToolUses)})
	}
	assert.Len(t, a.started, 3)
	assert.Len(t, a.ended, 3)
	for _, c := range a.ended {
		require.NotNil(t, c.Valid)
		assert.True(t, *c.Valid, "a tool-use answer with no schema is a valid answer")
	}
}

type toolLoopModel struct{ n int }

func (m *toolLoopModel) Call(context.Context, Request) (Reply, error) {
	m.n++
	return Reply{ToolUses: []ToolUse{{ID: "t" + string(rune('0'+m.n)), Name: "list_apps", Input: json.RawMessage(`{}`)}}}, nil
}
func (m *toolLoopModel) Name() (string, string) { return "stub", "stub-1" }

func toolUseBlocks(uses []ToolUse) []Block {
	out := make([]Block, len(uses))
	for i, u := range uses {
		out[i] = Block{Kind: BlockToolUse, ID: u.ID, Name: u.Name, Input: u.Input}
	}
	return out
}
