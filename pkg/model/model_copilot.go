package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/redact"
)

// ProviderCopilot is the GitHub Copilot agent runtime, driven through the
// Copilot SDK: the SDK starts the Copilot CLI as a child process and talks
// JSON-RPC to it over stdio. It needs a Copilot subscription. GitHub Models,
// the plain HTTP API, was retired on 2026-07-30.
const ProviderCopilot = "copilot"

// CopilotModelAuto lets the runtime pick the model for each call. Any other
// model name must be the one that served the call (see Call).
const CopilotModelAuto = "auto"

// copilotDisabledMCPServers are the runtime's built-in MCP servers. The
// GitHub one connects with the user's credentials even when no tool is
// offered to the model, so it is not started at all.
var copilotDisabledMCPServers = []string{"github-mcp-server"}

// copilotEnvAllowed is what the CLI process inherits from the caller's
// environment: enough to run, reach GitHub through a proxy and find the
// gh CLI login. Everything else (database passwords, API keys, a
// GITHUB_TOKEN meant for something else, which the runtime would pick up
// for authentication) stays out of the child process.
var copilotEnvAllowed = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "TMPDIR": true, "LANG": true, "LC_ALL": true,
	"XDG_CONFIG_HOME": true, "GH_CONFIG_DIR": true, "GH_HOST": true,
	"HTTPS_PROXY": true, "https_proxy": true, "HTTP_PROXY": true, "http_proxy": true, "NO_PROXY": true, "no_proxy": true,
	"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "NODE_EXTRA_CA_CERTS": true,
}

func copilotEnv(environ []string) []string {
	out := []string{}
	for _, kv := range environ {
		if k, _, ok := strings.Cut(kv, "="); ok && copilotEnvAllowed[k] {
			out = append(out, kv)
		}
	}
	return out
}

// copilotRuntime is the part of the Copilot SDK the backend uses; the
// tests replace it with a fake.
type copilotRuntime interface {
	createSession(ctx context.Context, cfg *copilot.SessionConfig) (copilotSession, error)
	// healthy reports whether the CLI process still answers; a runtime that
	// does not is restarted on the next call.
	healthy(ctx context.Context) bool
	reset()
	stop() error
}

type copilotSession interface {
	on(h copilot.SessionEventHandler) func()
	sendAndWait(ctx context.Context, opts copilot.MessageOptions) (*copilot.SessionEvent, error)
	disconnect() error
}

type copilotModel struct {
	rt    copilotRuntime
	model string
}

// NewCopilot returns the Copilot backend. token is a GitHub token of a
// user with a Copilot subscription; empty means the gh CLI login of the
// user the server runs as. cliPath is the Copilot CLI executable. The CLI
// process starts on the first call and runs until Close.
func NewCopilot(token, cliPath, model string) Model {
	return &copilotModel{rt: &sdkCopilotRuntime{token: token, cliPath: cliPath}, model: model}
}

func (m *copilotModel) Name() (string, string) { return ProviderCopilot, m.model }

// Close stops the CLI process, if it was started.
func (m *copilotModel) Close() error { return m.rt.stop() }

// copilotSchemaPrompt asks for the schema in the system message. The SDK's
// ResponseSchema always sends strict mode, which rejects the callers'
// schemas (they do not set additionalProperties:false on every object), so
// the schema is not enforced by the API; the caller checks the answer.
const copilotSchemaPrompt = "\n\nAnswer with exactly one JSON object that matches this JSON Schema, and nothing else:\n"

func (m *copilotModel) sessionConfig(req Request) *copilot.SessionConfig {
	system := req.System
	if len(req.Schema) > 0 {
		system += copilotSchemaPrompt + string(req.Schema)
	}
	off := false
	return &copilot.SessionConfig{
		Model:         m.model,
		SystemMessage: &copilot.SystemMessageConfig{Mode: "replace", Content: system},
		// No tool at all. A non-nil empty list is "none" in empty mode; nil
		// is refused there.
		AvailableTools:      []string{},
		DisabledMCPServers:  copilotDisabledMCPServers,
		OnPermissionRequest: copilotDenyPermission,
		Hooks: &copilot.SessionHooks{
			OnPreToolUse: copilotDenyTool,
		},
		InfiniteSessions: &copilot.InfiniteSessionConfig{Enabled: &off},
	}
}

// copilotDenyPermission and copilotDenyTool are the second and third
// guard behind the empty tool list: should the runtime still offer a tool,
// its use is refused.
func copilotDenyPermission(copilot.PermissionRequest, copilot.PermissionInvocation) (rpc.PermissionDecision, error) {
	fb := "the caller gives the model no tools"
	return &rpc.PermissionDecisionReject{Feedback: &fb}, nil
}

func copilotDenyTool(copilot.PreToolUseHookInput, copilot.HookInvocation) (*copilot.PreToolUseHookOutput, error) {
	return &copilot.PreToolUseHookOutput{PermissionDecision: "deny", PermissionDecisionReason: "the caller gives the model no tools"}, nil
}

