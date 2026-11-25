package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const copilotToken = "ghp_copilotTestTokenMustNotLeak0123456789" // gitleaks:allow (fake token the tests keep out of errors)

// fakeCopilot is a copilotRuntime: it records the session config and the
// message, and answers with the events and the reply it is given.
type fakeCopilot struct {
	mu        sync.Mutex
	cfg       *copilot.SessionConfig
	msg       copilot.MessageOptions
	createErr error
	sendErr   error
	events    []copilot.SessionEvent
	reply     *copilot.SessionEvent
	unhealthy bool
	resets    int
	stops     int
	closed    bool
}

func (f *fakeCopilot) createSession(_ context.Context, cfg *copilot.SessionConfig) (copilotSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = cfg
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &fakeCopilotSession{f: f}, nil
}
func (f *fakeCopilot) healthy(context.Context) bool { return !f.unhealthy }
func (f *fakeCopilot) reset()                       { f.resets++ }
func (f *fakeCopilot) stop() error                  { f.stops++; return nil }

type fakeCopilotSession struct {
	f *fakeCopilot
	h copilot.SessionEventHandler
}

func (s *fakeCopilotSession) on(h copilot.SessionEventHandler) func() { s.h = h; return func() {} }
func (s *fakeCopilotSession) sendAndWait(_ context.Context, o copilot.MessageOptions) (*copilot.SessionEvent, error) {
	s.f.msg = o
	for _, e := range s.f.events {
		s.h(e)
	}
	return s.f.reply, s.f.sendErr
}
func (s *fakeCopilotSession) disconnect() error { s.f.closed = true; return nil }

func usageEvent(model string, in, out int64, finish string) copilot.SessionEvent {
	return copilot.SessionEvent{Data: &rpc.AssistantUsageData{Model: model, InputTokens: &in, OutputTokens: &out, FinishReason: &finish}}
}

func replyEvent(text string) *copilot.SessionEvent {
	return &copilot.SessionEvent{Data: &rpc.AssistantMessageData{Content: text}}
}

var copilotReq = Request{
	System: "You size workloads.",
	Turns:  []Turn{{Role: "user", Text: "Size shop."}},
	Schema: json.RawMessage(`{"type":"object","properties":{"cpu":{"type":"string"}}}`),
}

func TestCopilot_RequestMapping(t *testing.T) {
	f := &fakeCopilot{events: []copilot.SessionEvent{usageEvent("gpt-6-luna", 40, 9, "stop")}, reply: replyEvent(`{"cpu":"1"}`)}
	m := &copilotModel{rt: f, model: "gpt-6-luna"}
	rep, err := m.Call(context.Background(), copilotReq)
	require.NoError(t, err)
	assert.Equal(t, `{"cpu":"1"}`, rep.Text)
	assert.Equal(t, StopEnd, rep.Stop)
	assert.Equal(t, int64(40), rep.InputTokens)
	assert.Equal(t, int64(9), rep.OutputTokens)

	cfg := f.cfg
	assert.Equal(t, "gpt-6-luna", cfg.Model)
	require.NotNil(t, cfg.SystemMessage)
	assert.Equal(t, "replace", cfg.SystemMessage.Mode)
	assert.True(t, strings.HasPrefix(cfg.SystemMessage.Content, "You size workloads."))
	assert.Contains(t, cfg.SystemMessage.Content, string(copilotReq.Schema), "the schema goes into the system message")
	assert.NotNil(t, cfg.AvailableTools, "nil would mean the default tools")
	assert.Empty(t, cfg.AvailableTools)
	assert.Contains(t, cfg.DisabledMCPServers, "github-mcp-server")
	require.NotNil(t, cfg.InfiniteSessions)
	assert.False(t, *cfg.InfiniteSessions.Enabled)
	assert.Equal(t, "Size shop.", f.msg.Prompt)
	assert.Nil(t, f.msg.ResponseSchema, "the SDK's strict schema mode rejects the callers' schemas")
	assert.True(t, f.closed, "the session is disconnected")
}

func TestCopilot_ToolsAreDenied(t *testing.T) {
	cfg := (&copilotModel{model: "auto"}).sessionConfig(copilotReq)
	dec, err := cfg.OnPermissionRequest(&rpc.PermissionRequestShell{FullCommandText: "cat ~/.ssh/config"}, copilot.PermissionInvocation{})
	require.NoError(t, err)
	_, ok := dec.(*rpc.PermissionDecisionReject)
	assert.True(t, ok, "every permission request is rejected, got %T", dec)
	require.NotNil(t, cfg.Hooks)
	out, err := cfg.Hooks.OnPreToolUse(copilot.PreToolUseHookInput{ToolName: "bash"}, copilot.HookInvocation{})
	require.NoError(t, err)
	assert.Equal(t, "deny", out.PermissionDecision)
}

