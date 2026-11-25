package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ProviderAnthropic is the Anthropic Messages API backend.
const ProviderAnthropic = "anthropic"

type anthropicModel struct {
	client anthropic.Client
	model  string
}

// NewAnthropic returns the Anthropic backend: one Messages request per
// call to baseURL + /v1/messages, the SDK's own retries off, redirects
// refused, and no credential read from the environment besides apiKey.
func NewAnthropic(apiKey, baseURL, model string) Model {
	return &anthropicModel{
		client: anthropic.NewClient(
			// Only what is passed here: no ANTHROPIC_* variable or
			// credential profile is read behind the operator's back.
			option.WithoutEnvironmentDefaults(),
			option.WithAPIKey(apiKey),
			option.WithBaseURL(baseURL),
			option.WithMaxRetries(0),
			option.WithHTTPClient(newHTTPClient()),
		),
		model: model,
	}
}

func (m *anthropicModel) Name() (string, string) { return ProviderAnthropic, m.model }

func (m *anthropicModel) Call(ctx context.Context, req Request) (Reply, error) {
	msgs := make([]anthropic.MessageParam, 0, len(req.Turns))
	for _, t := range req.Turns {
		blocks := anthropicBlocks(t)
		if t.Role == "assistant" {
			msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
			continue
		}
		msgs = append(msgs, anthropic.NewUserMessage(blocks...))
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(m.model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  msgs,
	}
	// A request without a schema (chat, its tool loop) asks for free text.
	if len(req.Schema) > 0 {
		schema, err := anthropicSchema(req.Schema)
		if err != nil {
			return Reply{}, &CallError{Class: Bad, Msg: err.Error()}
		}
		params.OutputConfig = anthropic.OutputConfigParam{Format: anthropic.JSONOutputFormatParam{Schema: schema}}
	}
	for _, td := range req.Tools {
		tool, err := anthropicTool(td)
		if err != nil {
			return Reply{}, &CallError{Class: Bad, Msg: err.Error()}
		}
		params.Tools = append(params.Tools, tool)
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	msg, err := m.client.Messages.New(ctx, params)
	if err != nil {
		return Reply{}, anthropicError(err)
	}
	var text strings.Builder
	var uses []ToolUse
	for _, block := range msg.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(b.Text)
		case anthropic.ToolUseBlock:
			uses = append(uses, ToolUse{ID: b.ID, Name: b.Name, Input: b.Input})
		}
	}
	rep := Reply{Text: text.String(), ToolUses: uses, InputTokens: msg.Usage.InputTokens, OutputTokens: msg.Usage.OutputTokens}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		rep.Stop = StopRefusal
	case anthropic.StopReasonMaxTokens:
		rep.Stop = StopMaxTokens
	}
	return rep, nil
}

// anthropicBlocks is a turn's content: its text, then its blocks. An empty
// text is left out (the API refuses an empty text block) unless the turn
// has nothing else.
func anthropicBlocks(t Turn) []anthropic.ContentBlockParamUnion {
	var out []anthropic.ContentBlockParamUnion
	if t.Text != "" || len(t.Blocks) == 0 {
		out = append(out, anthropic.NewTextBlock(t.Text))
	}
	for _, b := range t.Blocks {
		switch b.Kind {
		case BlockToolUse:
			input := b.Input
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			out = append(out, anthropic.NewToolUseBlock(b.ID, input, b.Name))
		case BlockToolResult:
			out = append(out, anthropic.NewToolResultBlock(b.ID, b.Content, b.IsError))
		}
	}
	return out
}

// anthropicTool is a ToolDef as the Messages API takes it.
func anthropicTool(td ToolDef) (anthropic.ToolUnionParam, error) {
	var s map[string]any
	if len(td.InputSchema) > 0 {
		if err := json.Unmarshal(td.InputSchema, &s); err != nil {
			return anthropic.ToolUnionParam{}, fmt.Errorf("tool %s: the input schema is not a JSON object", td.Name)
		}
	}
	in := anthropic.ToolInputSchemaParam{ExtraFields: map[string]any{}}
	for k, v := range s {
		switch k {
		case "type":
		case "properties":
			in.Properties = v
		case "required":
			in.Required = append(in.Required, asStrings(v)...)
		default:
			in.ExtraFields[k] = v
		}
	}
	tool := anthropic.ToolUnionParamOfTool(in, td.Name)
	if td.Description != "" {
		tool.OfTool.Description = anthropic.String(td.Description)
	}
	return tool, nil
}

func asStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// anthropicError maps an SDK error to a CallError. The message comes from
// the API's error body (error.message), not from err.Error(), which also
// holds the request URL.
func anthropicError(err error) *CallError {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		var hdr http.Header
		if apiErr.Response != nil {
			hdr = apiErr.Response.Header
		}
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		var msg string
		if json.Unmarshal([]byte(apiErr.RawJSON()), &body) == nil {
			msg = body.Error.Message
		}
		// No NotFound text of its own: a 404 carries the API's message.
		return httpError(classOf(apiErr.StatusCode), apiErr.StatusCode, hdr, msg, "")
	}
	// Network errors, timeouts and a cancelled context: busy, not a try.
	return transportError(err)
}

// unsupportedConstraints are the JSON Schema keywords the Messages API's
// structured outputs reject. They are removed from the schema sent and
// written into the field's description instead, so the model still knows
// the bound; the caller checks them on the answer.
var unsupportedConstraints = []string{"minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems"}

// anthropicSchema turns a request Schema into what output_config.format
// accepts.
func anthropicSchema(raw json.RawMessage) (map[string]any, error) {
	var s map[string]any
	if err := json.Unmarshal(raw, &s); err != nil || s == nil {
		return nil, errors.New("the request's form has no valid JSON schema")
	}
	props, _ := s["properties"].(map[string]any)
	for _, p := range props {
		field, ok := p.(map[string]any)
		if !ok {
			continue
		}
		var bounds []string
		for _, k := range unsupportedConstraints {
			if v, ok := field[k]; ok {
				bounds = append(bounds, fmt.Sprintf("%s %v", k, v))
				delete(field, k)
			}
		}
		if len(bounds) == 0 {
			continue
		}
		sort.Strings(bounds)
		desc, _ := field["description"].(string)
		if desc != "" {
			desc += " "
		}
		field["description"] = desc + "(" + strings.Join(bounds, ", ") + ")"
	}
	return s, nil
}
