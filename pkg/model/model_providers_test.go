package model_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

const (
	openAIKey = "sk-openai-test-must-not-leak-0815"
	gptM      = "gpt-test"
	bedrockM  = "eu.anthropic.claude-sonnet-5-5-v1:0"
	awsSecret = "aws-secret-must-not-leak-1234"
)

func call(t *testing.T, m model.Model) (model.Reply, error) {
	t.Helper()
	return m.Call(context.Background(), model.Request{
		System: "You size workloads.", Turns: []model.Turn{{Role: "user", Text: "Size shop."}},
		Schema: sizingSchema, MaxTokens: 256,
	})
}

func TestOpenAI_RequestAndReply(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer "+openAIKey, r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+strings.ReplaceAll(validFix, `"`, `\"`)+`"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
	}))
	t.Cleanup(srv.Close)

	rep, err := call(t, model.NewOpenAI(openAIKey, srv.URL+"/v1", gptM))
	require.NoError(t, err)
	assert.JSONEq(t, validFix, rep.Text)
	assert.Equal(t, model.StopEnd, rep.Stop)
	assert.Equal(t, int64(11), rep.InputTokens)
	assert.Equal(t, int64(7), rep.OutputTokens)

	assert.Equal(t, gptM, got["model"])
	msgs := got["messages"].([]any)
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]any)["role"])
	rf := got["response_format"].(map[string]any)
	assert.Equal(t, "json_schema", rf["type"])
	assert.NotNil(t, rf["json_schema"].(map[string]any)["schema"], "the answer schema is sent")
}

func TestOpenAI_StopsAndErrors(t *testing.T) {
	tests := map[string]struct {
		status    int
		body      string
		wantStop  model.Stop
		wantClass model.Class
		wantErr   bool
	}{
		"length":     {200, `{"choices":[{"message":{"content":"{"},"finish_reason":"length"}]}`, model.StopMaxTokens, 0, false},
		"refusal":    {200, `{"choices":[{"message":{"content":"","refusal":"no"},"finish_reason":"stop"}]}`, model.StopRefusal, 0, false},
		"429":        {429, `{"error":{"message":"slow down"}}`, 0, model.Busy, true},
		"no credits": {429, `{"error":{"message":"You have no credits remaining.","code":"insufficient_quota"}}`, 0, model.Refused, true},
		"401":        {401, `{"error":{"message":"bad key"}}`, 0, model.Refused, true},
		"404":        {404, `{}`, 0, model.NotFound, true},
		"no choice":  {200, `{"choices":[]}`, 0, model.Bad, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)
			rep, err := call(t, model.NewOpenAI(openAIKey, srv.URL, gptM))
			if !tt.wantErr {
				require.NoError(t, err)
				assert.Equal(t, tt.wantStop, rep.Stop)
				return
			}
			require.Error(t, err)
			ce := model.AsCallError(err)
			assert.Equal(t, tt.wantClass, ce.Class)
			assert.NotContains(t, ce.Error(), openAIKey)
		})
	}
}

func newBedrock(url string) model.Model {
	creds := credentials.NewStaticCredentialsProvider("AKIDTEST", awsSecret, "")
	return model.NewBedrock(creds, "eu-central-1", url, bedrockM)
}

func TestBedrock_SignedForcedToolCall(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The model ID has a colon, which is escaped in the path.
		assert.Equal(t, "/model/"+bedrockM+"/invoke", r.URL.Path)
		assert.Contains(t, r.URL.RawPath, "%3A", "the model ID is path-escaped")
		auth := r.Header.Get("Authorization")
		assert.True(t, strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIDTEST/"), auth)
		assert.Contains(t, auth, "/eu-central-1/bedrock/aws4_request")
		assert.NotEmpty(t, r.Header.Get("X-Amz-Date"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"Here you go."},{"type":"tool_use","name":"answer","input":`+validFix+`}],"stop_reason":"tool_use","usage":{"input_tokens":20,"output_tokens":9}}`)
	}))
	t.Cleanup(srv.Close)

	rep, err := call(t, newBedrock(srv.URL))
	require.NoError(t, err)
	assert.JSONEq(t, validFix, rep.Text, "the forced tool's input is the answer")
	assert.Equal(t, model.StopEnd, rep.Stop)
	assert.Equal(t, int64(20), rep.InputTokens)

	assert.Equal(t, "bedrock-2023-05-31", got["anthropic_version"])
	assert.Equal(t, "You size workloads.", got["system"])
	tc := got["tool_choice"].(map[string]any)
	assert.Equal(t, "tool", tc["type"])
	assert.Equal(t, "answer", tc["name"])
	tools := got["tools"].([]any)
	require.Len(t, tools, 1)
	assert.NotNil(t, tools[0].(map[string]any)["input_schema"])
}

func TestBedrock_Errors(t *testing.T) {
	tests := map[string]struct {
		status    int
		wantClass model.Class
	}{
		"throttled":       {429, model.Busy},
		"model not ready": {424, model.Busy},
		"access denied":   {403, model.Refused},
		"no such model":   {404, model.NotFound},
		"validation":      {400, model.Bad},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"message":"nope"}`)
			}))
			t.Cleanup(srv.Close)
			_, err := call(t, newBedrock(srv.URL))
			require.Error(t, err)
			ce := model.AsCallError(err)
			assert.Equal(t, tt.wantClass, ce.Class)
			assert.NotContains(t, ce.Error(), awsSecret)
		})
	}
}
