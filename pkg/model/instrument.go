package model

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/redact"
)

// Metrics counts model calls. Instrument calls it once per call; set it
// with WithMetrics. Labels: component is the Instrument component,
// provider the backend, outcome one of ok, max_tokens, refusal, busy,
// refused, not_found, bad, canceled, provider_refused, tools_unsupported,
// audit_failed.
type Metrics interface {
	RecordAIModelCall(component, provider, outcome string, inputTokens, outputTokens int64)
	// RecordAIPromptRedactions gets the number of parts redaction removed
	// from one request, also 0.
	RecordAIPromptRedactions(component string, n int)
}

// defaultMetrics is what Instrument counts in without WithMetrics. Only
// tests set it, so they can count calls they make through Instrument.
var defaultMetrics Metrics = noMetrics{}

type noMetrics struct{}

func (noMetrics) RecordAIModelCall(string, string, string, int64, int64) {}
func (noMetrics) RecordAIPromptRedactions(string, int)                   {}

// DefaultTracerName is the OpenTelemetry tracer Instrument uses unless
// WithTracerName sets another.
const DefaultTracerName = "innominatus-ai-sdk/model"

// Instrument wraps m so every Call gets one span ("ai.model.call", tracer
// DefaultTracerName or WithTracerName) and is counted by the WithMetrics
// Metrics. Every feature that calls a model wraps its backend with this, so
// model calls look the same in traces and metrics whichever feature made
// them.
//
// component names the caller and must be a constant: it becomes a metric
// label. The span carries no prompt or answer text.
//
// It also redacts every prompt before the backend sees it (the system
// prompt and each turn) with redact.Prompt. This is the one place every
// model call passes, so a secret that reaches a prompt from user text,
// tool output or configuration never leaves the process. The number of
// removed parts goes to Metrics.RecordAIPromptRedactions, the span
// (ai.prompt_redactions) and Reply.Redactions; the values go nowhere.
//
// With WithAudit it records every call in an audit trail: a start row
// before the provider is called (if that fails, no call is made) and an
// end row after it (if that fails, the answer is thrown away).
//
// Order per call: the provider allowlist (when m is a Guard), redaction,
// the tool check, redaction, the audit start row (with the redaction count
// and the hash of the redacted request), the provider call, the audit end
// row. A provider the allowlist refuses gets nothing: no redaction pass, no
// trail row; the refusal is the Guard's ProviderRefused event and the
// provider_refused outcome. A request with tools or tool blocks to a
// backend that cannot run them (ToolCapable: bedrock, copilot, replay) is
// refused the same way, as Bad with outcome tools_unsupported: nothing was
// sent, so there is no call to record.
func Instrument(m Model, component string, opts ...Option) Model {
	i := &instrumented{Model: m, component: component, metrics: defaultMetrics, tracer: DefaultTracerName}
	for _, o := range opts {
		o(i)
	}
	return i
}

// Redacting returns m when it already redacts (Instrument, Redacting),
// else m wrapped so that every call is redacted as Instrument does it,
// without the span and metrics. A caller that builds requests calls
// through it, so a backend that was not instrumented, in a
// test or a new wiring, still never gets a secret.
func Redacting(m Model) Model {
	switch m.(type) {
	case *instrumented, *redacting:
		return m
	}
	return &redacting{Model: m}
}

type redacting struct{ Model }

func (r *redacting) Call(ctx context.Context, req Request) (Reply, error) {
	// The backends no longer refuse tools themselves (Instrument does), so
	// a caller that skips Instrument gets the same refusal here.
	if usesTools(req) && !SupportsTools(r.Model) {
		provider, _ := r.Name()
		return Reply{}, errNoTools(provider)
	}
	req, n := RedactRequest(req)
	rep, err := r.Model.Call(ctx, req)
	rep.Redactions = n
	return rep, err
}

// Close closes the wrapped backend.
func (r *redacting) Close() error { return CloseModel(r.Model) }

type instrumented struct {
	Model
	component string
	audit     CallAudit
	metrics   Metrics
	tracer    string
}

