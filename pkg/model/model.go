// Package model is a provider-neutral way to call a language model: one
// Model interface with Anthropic, Bedrock, OpenAI-compatible, Ollama,
// GitHub Copilot and replay backends, a retry and tool-use Loop, a token
// Budget, error classes (Classify), and wrappers that every call should go
// through: Guard (a provider allowlist with contract terms) and Instrument
// (redaction, tracing, metrics and an audit hook).
//
// A backend's Call makes exactly one request, with no retries: the Loop's
// backoff is the only one, so a caller sees and budgets every try.
package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Defaults of the backends.
const (
	// DefaultMaxTokens is the output allowance of a backend whose request
	// sets no MaxTokens.
	DefaultMaxTokens        = 4096
	DefaultAnthropicBaseURL = "https://api.anthropic.com"
	DefaultOllamaBaseURL    = "http://localhost:11434"
	DefaultOpenAIBaseURL    = "https://api.openai.com/v1"
)

// Model is one model backend. Call makes exactly one request: no retries,
// so the caller's backoff (Loop) is the only one.
type Model interface {
	Call(ctx context.Context, req Request) (Reply, error)
	// Name returns the provider and model, for logs, metrics and audit.
	Name() (provider, model string)
}

// Request is one call to the model.
type Request struct {
	System string
	// Turns is the user prompt; from try 2 also the last answer and the
	// feedback on it.
	Turns []Turn
	// Schema is the JSON schema the answer must match. Empty: the answer
	// is free text (or tool calls).
	Schema    json.RawMessage
	MaxTokens int
	// Tools the model may call.
	// The anthropic, openai and ollama backends send them; Instrument
	// refuses a request with tools or blocks to bedrock, copilot and replay
	// (Bad, see ToolCapable).
	Tools []ToolDef
}

// Turn is one message: Role is "user" or "assistant". Text-only callers
// set Text. A tool loop adds Blocks: an assistant turn's tool_use blocks
// (after its Text, if any) and the next user turn's tool_result blocks.
type Turn struct {
	Role, Text string
	// omitempty keeps the prompt hash of text-only requests as it was.
	Blocks []Block `json:",omitempty"`
}

// BlockKind is the kind of a Block.
type BlockKind string

const (
	// BlockToolUse is a tool call the model asked for: ID, Name, Input.
	BlockToolUse BlockKind = "tool_use"
	// BlockToolResult answers the tool_use with the same ID: Content,
	// IsError.
	BlockToolResult BlockKind = "tool_result"
)

// Block is one tool_use or tool_result part of a turn.
type Block struct {
	Kind    BlockKind       `json:"kind"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Content string          `json:"content,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
}

// ToolDef is a tool the model may call; InputSchema is a JSON schema of
// an object.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// ToolUse is a tool call in the model's answer. Input is a JSON object.
type ToolUse struct {
	ID, Name string
	Input    json.RawMessage
}

// usesTools reports whether req has tools or tool blocks, which only some
// backends carry.
func usesTools(req Request) bool {
	if len(req.Tools) > 0 {
		return true
	}
	for _, t := range req.Turns {
		if len(t.Blocks) > 0 {
			return true
		}
	}
	return false
}

// errNoTools is the error of a backend that does not carry tools.
func errNoTools(provider string) *CallError {
	return &CallError{Class: Bad, Msg: "the " + provider + " backend does not support tool use"}
}

// Stop says how the model ended its answer.
type Stop int

const (
	StopEnd Stop = iota
	// StopMaxTokens: the answer was cut at the token limit; it counts as
	// an invalid try.
	StopMaxTokens
	// StopRefusal: the model declined (Anthropic only); the job fails.
	StopRefusal
)

// Reply is the model's answer to one call.
type Reply struct {
	Text string
	// ToolUses are the tool calls the model asked for; Stop is StopEnd.
	ToolUses                  []ToolUse
	Stop                      Stop
	InputTokens, OutputTokens int64
	// Redactions is how many parts of the prompt redaction removed before
	// the call. Set by Instrument and Redacting, never by a backend.
	Redactions int
}

// Class is what a caller should do with a failed call.
type Class int

const (
	// Busy: 429, 5xx, a network error or a timeout. Not a try: back off
	// and call again later.
	Busy Class = iota
	// Refused: 401 or 403. Final; log it as an error, because the key or
	// the proxy in front of the model is wrong.
	Refused
	// NotFound: 404, the model is not on the (Ollama) server. Final.
	NotFound
	// Bad: 400 and every other answer. Final.
	Bad
)

func (c Class) String() string {
	switch c {
	case Busy:
		return "busy"
	case Refused:
		return "refused"
	case NotFound:
		return "not found"
	}
	return "bad request"
}

// CallError carries the class a caller acts on; every non-nil error from
// Model.Call is one.
type CallError struct {
	Class  Class
	Status int // 0 for network errors and timeouts
	// RetryAfter is the retry-after header; 0 when absent.
	RetryAfter time.Duration
	// Msg is cut to 300 bytes and never holds the API key.
	Msg string
	// AuditFailed: the AI audit trail could not record the call (Instrument
	// with WithAudit), so the model was not called or its answer was
	// discarded.
	AuditFailed bool
}

func (e *CallError) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("model API %s (%d): %s", e.Class, e.Status, e.Msg)
	}
	return fmt.Sprintf("model API %s: %s", e.Class, e.Msg)
}

// AsCallError returns err as a *CallError; an error that is none is Busy,
// so an unexpected transport error is retried, not final.
func AsCallError(err error) *CallError {
	var ce *CallError
	if errors.As(err, &ce) {
		return ce
	}
	return &CallError{Class: Busy, Msg: cut(err.Error(), maxErrBytes)}
}

// classOf maps an HTTP status to a Class.
func classOf(status int) Class {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return Refused
	case status == http.StatusNotFound:
		return NotFound
	case status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500:
		return Busy
	}
	return Bad
}

// retryAfter reads a retry-after header in whole seconds; 0 when absent or
// not a number of seconds.
func retryAfter(h http.Header) time.Duration {
	v := h.Get("retry-after")
	if v == "" {
		return 0
	}
	var s int
	if _, err := fmt.Sscanf(v, "%d", &s); err != nil || s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

const maxErrBytes = 300

// Cut shortens s to at most n bytes, on a rune boundary.
func Cut(s string, n int) string { return cut(s, n) }

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// CallTimeout is the timeout a caller should give one model call. A
// CPU-only Ollama can take longer for a large answer; that is a Busy
// outcome.
const CallTimeout = 90 * time.Second

// errRedirect is the error of a call the base URL answered with a
// redirect. It is final (Bad): the operator must set the right base URL.
var errRedirect = errors.New("redirect refused: the base URL must point at the model API itself")

// newHTTPClient returns the client both backends use: it refuses
// redirects, so a redirect cannot send the prompt (or the key) to a host
// the operator did not configure.
func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: CallTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirect
		},
	}
}

// transportError maps an error of http.Client.Do: a refused redirect is
// final, anything else (connection refused, timeout, DNS) is busy.
func transportError(err error) *CallError {
	if errors.Is(err, errRedirect) {
		return &CallError{Class: Bad, Msg: errRedirect.Error()}
	}
	return &CallError{Class: Busy, Msg: cut(err.Error(), maxErrBytes)}
}