func TestCopilot_ToolRequestFailsTheCall(t *testing.T) {
	f := &fakeCopilot{
		events: []copilot.SessionEvent{
			{Data: &rpc.AssistantMessageData{ToolRequests: make([]rpc.AssistantMessageToolRequest, 1)}},
			usageEvent("m", 5, 5, "stop"),
		},
		reply: replyEvent(`{}`),
	}
	_, err := (&copilotModel{rt: f, model: "m"}).Call(context.Background(), copilotReq)
	require.Error(t, err)
	assert.Equal(t, Bad, AsCallError(err).Class)
}

func TestCopilot_Turns(t *testing.T) {
	got := copilotPrompt([]Turn{{Role: "user", Text: "Size shop."}, {Role: "assistant", Text: "{}"}, {Role: "user", Text: "cpu is missing"}})
	assert.Contains(t, got, "[user]\nSize shop.")
	assert.Contains(t, got, "[assistant]\n{}")
	assert.True(t, strings.HasSuffix(got, "[user]\ncpu is missing"), got)
	assert.Less(t, strings.Index(got, "Size shop."), strings.Index(got, "cpu is missing"))
}

func TestCopilot_ReplyText(t *testing.T) {
	tests := map[string]struct{ in, want string }{
		"plain":       {`{"a":1}`, `{"a":1}`},
		"json fence":  {"```json\n{\"a\":1}\n```", `{"a":1}`},
		"bare fence":  {"```\n{\"a\":1}\n```", `{"a":1}`},
		"padded":      {"\n```json\n{\"a\":1}\n```\n", `{"a":1}`},
		"inner fence": {"text with ``` inside", "text with ``` inside"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) { assert.Equal(t, tt.want, stripJSONFence(tt.in)) })
	}
}

func TestCopilot_Stops(t *testing.T) {
	for finish, want := range map[string]Stop{"length": StopMaxTokens, "content_filter": StopRefusal, "stop": StopEnd} {
		f := &fakeCopilot{events: []copilot.SessionEvent{usageEvent("m", 1, 1, finish)}, reply: replyEvent("{")}
		rep, err := (&copilotModel{rt: f, model: "m"}).Call(context.Background(), copilotReq)
		require.NoError(t, err)
		assert.Equal(t, want, rep.Stop, finish)
	}
}

func TestCopilot_TokensNeverZero(t *testing.T) {
	f := &fakeCopilot{reply: replyEvent(`{"cpu":"1"}`)}
	rep, err := (&copilotModel{rt: f, model: "auto"}).Call(context.Background(), copilotReq)
	require.NoError(t, err)
	assert.Positive(t, rep.InputTokens)
	assert.Positive(t, rep.OutputTokens)

	f = &fakeCopilot{sendErr: errors.New("JSON-RPC Error -32603: Execution failed: 500 upstream")}
	rep, err = (&copilotModel{rt: f, model: "auto"}).Call(context.Background(), copilotReq)
	require.Error(t, err)
	assert.Positive(t, rep.InputTokens+rep.OutputTokens, "a failed call is settled to an estimate, not 0")
}

func TestCopilot_ServedModel(t *testing.T) {
	tests := map[string]struct {
		configured, served string
		wantErr            bool
	}{
		"same":         {"gpt-6-luna", "gpt-6-luna", false},
		"dated":        {"claude-sonnet-4.5", "claude-sonnet-4.5-20250929", false},
		"case":         {"GPT-6-Luna", "gpt-6-luna", false},
		"auto":         {"auto", "mai-code-1.1-flash", false},
		"rerouted":     {"claude-sonnet-4.5", "mai-code-1.1-flash", true},
		"variant":      {"gpt-6", "gpt-6-luna", true},
		"other family": {"gpt-6-luna", "gpt-6", true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := &fakeCopilot{events: []copilot.SessionEvent{usageEvent(tt.served, 3, 3, "stop")}, reply: replyEvent("{}")}
			rep, err := (&copilotModel{rt: f, model: tt.configured}).Call(context.Background(), copilotReq)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			ce := AsCallError(err)
			assert.Equal(t, NotFound, ce.Class)
			assert.Contains(t, ce.Msg, tt.served)
			assert.Equal(t, int64(6), rep.InputTokens+rep.OutputTokens, "spent tokens are reported with the error")
		})
	}
}

