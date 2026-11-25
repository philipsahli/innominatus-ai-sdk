// Package llmtest pins the model requests a feature sends. A test gives
// the feature Backend.Model() instead of its real backend, runs one call
// path with fixed input, and Backend.Assert compares every request the
// backend received with a golden file under testdata/ai-requests.
//
// Each golden call holds:
//   - the request as the backend got it (after Instrument's redaction):
//     system, schema, turns, tools, max tokens;
//   - its prompt hash and prompt version: the replay backend looks a
//     recording up by <prompt version>/<prompt hash>, so a changed hash
//     means the recordings of that call stop matching;
//   - the audit caller and via;
//   - the JSON body the anthropic backend posts for it, so a change in the
//     provider conversion shows up too.
//
// Regenerate after an intended change with
//
//	go test ./your/packages -run TestGoldenRequests -update-ai-requests
//
// and review the diff: every changed prompt_hash is a recording that has
// to be made again. Only packages that import llmtest know the flag, so
// name them instead of ./....
package llmtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

var update = flag.Bool("update-ai-requests", false, "rewrite testdata/ai-requests/*.json from the requests the callers send now")

// Component is the Instrument component of Backend.Model; it is not part
// of the golden file (the real component is set by the caller's wiring).
const Component = "golden"

// ModelName is the model name the anthropic body carries; the real one
// comes from the caller's configuration, not from the feature.
const ModelName = "golden-model"

// Backend is a fake model backend that answers from a script and records
// every request it receives.
type Backend struct {
	mu      sync.Mutex
	replies []model.Reply
	calls   []Call
	pending *model.AuditedCall
	extra   map[string]any
}

// Call is one recorded request, as written to the golden file.
type Call struct {
	PromptVersion string          `json:"prompt_version"`
	PromptHash    string          `json:"prompt_hash"`
	Caller        string          `json:"caller"`
	Via           string          `json:"via"`
	Request       Request         `json:"request"`
	AnthropicBody json.RawMessage `json:"anthropic_body"`
}

// Request is model.Request with JSON names.
type Request struct {
	System    string          `json:"system"`
	Schema    json.RawMessage `json:"schema,omitempty"`
	Turns     []model.Turn    `json:"turns"`
	Tools     []model.ToolDef `json:"tools,omitempty"`
	MaxTokens int             `json:"max_tokens"`
}

type file struct {
	Calls []Call         `json:"calls"`
	Extra map[string]any `json:"extra,omitempty"`
}

// New returns a backend that answers the n-th call with replies[n]; a call
// past the script gets a Bad error, which the golden file then shows as an
// extra call.
func New(replies ...model.Reply) *Backend {
	return &Backend{replies: replies}
}

// Model is the backend wrapped the way production code should wrap it: Instrument with
// an audit trail, so the request is redacted and hashed by the production
// code.
func (b *Backend) Model() model.Model {
	return model.Instrument(&raw{b}, Component, model.WithAudit(audit{b}))
}

// Raw is the backend without Instrument, for a caller that must be given
// a plain backend. Its calls carry no prompt hash.
func (b *Backend) Raw() model.Model { return &raw{b} }

// Extra adds a caller-specific value to the golden file (for example the
// hashes the caller stores itself).
func (b *Backend) Extra(key string, v any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.extra == nil {
		b.extra = map[string]any{}
	}
	b.extra[key] = v
}

// Requests returns the requests received so far.
func (b *Backend) Requests() []Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Request, len(b.calls))
	for i, c := range b.calls {
		out[i] = c.Request
	}
	return out
}

// Assert compares the recorded calls with testdata/ai-requests/<name>.json,
// or rewrites that file with -update-ai-requests.
func (b *Backend) Assert(t *testing.T, name string) {
	t.Helper()
	b.mu.Lock()
	f := file{Calls: b.calls, Extra: b.extra}
	b.mu.Unlock()
	if len(f.Calls) == 0 {
		t.Fatal("aigolden: the backend received no call")
	}
	for i := range f.Calls {
		f.Calls[i].AnthropicBody = anthropicBody(t, f.Calls[i].Request)
	}
	got, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("aigolden: %v", err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "ai-requests", name+".json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) // #nosec G304 -- test data path built from a constant
	if err != nil {
		t.Fatalf("aigolden: %v (run with -update-ai-requests to create it)", err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("aigolden: the model requests differ from %s.\n"+
			"A changed prompt_hash means replay recordings of this call stop matching.\n"+
			"If the change is intended, rerun with -update-ai-requests and review the diff.\n%s",
			path, lineDiff(string(want), string(got)))
	}
}

type raw struct{ b *Backend }

func (r *raw) Name() (string, string) { return "golden", ModelName }

func (r *raw) Call(_ context.Context, req model.Request) (model.Reply, error) {
	b := r.b
	b.mu.Lock()
	defer b.mu.Unlock()
	c := Call{Request: Request{System: req.System, Schema: req.Schema, Turns: req.Turns, Tools: req.Tools, MaxTokens: req.MaxTokens}}
	if p := b.pending; p != nil {
		c.PromptVersion, c.PromptHash, c.Caller, c.Via = p.PromptVersion, p.PromptHash, p.Caller, p.Via
		b.pending = nil
	}
	i := len(b.calls)
	b.calls = append(b.calls, c)
	if i >= len(b.replies) {
		return model.Reply{}, &model.CallError{Class: model.Bad, Msg: "aigolden: no scripted reply left"}
	}
	return b.replies[i], nil
}

// audit hands the start row's hash and version to the next raw call;
// Instrument writes the start row right before it calls the backend.
type audit struct{ b *Backend }

func (a audit) CallStarted(_ context.Context, c *model.AuditedCall) error {
	a.b.mu.Lock()
	defer a.b.mu.Unlock()
	cp := *c
	a.b.pending = &cp
	return nil
}

func (a audit) CallEnded(context.Context, *model.AuditedCall) error { return nil }

// anthropicBody is the JSON the anthropic backend posts for req, with its
// keys sorted.
func anthropicBody(t *testing.T, req Request) json.RawMessage {
	t.Helper()
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_golden","type":"message","role":"assistant","model":"`+ModelName+
			`","content":[{"type":"text","text":"{}"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	m := model.NewAnthropic("golden-key", srv.URL, ModelName)
	_, err := m.Call(context.Background(), model.Request{
		System: req.System, Schema: req.Schema, Turns: req.Turns, Tools: req.Tools, MaxTokens: req.MaxTokens,
	})
	var ce *model.CallError
	if err != nil && (!errors.As(err, &ce) || body == nil) {
		t.Fatalf("aigolden: the anthropic backend sent nothing: %v", err)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("aigolden: anthropic body is not JSON: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// lineDiff shows the first differing line of two texts with a few lines
// of context; the golden lines can be long, so a full diff is not useful.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	i := 0
	for i < len(w) && i < len(g) && w[i] == g[i] {
		i++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "first difference at line %d:\n", i+1)
	for j := max(0, i-2); j < i; j++ {
		fmt.Fprintf(&b, "  %s\n", w[j])
	}
	for j := i; j < min(len(w), i+3); j++ {
		fmt.Fprintf(&b, "- %s\n", w[j])
	}
	for j := i; j < min(len(g), i+3); j++ {
		fmt.Fprintf(&b, "+ %s\n", g[j])
	}
	return b.String()
}
