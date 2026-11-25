package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/redact"
)

// Embedder turns texts into vectors. Embed returns one vector per text, in
// order. Like Model.Call it makes no retries.
type Embedder interface {
	Embed(ctx context.Context, texts []string) (Embedding, error)
	// Name returns the provider and model, for logs, metrics and audit.
	Name() (provider, model string)
}

// Embedding is the answer of one Embed call.
type Embedding struct {
	Vectors [][]float32
	// InputTokens is what the provider counted; 0 when it does not say.
	InputTokens int64
	// Redactions is how many parts InstrumentEmbedder removed from the
	// texts before they were sent.
	Redactions int
}

// embedBatch is how many texts one request carries: it keeps an answer
// under maxResponseBody for vectors of a few thousand dimensions.
const embedBatch = 32

// embedInBatches calls one(texts[i:j]) for batches of embedBatch and joins
// the answers.
func embedInBatches(ctx context.Context, texts []string, one func(context.Context, []string) (Embedding, error)) (Embedding, error) {
	var out Embedding
	for i := 0; i < len(texts); i += embedBatch {
		j := min(i+embedBatch, len(texts))
		e, err := one(ctx, texts[i:j])
		if err != nil {
			return out, err
		}
		if len(e.Vectors) != j-i {
			return out, &CallError{Class: Bad, Msg: fmt.Sprintf("the provider returned %d vectors for %d texts", len(e.Vectors), j-i)}
		}
		out.Vectors = append(out.Vectors, e.Vectors...)
		out.InputTokens += e.InputTokens
	}
	return out, nil
}

type ollamaEmbedder struct {
	http    *http.Client
	baseURL string
	model   string
}

// NewOllamaEmbedder returns the Ollama embedding backend: POST
// {baseURL}/api/embed with a batch of inputs. Ollama returns L2-normalised
// vectors, so cosine and inner-product search agree.
func NewOllamaEmbedder(baseURL, model string) Embedder {
	return &ollamaEmbedder{http: newHTTPClient(), baseURL: strings.TrimRight(baseURL, "/"), model: model}
}

func (e *ollamaEmbedder) Name() (string, string) { return ProviderOllama, e.model }

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) (Embedding, error) {
	return embedInBatches(ctx, texts, func(ctx context.Context, batch []string) (Embedding, error) {
		body, err := json.Marshal(map[string]any{"model": e.model, "input": batch})
		if err != nil {
			return Embedding{}, &CallError{Class: Bad, Msg: "encode request: " + err.Error()}
		}
		data, status, hdr, ce := postJSON(ctx, e.http, e.baseURL+"/api/embed", body, nil)
		if ce != nil {
			return Embedding{}, ce
		}
		if status != http.StatusOK {
			return Embedding{}, ollamaError(status, hdr, data, e.model)
		}
		var out struct {
			Embeddings      [][]float32 `json:"embeddings"`
			PromptEvalCount int64       `json:"prompt_eval_count"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return Embedding{}, &CallError{Class: Bad, Msg: cut("decode answer: "+err.Error(), maxErrBytes)}
		}
		return Embedding{Vectors: out.Embeddings, InputTokens: out.PromptEvalCount}, nil
	})
}

type openAIEmbedder struct {
	http    *http.Client
	apiKey  string
	baseURL string
	model   string
}

// NewOpenAIEmbedder returns the OpenAI-compatible embedding backend: POST
// {baseURL}/embeddings. baseURL includes the version path, as for
// NewOpenAI (https://api.openai.com/v1). apiKey may be empty for a local
// server.
func NewOpenAIEmbedder(apiKey, baseURL, model string) Embedder {
	return &openAIEmbedder{http: newHTTPClient(), apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), model: model}
}

func (e *openAIEmbedder) Name() (string, string) { return ProviderOpenAI, e.model }

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) (Embedding, error) {
	return embedInBatches(ctx, texts, func(ctx context.Context, batch []string) (Embedding, error) {
		body, err := json.Marshal(map[string]any{"model": e.model, "input": batch})
		if err != nil {
			return Embedding{}, &CallError{Class: Bad, Msg: "encode request: " + err.Error()}
		}
		data, status, hdr, ce := postJSON(ctx, e.http, e.baseURL+"/embeddings", body, func(r *http.Request) *CallError {
			if e.apiKey != "" {
				r.Header.Set("Authorization", "Bearer "+e.apiKey)
			}
			return nil
		})
		if ce != nil {
			return Embedding{}, ce
		}
		if status != http.StatusOK {
			var body struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(data, &body)
			return Embedding{}, httpError(classOf(status), status, hdr, body.Error.Message, fmt.Sprintf("model %s or the path /embeddings is not on the server", e.model))
		}
		var out struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
			Usage struct {
				PromptTokens int64 `json:"prompt_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(data, &out); err != nil {
			return Embedding{}, &CallError{Class: Bad, Msg: cut("decode answer: "+err.Error(), maxErrBytes)}
		}
		vecs := make([][]float32, len(batch))
		for _, d := range out.Data {
			if d.Index < 0 || d.Index >= len(vecs) {
				return Embedding{}, &CallError{Class: Bad, Msg: fmt.Sprintf("the provider returned index %d for %d texts", d.Index, len(batch))}
			}
			vecs[d.Index] = d.Embedding
		}
		for i, v := range vecs {
			if v == nil {
				return Embedding{}, &CallError{Class: Bad, Msg: fmt.Sprintf("the provider returned no vector for text %d", i)}
			}
		}
		return Embedding{Vectors: vecs, InputTokens: out.Usage.PromptTokens}, nil
	})
}

