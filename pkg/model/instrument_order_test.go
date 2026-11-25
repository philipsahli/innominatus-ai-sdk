package model

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// anthropicRecorder is an allowlist-visible provider fake that keeps the
// request it was sent.
type anthropicRecorder struct {
	called int
	got    Request
}

func (r *anthropicRecorder) Call(_ context.Context, req Request) (Reply, error) {
	r.called++
	r.got = req
	return Reply{Text: "{}", Stop: StopEnd, InputTokens: 3, OutputTokens: 1}, nil
}
func (r *anthropicRecorder) Name() (string, string) { return ProviderAnthropic, "claude-sonnet-5-5" }

const orderPrompt = "token=abcdefghijklmnop0123 please"

// With the allowlist (#1089), redaction (#1087) and the AI audit trail
// (#1090) on one call, the order is: guard, redaction, audit start row,
// provider, audit end row. A refused provider gets nothing: no redaction
// pass, no trail row, no call.
func TestInstrument_GuardRefusesBeforeRedactionAndAudit(t *testing.T) {
	fake := &anthropicRecorder{}
	trail := &recordingAudit{}
	events := &recordedAudit{}
	redactions0 := series(t, `innominatus_ai_prompt_redactions_total{component="order_test"}`)
	m := Instrument(Guard(fake, ProviderAllowlist{File: "/etc/ai.yaml"}, "https://api.anthropic.com", false, events.report), "order_test", WithAudit(trail))

	_, err := m.Call(context.Background(), Request{Turns: []Turn{{Role: "user", Text: orderPrompt}}})
	if !errors.Is(err, ErrProviderNotAllowed) {
		t.Fatalf("err = %v, want ErrProviderNotAllowed", err)
	}
	if fake.called != 0 {
		t.Errorf("provider called %d times", fake.called)
	}
	if len(trail.started) != 0 || len(trail.ended) != 0 {
		t.Errorf("trail rows: %d start, %d end; want none for a refused provider", len(trail.started), len(trail.ended))
	}
	if d := series(t, `innominatus_ai_prompt_redactions_total{component="order_test"}`) - redactions0; d != 0 {
		t.Errorf("redactions counted %d for a call that was refused first", d)
	}
	if len(events.events) != 1 || events.events[0].Kind != ProviderRefused {
		t.Errorf("audit events = %+v", events.events)
	}
}

// An allowed call (local mode, contract not checked): one guard
// decision and event, then the start row holds the redaction count and the
// hash of what the provider got, then the end row.
func TestInstrument_AllowedCallRedactsThenAuditsThenCalls(t *testing.T) {
	fake := &anthropicRecorder{}
	trail := &recordingAudit{}
	events := &recordedAudit{}
	m := Instrument(Guard(fake, ProviderAllowlist{}, "https://api.anthropic.com", true, events.report), "order_test", WithAudit(trail))

	rep, err := m.Call(context.Background(), Request{Turns: []Turn{{Role: "user", Text: orderPrompt}}})
	if err != nil {
		t.Fatal(err)
	}
	if fake.called != 1 || strings.Contains(fake.got.Turns[0].Text, "abcdefghijklmnop0123") {
		t.Fatalf("called %d, sent %q", fake.called, fake.got.Turns[0].Text)
	}
	if len(events.events) != 1 || events.events[0].Kind != ProviderUncheckedUsed {
		t.Errorf("guard decided %d times: %+v", len(events.events), events.events)
	}
	if len(trail.started) != 1 || len(trail.ended) != 1 {
		t.Fatalf("trail rows: %d start, %d end", len(trail.started), len(trail.ended))
	}
	s := trail.started[0]
	if s.RedactionCount != rep.Redactions || s.RedactionCount == 0 {
		t.Errorf("start row redactions %d, reply %d", s.RedactionCount, rep.Redactions)
	}
	if s.PromptHash != promptHash(fake.got) {
		t.Error("start row hash is not the hash of the request the provider got")
	}
	if s.Provider != ProviderAnthropic || trail.ended[0].Outcome != "ok" {
		t.Errorf("start %+v end %+v", s, trail.ended[0])
	}
}