func TestCopilot_Errors(t *testing.T) {
	tests := map[string]struct {
		createErr, sendErr error
		unhealthy          bool
		wantClass          Class
		wantStatus         int
		wantMsg            string
		wantReset          bool
	}{
		"bad token": {createErr: errors.New("failed to create session: JSON-RPC Error -32603: Request session.create failed with message: Authentication failed: Failed to validate SDK token (401): GitHub returned: Bad credentials " + copilotToken),
			wantClass: Refused, wantStatus: 401},
		"no login":       {sendErr: errors.New("Execution failed: model resolution leaf failed: get_model_list: GenericFailure, Session was not created with authentication info or custom provider"), wantClass: Refused},
		"no seat":        {sendErr: errors.New("Execution failed: 403 Copilot is not enabled for this account"), wantClass: Refused, wantStatus: 403},
		"out of credits": {sendErr: errors.New("Execution failed: 402 quota exceeded"), wantClass: Refused, wantStatus: 402},
		"bad schema":     {sendErr: errors.New("JSON-RPC Error -32603: Execution failed: 400 Invalid schema for response_format"), wantClass: Bad, wantStatus: 400},
		"unknown model":  {sendErr: errors.New("Execution failed: 404 model not found"), wantClass: NotFound, wantStatus: 404},
		"rate limit":     {sendErr: errors.New("Execution failed: 429 too many requests"), wantClass: Busy, wantStatus: 429},
		"cli missing": {createErr: errCopilotStartf("failed to start CLI server: fork/exec /nope/copilot: no such file or directory"),
			wantClass: Bad, wantMsg: "Copilot CLI path"},
		"timeout":     {sendErr: context.DeadlineExceeded, wantClass: Busy, wantMsg: "deadline exceeded"},
		"cli crashed": {sendErr: errors.New("connection closed"), unhealthy: true, wantClass: Busy, wantReset: true},
		"busy alive":  {sendErr: errors.New("connection hiccup"), wantClass: Busy},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := &fakeCopilot{createErr: tt.createErr, sendErr: tt.sendErr, unhealthy: tt.unhealthy}
			_, err := (&copilotModel{rt: f, model: "auto"}).Call(context.Background(), copilotReq)
			require.Error(t, err)
			var ce *CallError
			require.True(t, errors.As(err, &ce), "every error is a *CallError")
			assert.Equal(t, tt.wantClass, ce.Class, ce.Msg)
			assert.Equal(t, tt.wantStatus, ce.Status)
			assert.Contains(t, ce.Msg, tt.wantMsg)
			assert.NotContains(t, ce.Error(), copilotToken)
			assert.LessOrEqual(t, len(ce.Msg), maxErrBytes)
			assert.Equal(t, tt.wantReset, f.resets > 0)
		})
	}
}

func errCopilotStartf(msg string) error { return errors.Join(errCopilotStart, errors.New(msg)) }

// The composer's timeout mapping (composeapi.timedOut) reads Status 0 and
// "deadline exceeded" in the text.
func TestCopilot_TimeoutKeepsDeadlineText(t *testing.T) {
	f := &fakeCopilot{sendErr: errors.New("waiting for response: context deadline exceeded")}
	_, err := (&copilotModel{rt: f, model: "auto"}).Call(context.Background(), copilotReq)
	ce := AsCallError(err)
	assert.Equal(t, Busy, ce.Class)
	assert.Equal(t, 0, ce.Status)
	assert.Contains(t, ce.Msg, "deadline exceeded")
}

func TestCopilot_CloseThroughInstrument(t *testing.T) {
	f := &fakeCopilot{}
	m := Instrument(&copilotModel{rt: f, model: "auto"}, "compose")
	require.NoError(t, CloseModel(m))
	assert.Equal(t, 1, f.stops)
	assert.NoError(t, CloseModel(NewOpenAI("", DefaultOpenAIBaseURL, "m")), "HTTP backends hold nothing")
}

func TestCopilotEnv_KeepsOnlyAllowed(t *testing.T) {
	got := copilotEnv([]string{"PATH=/bin", "HOME=/h", "GITHUB_TOKEN=x", "DB_PASSWORD=y", "ANTHROPIC_API_KEY=z", "HTTPS_PROXY=http://p"})
	assert.ElementsMatch(t, []string{"PATH=/bin", "HOME=/h", "HTTPS_PROXY=http://p"}, got)
}
