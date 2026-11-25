package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ProviderOllama is the Ollama /api/chat backend.
const ProviderOllama = "ollama"

type ollamaModel struct {
	http    *http.Client
	baseURL string
	model   string
}

// NewOllama returns the Ollama backend: one POST {baseURL}/api/chat per
// call, with format set to the JSON schema and no streaming.
func NewOllama(baseURL, model string) Model {
	return &ollamaModel{http: newHTTPClient(), baseURL: strings.TrimRight(baseURL, "/"), model: model}
}

func (m *ollamaModel) Name() (string, string) { return ProviderOllama, m.model }

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	// ToolName names the tool a "tool" message answers.
	ToolName string `json:"tool_name,omitempty"`
}

// ollamaToolCall is a function call; unlike OpenAI, Arguments is a JSON
// object, and older Ollama versions send no ID.
type ollamaToolCall struct {
	ID       string `json:"id,omitempty"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	// Format is left out when the request has no schema (free text).
	Format  json.RawMessage `json:"format,omitempty"`
	Tools   []openAITool    `json:"tools,omitempty"`
	Stream  bool            `json:"stream"`
	Options struct {
		NumPredict int `json:"num_predict"`
	} `json:"options"`
}

type ollamaChatResponse struct {
	Message         ollamaMessage `json:"message"`
	DoneReason      string        `json:"done_reason"`
	PromptEvalCount int64         `json:"prompt_eval_count"`
	EvalCount       int64         `json:"eval_count"`
}

func (m *ollamaModel) Call(ctx context.Context, req Request) (Reply, error) {
	in := ollamaChatRequest{Model: m.model, Format: req.Schema}
	in.Options.NumPredict = req.MaxTokens
	if req.System != "" {
		in.Messages = append(in.Messages, ollamaMessage{Role: "system", Content: req.System})
	}
	for _, t := range req.Turns {
		in.Messages = append(in.Messages, ollamaMessages(t, req.Turns)...)
	}
	in.Tools = functionTools(req.Tools)
	body, err := json.Marshal(in)
	if err != nil {
		return Reply{}, &CallError{Class: Bad, Msg: "encode request: " + err.Error()}
	}
	// Connection refused (Ollama not running), a timeout, DNS: busy.
	data, status, hdr, ce := postJSON(ctx, m.http, m.baseURL+"/api/chat", body, nil)
	if ce != nil {
		return Reply{}, ce
	}
	if status != http.StatusOK {
		return Reply{}, ollamaError(status, hdr, data, m.model)
	}
	var out ollamaChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Reply{}, &CallError{Class: Bad, Status: status, Msg: "the answer is not Ollama chat JSON"}
	}
	rep := Reply{Text: out.Message.Content, InputTokens: out.PromptEvalCount, OutputTokens: out.EvalCount}
	for i, tc := range out.Message.ToolCalls {
		id := tc.ID
		if id == "" {
			// The loop pairs results with calls by ID.
			id = fmt.Sprintf("call_%d", i)
		}
		rep.ToolUses = append(rep.ToolUses, ToolUse{ID: id, Name: tc.Function.Name, Input: objectOrEmpty(tc.Function.Arguments)})
	}
	if out.DoneReason == "length" {
		rep.Stop = StopMaxTokens
	}
	return rep, nil
}

// ollamaMessages is one turn as /api/chat messages. A tool_result names
// its tool (tool_name), found by the ID of the tool_use it answers.
func ollamaMessages(t Turn, all []Turn) []ollamaMessage {
	msg := ollamaMessage{Role: t.Role, Content: t.Text}
	var results []ollamaMessage
	for _, b := range t.Blocks {
		switch b.Kind {
		case BlockToolUse:
			tc := ollamaToolCall{ID: b.ID}
			tc.Function.Name = b.Name
			tc.Function.Arguments = objectOrEmpty(b.Input)
			msg.ToolCalls = append(msg.ToolCalls, tc)
		case BlockToolResult:
			results = append(results, ollamaMessage{Role: "tool", Content: b.Content, ToolName: toolNameOf(b.ID, all)})
		}
	}
	if len(results) > 0 && t.Text == "" && len(msg.ToolCalls) == 0 {
		return results
	}
	return append([]ollamaMessage{msg}, results...)
}

func toolNameOf(id string, turns []Turn) string {
	for _, t := range turns {
		for _, b := range t.Blocks {
			if b.Kind == BlockToolUse && b.ID == id {
				return b.Name
			}
		}
	}
	return ""
}

func ollamaError(status int, hdr http.Header, data []byte, model string) *CallError {
	var body struct {
		Error string `json:"error"`
	}
	var msg string
	if json.Unmarshal(data, &body) == nil {
		msg = body.Error
	}
	// The caller tells its user about NotFound; this text is for the log.
	return httpError(classOf(status), status, hdr, msg, fmt.Sprintf("model %s is not on the Ollama server", model))
}

// ListOllamaModels returns the model names an Ollama server has (GET
// /api/tags), for example to warn at startup about a model that was not
// pulled.
func ListOllamaModels(ctx context.Context, baseURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := newHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /api/tags: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&out); err != nil {
		return nil, errors.New("GET /api/tags: the answer is not JSON")
	}
	names := make([]string, len(out.Models))
	for i, mo := range out.Models {
		names[i] = mo.Name
	}
	return names, nil
}