// copilotPrompt joins the turns into one message: a session is created per
// call, so earlier turns (the last answer and the feedback on it) are sent
// as text, labelled, oldest first.
func copilotPrompt(turns []Turn) string {
	if len(turns) == 1 {
		return turns[0].Text
	}
	var b strings.Builder
	b.WriteString("The conversation so far, oldest first. Answer the last user message.\n\n")
	for _, t := range turns {
		fmt.Fprintf(&b, "[%s]\n%s\n\n", t.Role, t.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}

// copilotUsage collects the usage events of one call.
type copilotUsage struct {
	mu           sync.Mutex
	in, out      int64
	served       string
	finish       string
	filtered     bool
	toolRequests int
}

func (u *copilotUsage) handle(e copilot.SessionEvent) {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch d := e.Data.(type) {
	case *rpc.AssistantUsageData:
		if d.InputTokens != nil {
			u.in += *d.InputTokens
		}
		if d.OutputTokens != nil {
			u.out += *d.OutputTokens
		}
		u.served = d.Model
		if d.FinishReason != nil {
			u.finish = *d.FinishReason
		}
		if d.ContentFilterTriggered != nil && *d.ContentFilterTriggered {
			u.filtered = true
		}
	case *rpc.AssistantMessageData:
		u.toolRequests += len(d.ToolRequests)
	}
}

func (m *copilotModel) Call(ctx context.Context, req Request) (Reply, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	sess, err := m.rt.createSession(ctx, m.sessionConfig(req))
	if err != nil {
		return Reply{}, m.callError(ctx, err)
	}
	defer func() { _ = sess.disconnect() }()
	var u copilotUsage
	unsubscribe := sess.on(u.handle)
	ev, err := sess.sendAndWait(ctx, copilot.MessageOptions{Prompt: copilotPrompt(req.Turns)})
	unsubscribe()

	u.mu.Lock()
	defer u.mu.Unlock()
	var text string
	if ev != nil {
		if d, ok := ev.Data.(*rpc.AssistantMessageData); ok {
			text = d.Content
		}
	}
	rep := Reply{Text: stripJSONFence(text), InputTokens: u.in, OutputTokens: u.out}
	if rep.InputTokens == 0 && rep.OutputTokens == 0 {
		// No usage event: estimate, so the budget is never settled to zero
		// for a call that may have spent tokens.
		rep.InputTokens, rep.OutputTokens = estimateTokens(req, text)
	}
	if err != nil {
		return rep, m.callError(ctx, err)
	}
	if u.toolRequests > 0 {
		// Every guard refused it, but a runtime that offers tools despite
		// the empty list is not what this backend promises.
		return rep, &CallError{Class: Bad, Msg: "the Copilot runtime offered the model a tool although none was configured; the call is refused"}
	}
	if m.model != CopilotModelAuto && u.served != "" && !servedBy(m.model, u.served) {
		// The runtime routes an unknown or unavailable model to another one
		// without an error; the answer would be labelled with the wrong model.
		return rep, &CallError{Class: NotFound, Msg: cut(fmt.Sprintf("model %s is not available to this Copilot account: the runtime routed the call to %s; set the model to an available one or to auto", m.model, u.served), maxErrBytes)}
	}
	if ev == nil || strings.TrimSpace(text) == "" {
		return rep, &CallError{Class: Bad, Msg: "the Copilot runtime returned no assistant message"}
	}
	switch {
	case u.filtered || u.finish == "content_filter":
		rep.Stop = StopRefusal
	case u.finish == "length":
		rep.Stop = StopMaxTokens
	}
	return rep, nil
}

// servedBy accepts a served model that is the configured one, also with a
// version or date suffix ("claude-sonnet-4.5" served as
// "claude-sonnet-4.5-20250929"), but not another variant ("gpt-6" served
// as "gpt-6-luna").
func servedBy(configured, served string) bool {
	c, s := strings.ToLower(configured), strings.ToLower(served)
	if s == c {
		return true
	}
	rest, ok := strings.CutPrefix(s, c+"-")
	return ok && rest != "" && rest[0] >= '0' && rest[0] <= '9'
}

// estimateTokens is about four bytes per token (as Estimate), at least one
// each way.
func estimateTokens(req Request, answer string) (int64, int64) {
	n := len(req.System) + len(req.Schema)
	for _, t := range req.Turns {
		n += len(t.Text)
	}
	return max(int64(n/4), 1), max(int64(len(answer)/4), 1)
}

var jsonFence = regexp.MustCompile("(?s)^```[a-zA-Z]*\\s*\\n(.*?)\\n?```$")

// stripJSONFence removes a Markdown code fence around the whole answer:
// without an enforced response format, chat models often add one.
func stripJSONFence(s string) string {
	t := strings.TrimSpace(s)
	if m := jsonFence.FindStringSubmatch(t); m != nil {
		return strings.TrimSpace(m[1])
	}
	return s
}

// copilotStatus is the HTTP status the runtime puts into its error text
// ("Execution failed: 400 Invalid schema ...", "(401): GitHub returned").
var copilotStatus = regexp.MustCompile(`(?:Execution failed: |\()([1-5][0-9]{2})\b`)

// errCopilotStart marks a CLI process that did not start.
var errCopilotStart = errors.New("the Copilot CLI did not start")

// callError maps an SDK or runtime error to a Class. The text
// is redacted (it can echo parts of the prompt) and cut; the token is
// never in it, as the SDK passes it in the child's environment only.
func (m *copilotModel) callError(ctx context.Context, err error) *CallError {
	var ce *CallError
	if errors.As(err, &ce) {
		return ce
	}
	msg := cut(redact.Patterns(err.Error()), maxErrBytes)
	lower := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, errCopilotStart):
		return &CallError{Class: Bad, Msg: cut(msg+"; check the Copilot CLI path", maxErrBytes)}
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		if !strings.Contains(msg, "deadline exceeded") {
			msg = cut("context deadline exceeded: "+msg, maxErrBytes)
		}
		return &CallError{Class: Busy, Msg: msg}
	case strings.Contains(lower, "authenticat") || strings.Contains(lower, "bad credentials"):
		return &CallError{Class: Refused, Status: copilotStatusOf(err.Error()), Msg: msg}
	}
	if status := copilotStatusOf(err.Error()); status != 0 {
		class := classOf(status)
		if status == 402 {
			// Out of premium requests or no seat: waiting does not help.
			class = Refused
		}
		return &CallError{Class: class, Status: status, Msg: msg}
	}
	if ctx.Err() == nil && !m.rt.healthy(ctx) {
		// The CLI process died or hung: start a new one on the next call.
		m.rt.reset()
	}
	return &CallError{Class: Busy, Msg: msg}
}