// GuardEmbedder returns e behind the allowlist, as Guard does for a Model:
// every Embed is decided again, and a refused one returns
// ErrProviderNotAllowed without e seeing the texts.
func GuardEmbedder(e Embedder, list ProviderAllowlist, endpoint string, local bool, report func(context.Context, ProviderEvent)) Embedder {
	return &guardedEmbedder{Embedder: e, g: &guarded{Model: embedderName{e}, list: list, endpoint: endpoint, local: local, report: report}}
}

// embedderName lets the Model guard decide for an Embedder: admit only
// asks for the name.
type embedderName struct{ e Embedder }

func (n embedderName) Name() (string, string) { return n.e.Name() }
func (n embedderName) Call(context.Context, Request) (Reply, error) {
	return Reply{}, errors.New("embedderName is not callable")
}

type guardedEmbedder struct {
	Embedder
	g *guarded
}

func (g *guardedEmbedder) Embed(ctx context.Context, texts []string) (Embedding, error) {
	if err := g.g.admit(ctx); err != nil {
		return Embedding{}, err
	}
	return g.Embedder.Embed(ctx, texts)
}

// InstrumentEmbedder wraps e as Instrument wraps a Model, with the same
// options: per call one span ("ai.model.embed"), the allowlist when e is a
// GuardEmbedder, redaction of every text (redact.Prompt), the audit start
// and end rows (the prompt hash is of the redacted texts; tokens are input
// only) and the metrics, with output tokens 0.
//
// Redaction changes what is embedded: a document whose secret is removed
// is found by its other words, never by the secret.
func InstrumentEmbedder(e Embedder, component string, opts ...Option) Embedder {
	i := &instrumented{component: component, metrics: defaultMetrics, tracer: DefaultTracerName}
	for _, o := range opts {
		o(i)
	}
	return &instrumentedEmbedder{Embedder: e, i: i}
}

type instrumentedEmbedder struct {
	Embedder
	i *instrumented
}

func (w *instrumentedEmbedder) Embed(ctx context.Context, texts []string) (Embedding, error) {
	i := w.i
	provider, model := w.Name()
	ctx, span := otel.Tracer(i.tracer).Start(withComponent(ctx, i.component), "ai.model.embed")
	defer span.End()
	span.SetAttributes(
		attribute.String("ai.component", i.component),
		attribute.String("ai.provider", provider),
		attribute.String("ai.model", model),
		attribute.Int("ai.texts", len(texts)),
	)

	next := w.Embedder
	if g, ok := next.(*guardedEmbedder); ok {
		if err := g.g.admit(ctx); err != nil {
			span.SetAttributes(attribute.String("ai.outcome", "provider_refused"))
			span.SetStatus(codes.Error, err.Error())
			i.metrics.RecordAIModelCall(i.component, provider, "provider_refused", 0, 0)
			return Embedding{}, err
		}
		next = g.Embedder
	}

	clean := make([]string, len(texts))
	redactions := 0
	for k, t := range texts {
		out, n := redact.PromptCount(t)
		clean[k], redactions = out, redactions+n
	}
	span.SetAttributes(attribute.Bool("ai.prompt_redacted", redactions > 0), attribute.Int("ai.prompt_redactions", redactions))
	i.metrics.RecordAIPromptRedactions(i.component, redactions)

	var call *AuditedCall
	if i.audit != nil {
		caller, via := callerOf(ctx)
		call = &AuditedCall{
			Component: i.component, Provider: provider, Model: model, Caller: caller, Via: via,
			PromptVersion: promptVersionOf(ctx), PromptHash: textsHash(clean), RedactionCount: redactions,
		}
		if sc := span.SpanContext(); sc.HasTraceID() {
			call.RequestID = sc.TraceID().String()
		}
		if err := i.audit.CallStarted(ctx, call); err != nil {
			span.SetStatus(codes.Error, "ai audit start row not written")
			i.metrics.RecordAIModelCall(i.component, provider, "audit_failed", 0, 0)
			return Embedding{}, &CallError{Class: Busy, AuditFailed: true, Msg: "the AI audit trail cannot be written; the model was not called"}
		}
	}

	began := time.Now()
	emb, err := next.Embed(ctx, clean)
	emb.Redactions = redactions
	outcome := outcomeOf(Reply{Stop: StopEnd}, err)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		outcome = "canceled"
	}
	span.SetAttributes(attribute.String("ai.outcome", outcome), attribute.Int64("ai.input_tokens", emb.InputTokens))
	if err != nil && outcome != "canceled" {
		span.SetStatus(codes.Error, err.Error())
	}
	if call != nil {
		ok := err == nil
		call.Outcome, call.Valid = outcome, &ok
		if err != nil {
			call.Valid = nil
		}
		call.TokensIn, call.Duration = emb.InputTokens, time.Since(began)
		if aerr := i.audit.CallEnded(context.WithoutCancel(ctx), call); aerr != nil {
			span.SetStatus(codes.Error, "ai audit end row not written")
			i.metrics.RecordAIModelCall(i.component, provider, "audit_failed", emb.InputTokens, 0)
			return Embedding{InputTokens: emb.InputTokens, Redactions: redactions},
				&CallError{Class: Bad, AuditFailed: true, Msg: "the AI audit trail cannot be written; the answer was discarded"}
		}
	}
	i.metrics.RecordAIModelCall(i.component, provider, outcome, emb.InputTokens, 0)
	return emb, err
}

// textsHash is hex SHA-256 of the texts as sent, as JSON: the audit row's
// prompt hash of an embedding call.
func textsHash(texts []string) string {
	b, _ := json.Marshal(texts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
