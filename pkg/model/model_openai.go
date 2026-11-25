package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ProviderOpenAI is any OpenAI-compatible Chat Completions API: OpenAI,
// Azure OpenAI (through its OpenAI-compatible /openai/v1 path), vLLM,
// LiteLLM and others.
const ProviderOpenAI = "openai"

type openAIModel struct {
	http    *http.Client
	baseURL string
	apiKey  string
	model   string
}

// NewOpenAI returns the OpenAI-compatible backend: one POST
// {baseURL}/chat/completions per call, with response_format set to the
// JSON schema and no streaming. baseURL includes the version path, such as
// https://api.openai.com/v1. apiKey may be empty for a local server.
func NewOpenAI(apiKey, baseURL, model string) Model {
	return &openAIModel{http: newHTTPClient(), baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, model: model}
}

func (m *openAIModel) Name() (string, string) { return ProviderOpenAI, m.model }

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// openAIToolCall is a function call; Arguments is a JSON object as text.
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openAITool is a tool definition, the function form both OpenAI and
// Ollama take.
type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters,omitempty"`
	} `json:"function"`
}

func functionTools(tools []ToolDef) []openAITool {
	out := make([]openAITool, len(tools))
	for i, td := range tools {
		out[i].Type = "function"
		out[i].Function.Name = td.Name
		out[i].Function.Description = td.Description
		out[i].Function.Parameters = td.InputSchema
	}
	return out
}

type openAIChatRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *openAIFormat   `json:"response_format,omitempty"`
	Tools          []openAITool    `json:"tools,omitempty"`
}

type openAIFormat struct {
	Type       string `json:"type"`
	JSONSchema struct {
		Name   string          `json:"name"`
		Schema json.RawMessage `json:"schema"`
		// Strict stays off: strict mode rejects schemas without
		// additionalProperties:false on every object, which the callers'
		// schemas do not promise. The caller checks the answer anyway.
		Strict bool `json:"strict"`
	} `json:"json_schema"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			Refusal   string           `json:"refusal"`
			ToolCalls []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

func (m *openAIModel) Call(ctx context.Context, req Request) (Reply, error) {
	in := openAIChatRequest{Model: m.model, MaxTokens: req.MaxTokens}
	if len(req.Schema) > 0 {
		f := &openAIFormat{Type: "json_schema"}
		f.JSONSchema.Name = "answer"
		f.JSONSchema.Schema = req.Schema
		in.ResponseFormat = f
	}
	if req.System != "" {
		in.Messages = append(in.Messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, t := range req.Turns {
		in.Messages = append(in.Messages, openAIMessages(t)...)
	}
	in.Tools = functionTools(req.Tools)
	body, err := json.Marshal(in)
	if err != nil {
		return Reply{}, &CallError{Class: Bad, Msg: "encode request: " + err.Error()}
	}
	var auth func(*http.Request) *CallError
	if m.apiKey != "" {
		auth = func(httpReq *http.Request) *CallError {
			httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
			return nil
		}
	}
	data, status, hdr, ce := postJSON(ctx, m.http, m.baseURL+"/chat/completions", body, auth)
	if ce != nil {
		return Reply{}, ce
	}
	if status != http.StatusOK {
		return Reply{}, openAIError(status, hdr, data, m.model)
	}
	var out openAIChatResponse
	if err := json.Unmarshal(data, &out); err != nil || len(out.Choices) == 0 {
		return Reply{}, &CallError{Class: Bad, Status: status, Msg: "the answer is not Chat Completions JSON with a choice"}
	}
	c := out.Choices[0]
	rep := Reply{Text: c.Message.Content, InputTokens: out.Usage.PromptTokens, OutputTokens: out.Usage.CompletionTokens}
	for _, tc := range c.Message.ToolCalls {
		rep.ToolUses = append(rep.ToolUses, ToolUse{ID: tc.ID, Name: tc.Function.Name, Input: argumentsJSON(tc.Function.Arguments)})
	}
	switch {
	case c.Message.Refusal != "":
		rep.Stop, rep.Text = StopRefusal, c.Message.Refusal
	case c.FinishReason == "length":
		rep.Stop = StopMaxTokens
	case c.FinishReason == "content_filter":
		rep.Stop = StopRefusal
	}
	return rep, nil
}

// openAIMessages is one turn as Chat Completions messages: an assistant
// turn's tool_use blocks are its tool_calls, and each tool_result is its
// own "tool" message.
func openAIMessages(t Turn) []openAIMessage {
	msg := openAIMessage{Role: t.Role, Content: t.Text}
	var results []openAIMessage
	for _, b := range t.Blocks {
		switch b.Kind {
		case BlockToolUse:
			tc := openAIToolCall{ID: b.ID, Type: "function"}
			tc.Function.Name = b.Name
			tc.Function.Arguments = string(objectOrEmpty(b.Input))
			msg.ToolCalls = append(msg.ToolCalls, tc)
		case BlockToolResult:
			results = append(results, openAIMessage{Role: "tool", ToolCallID: b.ID, Content: b.Content})
		}
	}
	if len(results) > 0 && t.Text == "" && len(msg.ToolCalls) == 0 {
		return results
	}
	return append([]openAIMessage{msg}, results...)
}

// argumentsJSON is a function call's arguments as a JSON object; text that
// is not one becomes {} so the caller never forwards invalid JSON.
func argumentsJSON(s string) json.RawMessage {
	raw := json.RawMessage(strings.TrimSpace(s))
	if !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return objectOrEmpty(raw)
}

func objectOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(`{}`)
	}
	return raw
}

func openAIError(status int, hdr http.Header, data []byte, model string) *CallError {
	class := classOf(status)
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	var msg string
	if json.Unmarshal(data, &body) == nil {
		msg = body.Error.Message
		if body.Error.Code == "insufficient_quota" {
			// OpenAI answers an account without credits with 429 like a rate
			// limit, but waiting does not help: the operator must add credits.
			class = Refused
		}
	}
	return httpError(class, status, hdr, msg, fmt.Sprintf("model %s or the path /chat/completions is not on the server", model))
}