func copilotStatusOf(s string) int {
	if sm := copilotStatus.FindStringSubmatch(s); sm != nil {
		n, _ := strconv.Atoi(sm[1])
		return n
	}
	return 0
}

// sdkCopilotRuntime is the real runtime: one Copilot client and CLI
// process per backend, started on the first call.
type sdkCopilotRuntime struct {
	token, cliPath string

	mu     sync.Mutex
	client *copilot.Client
	// home is the client's BaseDirectory (COPILOT_HOME) and the parent of
	// its empty working directory; removed on stop.
	home string
}

func (r *sdkCopilotRuntime) started(ctx context.Context) (*copilot.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil {
		return r.client, nil
	}
	home, err := os.MkdirTemp("", "copilot-model-")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errCopilotStart, err)
	}
	work := filepath.Join(home, "work")
	if err := os.Mkdir(work, 0o700); err != nil {
		_ = os.RemoveAll(home)
		return nil, fmt.Errorf("%w: %v", errCopilotStart, err)
	}
	opts := &copilot.ClientOptions{
		// Empty mode: no memory, session store, skills, plugins or custom
		// instructions; every session must name its tools.
		Mode:             copilot.ModeEmpty,
		BaseDirectory:    home,
		WorkingDirectory: work,
		Connection:       copilot.StdioConnection{Path: r.cliPath},
		Env:              copilotEnv(os.Environ()),
		LogLevel:         "error",
		GitHubToken:      r.token,
	}
	c := copilot.NewClient(opts)
	if err := c.Start(ctx); err != nil {
		_ = os.RemoveAll(home)
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %v", errCopilotStart, err)
	}
	r.client, r.home = c, home
	return c, nil
}

func (r *sdkCopilotRuntime) createSession(ctx context.Context, cfg *copilot.SessionConfig) (copilotSession, error) {
	c, err := r.started(ctx)
	if err != nil {
		return nil, err
	}
	s, err := c.CreateSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return sdkCopilotSession{s}, nil
}

func (r *sdkCopilotRuntime) healthy(ctx context.Context) bool {
	r.mu.Lock()
	c := r.client
	r.mu.Unlock()
	if c == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err := c.Ping(ctx, "innominatus-ai-sdk")
	return err == nil
}

func (r *sdkCopilotRuntime) reset() {
	r.mu.Lock()
	c, home := r.client, r.home
	r.client, r.home = nil, ""
	r.mu.Unlock()
	if c != nil {
		c.ForceStop()
		_ = os.RemoveAll(home)
	}
}

func (r *sdkCopilotRuntime) stop() error {
	r.mu.Lock()
	c, home := r.client, r.home
	r.client, r.home = nil, ""
	r.mu.Unlock()
	if c == nil {
		return nil
	}
	err := c.Stop()
	_ = os.RemoveAll(home)
	return err
}

type sdkCopilotSession struct{ s *copilot.Session }

func (s sdkCopilotSession) on(h copilot.SessionEventHandler) func() { return s.s.On(h) }
func (s sdkCopilotSession) sendAndWait(ctx context.Context, o copilot.MessageOptions) (*copilot.SessionEvent, error) {
	return s.s.SendAndWait(ctx, o)
}
func (s sdkCopilotSession) disconnect() error { return s.s.Disconnect() }
