package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// CallAudit records every model call twice: CallStarted before the
// provider is called, CallEnded after it. Instrument calls it; an
// implementation typically appends to an append-only audit trail. Neither
// call gets prompt or answer text.
type CallAudit interface {
	// CallStarted records the start and sets c.StartSeq. An error means
	// no call is made.
	CallStarted(ctx context.Context, c *AuditedCall) error
	// CallEnded records the end. An error means the answer is thrown away.
	CallEnded(ctx context.Context, c *AuditedCall) error
}

// AuditedCall is what the trail keeps of one model call.
type AuditedCall struct {
	Component, Provider, Model string
	// Caller and Via come from WithAuditCaller; empty for system work.
	Caller, Via string
	// PromptVersion comes from WithPromptVersion.
	PromptVersion string
	// RequestID is the trace ID of the call's span.
	RequestID string
	// PromptHash is hex SHA-256 of the redacted request as it was sent.
	PromptHash string
	// RedactionCount is how many prompt parts the redaction changed.
	RedactionCount int
	// StartSeq is the start row's place in the trail, set by CallStarted.
	StartSeq int64

	// Set before CallEnded.
	Outcome   string
	Valid     *bool
	TokensIn  int64
	TokensOut int64
	Duration  time.Duration
}

type auditCallerKey struct{}
type promptVersionKey struct{}

type auditCaller struct{ caller, via string }

// WithAuditCaller names who asked for the model calls made with ctx (a
// username) and through what (a route such as "POST /api/..."), for the
// audit trail. via must come from code, never from the request.
func WithAuditCaller(ctx context.Context, caller, via string) context.Context {
	return context.WithValue(ctx, auditCallerKey{}, auditCaller{caller: caller, via: via})
}

// WithPromptVersion names the prompt version of the model calls made with
// ctx, for the audit trail.
func WithPromptVersion(ctx context.Context, version string) context.Context {
	return context.WithValue(ctx, promptVersionKey{}, version)
}

func callerOf(ctx context.Context) (string, string) {
	c, _ := ctx.Value(auditCallerKey{}).(auditCaller)
	return c.caller, c.via
}

func promptVersionOf(ctx context.Context) string {
	v, _ := ctx.Value(promptVersionKey{}).(string)
	return v
}

// Option changes what Instrument adds.
type Option func(*instrumented)

// WithAudit makes Instrument record every call in a. With a nil a calls
// are not recorded.
func WithAudit(a CallAudit) Option {
	return func(i *instrumented) { i.audit = a }
}

// WithMetrics makes Instrument count every call in m. Without it, or with
// a nil m, calls are not counted.
func WithMetrics(m Metrics) Option {
	return func(i *instrumented) {
		if m != nil {
			i.metrics = m
		}
	}
}

// WithTracerName sets the OpenTelemetry tracer name of Instrument's spans.
func WithTracerName(name string) Option {
	return func(i *instrumented) { i.tracer = name }
}

// PromptHash is hex SHA-256 of the request as sent: system prompt, schema,
// turns and tools, as JSON. It is the AuditedCall's PromptHash and the
// replay key (with the prompt version), so a caller can check that a
// request still matches its recording.
func PromptHash(req Request) string { return promptHash(req) }

func promptHash(req Request) string {
	b, err := json.Marshal(struct {
		System string          `json:"system"`
		Schema json.RawMessage `json:"schema,omitempty"`
		Turns  []Turn          `json:"turns"`
		Tools  []ToolDef       `json:"tools,omitempty"`
	}{req.System, req.Schema, req.Turns, req.Tools})
	if err != nil {
		// A schema that is not JSON: hash it as text.
		b = []byte(req.System + "\n" + string(req.Schema))
		for _, t := range req.Turns {
			b = append(b, "\n"+t.Role+"\n"+t.Text...)
		}
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// validAnswer says whether a reply can be used at all: the model ended
// normally and, when the request has a schema, the text is one JSON value.
// Whether its content passes the feature's own checks is the feature's
// audit event. nil when the call failed.
func validAnswer(req Request, rep Reply, err error) *bool {
	if err != nil {
		return nil
	}
	ok := rep.Stop == StopEnd && (len(req.Schema) == 0 || json.Valid(bytes.TrimSpace([]byte(rep.Text))))
	return &ok
}
