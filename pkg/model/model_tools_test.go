package model_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

// The tool loop's second call: the model asked for list_applications, the
// server ran it and sends the result back.
func toolRequest() model.Request {
	return model.Request{
		System:    "You help with innominatus.",
		MaxTokens: 512,
		Tools: []model.ToolDef{{
			Name: "list_applications", Description: "List the user's applications",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"team":{"type":"string"}},"required":["team"]}`),
		}},
		Turns: []model.Turn{
			{Role: "user", Text: "list my apps"},
			{Role: "assistant", Text: "Looking.", Blocks: []model.Block{{Kind: model.BlockToolUse, ID: "call_1", Name: "list_applications", Input: json.RawMessage(`{"team":"platform"}`)}}},
			{Role: "user", Blocks: []model.Block{{Kind: model.BlockToolResult, ID: "call_1", Content: `[{"name":"shop"}]`}}},
		},
	}
}

// fixture serves body and keeps the request it got.
func fixture(t *testing.T, path, body string) (*httptest.Server, *map[string]any) {
	t.Helper()
	got := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, path, r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestAnthropic_ToolUseRoundTrip(t *testing.T) {
	srv, got := fixture(t, "/v1/messages", `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test",
		"content":[{"type":"text","text":"One more."},{"type":"tool_use","id":"toolu_2","name":"list_applications","input":{"team":"data"}}],
		"stop_reason":"tool_use","usage":{"input_tokens":30,"output_tokens":12}}`)

	rep, err := model.NewAnthropic("k", srv.URL, "claude-test").Call(context.Background(), toolRequest())
	require.NoError(t, err)
	assert.Equal(t, model.StopEnd, rep.Stop)
	assert.Equal(t, "One more.", rep.Text)
	require.Len(t, rep.ToolUses, 1)
	assert.Equal(t, "toolu_2", rep.ToolUses[0].ID)
	assert.Equal(t, "list_applications", rep.ToolUses[0].Name)
	assert.JSONEq(t, `{"team":"data"}`, string(rep.ToolUses[0].Input))

	req := *got
	assert.Nil(t, req["output_config"], "no schema: free text, no output format")
	tools := req["tools"].([]any)
	require.Len(t, tools, 1)
	tool := tools[0].(map[string]any)
	assert.Equal(t, "list_applications", tool["name"])
	assert.Equal(t, "List the user's applications", tool["description"])
	assert.Equal(t, []any{"team"}, tool["input_schema"].(map[string]any)["required"])

	msgs := req["messages"].([]any)
	require.Len(t, msgs, 3)
	asst := msgs[1].(map[string]any)["content"].([]any)
	require.Len(t, asst, 2)
	assert.Equal(t, "tool_use", asst[1].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"team": "platform"}, asst[1].(map[string]any)["input"])
	res := msgs[2].(map[string]any)["content"].([]any)
	require.Len(t, res, 1, "no empty text block next to the tool result")
	assert.Equal(t, "tool_result", res[0].(map[string]any)["type"])
	assert.Equal(t, "call_1", res[0].(map[string]any)["tool_use_id"])
}

func TestOpenAI_ToolUseRoundTrip(t *testing.T) {
	srv, got := fixture(t, "/v1/chat/completions", `{"choices":[{"message":{"content":"","tool_calls":[
		{"id":"call_2","type":"function","function":{"name":"list_applications","arguments":"{\"team\":\"data\"}"}}]},
		"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":30,"completion_tokens":12}}`)

	rep, err := model.NewOpenAI("k", srv.URL+"/v1", "gpt-test").Call(context.Background(), toolRequest())
	require.NoError(t, err)
	assert.Equal(t, model.StopEnd, rep.Stop)
	require.Len(t, rep.ToolUses, 1)
	assert.Equal(t, "call_2", rep.ToolUses[0].ID)
	assert.JSONEq(t, `{"team":"data"}`, string(rep.ToolUses[0].Input))

	req := *got
	assert.Nil(t, req["response_format"])
	fn := req["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	assert.Equal(t, "list_applications", fn["name"])
	assert.NotNil(t, fn["parameters"])
	msgs := req["messages"].([]any)
	require.Len(t, msgs, 4, "system, user, assistant with tool_calls, tool")
	asst := msgs[2].(map[string]any)
	call := asst["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, "call_1", call["id"])
	assert.JSONEq(t, `{"team":"platform"}`, call["function"].(map[string]any)["arguments"].(string))
	tool := msgs[3].(map[string]any)
	assert.Equal(t, "tool", tool["role"])
	assert.Equal(t, "call_1", tool["tool_call_id"])
	assert.Equal(t, `[{"name":"shop"}]`, tool["content"])
}

func TestOllama_ToolUseRoundTrip(t *testing.T) {
	srv, got := fixture(t, "/api/chat", `{"message":{"role":"assistant","content":"","tool_calls":[
		{"function":{"name":"list_applications","arguments":{"team":"data"}}}]},
		"done_reason":"stop","prompt_eval_count":30,"eval_count":12}`)

	rep, err := model.NewOllama(srv.URL, "llama-test").Call(context.Background(), toolRequest())
	require.NoError(t, err)
	require.Len(t, rep.ToolUses, 1)
	assert.Equal(t, "call_0", rep.ToolUses[0].ID, "older Ollama sends no ID: one is made up")
	assert.JSONEq(t, `{"team":"data"}`, string(rep.ToolUses[0].Input))

	req := *got
	_, hasFormat := req["format"]
	assert.False(t, hasFormat, "no schema: no format")
	msgs := req["messages"].([]any)
	require.Len(t, msgs, 4)
	call := msgs[2].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{"team": "platform"}, call["function"].(map[string]any)["arguments"])
	tool := msgs[3].(map[string]any)
	assert.Equal(t, "tool", tool["role"])
	assert.Equal(t, "list_applications", tool["tool_name"])
}

// Instrument refuses the tool request before the backend is called: the
// bedrock endpoint does not listen and the copilot CLI does not exist, so a
// call that reached them would fail as busy, not bad.
func TestBedrockAndCopilot_RefuseTools(t *testing.T) {
	creds := credentials.NewStaticCredentialsProvider("AKID", "secret", "")
	for _, m := range []model.Model{
		model.NewBedrock(creds, "eu-central-1", "http://127.0.0.1:1", "eu.anthropic.claude-test"),
		model.NewCopilot("", "/nonexistent/copilot", "auto"),
	} {
		assert.False(t, model.SupportsTools(m))
		_, err := model.Instrument(m, "tools_test").Call(context.Background(), toolRequest())
		require.Error(t, err)
		ce := model.AsCallError(err)
		assert.Equal(t, model.Bad, ce.Class)
		assert.Contains(t, ce.Msg, "tool use")
	}
}