func (i *instrumented) Call(ctx context.Context, req Request) (Reply, error) {
	provider, model := i.Name()
	ctx, span := otel.Tracer(i.tracer).Start(withComponent(ctx, i.component), "ai.model.call")
	defer span.End()
	span.SetAttributes(
		attribute.String("ai.component", i.component),
		attribute.String("ai.provider", provider),
		attribute.String("ai.model", model),
		attribute.Int("ai.max_tokens", req.MaxTokens),
	)

	// The allowlist decides first. Instrument asks the guard itself, so a
	// refused provider costs no audit row, and calls the guarded backend
	// directly afterwards so the decision (and its ProviderUncheckedUsed
	// event in local mode) is made once per call.
	next := i.Model
	if g, ok := i.Model.(*guarded); ok {
		if err := g.admit(ctx); err != nil {
			span.SetAttributes(attribute.String("ai.outcome", "provider_refused"))
			span.SetStatus(codes.Error, err.Error())
			i.metrics.RecordAIModelCall(i.component, provider, "provider_refused", 0, 0)
			return Reply{}, err
		}
		next = g.Model
	}
	if usesTools(req) && !SupportsTools(next) {
		span.SetAttributes(attribute.String("ai.outcome", "tools_unsupported"))
		err := errNoTools(provider)
		span.SetStatus(codes.Error, err.Error())
		i.metrics.RecordAIModelCall(i.component, provider, "tools_unsupported", 0, 0)
		return Reply{}, err
	}

	req, redactions := RedactRequest(req)
	span.SetAttributes(attribute.Bool("ai.prompt_redacted", redactions > 0), attribute.Int("ai.prompt_redactions", redactions))
	i.metrics.RecordAIPromptRedactions(i.component, redactions)

	var call *AuditedCall
	if i.audit != nil {
		caller, via := callerOf(ctx)
		call = &AuditedCall{
			Component: i.component, Provider: provider, Model: model, Caller: caller, Via: via,
			PromptVersion: promptVersionOf(ctx), PromptHash: promptHash(req), RedactionCount: redactions,
		}
		if sc := span.SpanContext(); sc.HasTraceID() {
			call.RequestID = sc.TraceID().String()
		}
		if err := i.audit.CallStarted(ctx, call); err != nil {
			// The audit implementation logs its own error; the store's
			// text does not travel with the call error.
			span.SetStatus(codes.Error, "ai audit start row not written")
			i.metrics.RecordAIModelCall(i.component, provider, "audit_failed", 0, 0)
			return Reply{}, &CallError{Class: Busy, AuditFailed: true, Msg: "the AI audit trail cannot be written; the model was not called"}
		}
	}

	began := time.Now()
	rep, err := next.Call(ctx, req)
	rep.Redactions = redactions
	outcome := outcomeOf(rep, err)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		// The caller gave up (shutdown, request gone): not the model's
		// answer, so it must not show up as a busy model API. A deadline
		// stays busy: that is the model API being slow.
		outcome = "canceled"
	}
	span.SetAttributes(
		attribute.String("ai.outcome", outcome),
		attribute.Int64("ai.input_tokens", rep.InputTokens),
		attribute.Int64("ai.output_tokens", rep.OutputTokens),
	)
	if err != nil && outcome != "canceled" {
		// CallError.Msg never holds the key (model.go), so it is safe here.
		span.SetStatus(codes.Error, err.Error())
	}
	if call != nil {
		call.Outcome, call.Valid = outcome, validAnswer(req, rep, err)
		call.TokensIn, call.TokensOut, call.Duration = rep.InputTokens, rep.OutputTokens, time.Since(began)
		// The tokens are spent even when the caller went away: the end row
		// is written whatever ctx says.
		if aerr := i.audit.CallEnded(context.WithoutCancel(ctx), call); aerr != nil {
			span.SetStatus(codes.Error, "ai audit end row not written")
			i.metrics.RecordAIModelCall(i.component, provider, "audit_failed", rep.InputTokens, rep.OutputTokens)
			// Final, not busy: a retry would spend again. The token counts
			// stay so the caller's budget settles what was spent.
			return Reply{InputTokens: rep.InputTokens, OutputTokens: rep.OutputTokens, Redactions: redactions},
				&CallError{Class: Bad, AuditFailed: true, Msg: "the AI audit trail cannot be written; the answer was discarded"}
		}
	}
	i.metrics.RecordAIModelCall(i.component, provider, outcome, rep.InputTokens, rep.OutputTokens)
	return rep, err
}

// Close closes the wrapped backend when it holds a process (copilot).
func (i *instrumented) Close() error { return CloseModel(i.Model) }

// CloseModel releases what a backend holds: the copilot backend's CLI
// process. The HTTP backends hold nothing; for them it is a no-op. Call it
// on shutdown.
func CloseModel(m Model) error {
	if c, ok := m.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

// outcomeOf is the metric label of one call: the error class for a failed
// call, else how the model stopped.
func outcomeOf(rep Reply, err error) string {
	if errors.Is(err, ErrProviderNotAllowed) {
		return "provider_refused"
	}
	if err != nil {
		switch AsCallError(err).Class {
		case Busy:
			return "busy"
		case Refused:
			return "refused"
		case NotFound:
			return "not_found"
		}
		return "bad"
	}
	switch rep.Stop {
	case StopMaxTokens:
		return "max_tokens"
	case StopRefusal:
		return "refusal"
	}
	return "ok"
}

// RedactRequest returns req with its system prompt and turns redacted, and
// how many parts were removed: each turn's text, each tool_result's
// content and the string values of each tool_use's input (a tool loop
// sends them again on every later call). The schema and the tool
// definitions are the caller's own and stay. The caller's request is not
// changed, so a tool loop can keep the unredacted tool output for its own
// checks.
func RedactRequest(req Request) (Request, int) {
	total := 0
	scrub := func(s string) string {
		out, n := redact.PromptCount(s)
		total += n
		return out
	}
	req.System = scrub(req.System)
	turns := make([]Turn, len(req.Turns))
	for i, t := range req.Turns {
		turns[i] = Turn{Role: t.Role, Text: scrub(t.Text)}
		if len(t.Blocks) == 0 {
			continue
		}
		blocks := make([]Block, len(t.Blocks))
		for j, b := range t.Blocks {
			b.Content = scrub(b.Content)
			b.Input = scrubJSONStrings(b.Input, scrub)
			blocks[j] = b
		}
		turns[i].Blocks = blocks
	}
	req.Turns = turns
	return req, total
}

// scrubJSONStrings runs scrub on every string value in raw and returns the
// JSON again; raw that is not JSON is scrubbed as text.
func scrubJSONStrings(raw json.RawMessage, scrub func(string) string) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(scrub(string(raw)))
	}
	out, err := json.Marshal(scrubValue(v, scrub))
	if err != nil {
		return json.RawMessage(scrub(string(raw)))
	}
	return out
}

func scrubValue(v any, scrub func(string) string) any {
	switch x := v.(type) {
	case string:
		return scrub(x)
	case []any:
		for i := range x {
			x[i] = scrubValue(x[i], scrub)
		}
	case map[string]any:
		for k := range x {
			x[k] = scrubValue(x[k], scrub)
		}
	}
	return v
}
