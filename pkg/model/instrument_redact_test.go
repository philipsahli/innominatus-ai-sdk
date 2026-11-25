package model

import (
	"context"
	"strings"
	"testing"
)

// recordModel keeps the request the backend got.
type recordModel struct{ got Request }

func (r *recordModel) Call(_ context.Context, req Request) (Reply, error) {
	r.got = req
	return Reply{Text: "{}"}, nil
}
func (r *recordModel) Name() (string, string) { return "stub", "stub-1" }

// A secret that reaches a prompt (a step output, a driver's text) never
// reaches the model: the system prompt and every turn are redacted.
func TestInstrument_RedactsEveryPrompt(t *testing.T) {
	inner := &recordModel{}
	m := Instrument(inner, "instrument_test")
	secrets := []string{"hunter2-very-secret", "AKIAIOSFODNN7EXAMPLE", "s3cr3t-json-value"}
	req := Request{
		System: "Use the database. password=hunter2-very-secret",
		Turns: []Turn{
			{Role: "user", Text: "Size it. The key is AKIAIOSFODNN7EXAMPLE."},
			{Role: "assistant", Text: `{"size":"small","api_key":"s3cr3t-json-value"}`},
		},
		MaxTokens: 10,
	}
	if _, err := m.Call(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	all := inner.got.System
	for _, tr := range inner.got.Turns {
		all += "\n" + tr.Text
	}
	for _, s := range secrets {
		if strings.Contains(all, s) {
			t.Errorf("the model got %q:\n%s", s, all)
		}
	}
	if !strings.Contains(all, `"size":"small"`) || !strings.Contains(all, "Size it.") {
		t.Errorf("redaction removed more than the secrets:\n%s", all)
	}
	if req.Turns[0].Text != "Size it. The key is AKIAIOSFODNN7EXAMPLE." {
		t.Error("the caller's request was changed in place")
	}
}

func TestRedactRequest_CountsTheRemovedParts(t *testing.T) {
	if _, n := RedactRequest(Request{System: "plain", Turns: []Turn{{Role: "user", Text: "no secret here"}}}); n != 0 {
		t.Errorf("count = %d for a prompt without secrets", n)
	}
	if _, n := RedactRequest(Request{System: "token=abcdef0123456789", Turns: []Turn{{Role: "user", Text: "token=abcdef0123456789 and AKIAABCDEFGHIJKLMNOP"}}}); n != 3 {
		t.Errorf("count = %d, want 3", n)
	}
}

// Instrument and Redacting report the count on the reply; Redacting does
// not wrap a model that already redacts.
func TestRedacting_RedactsAndCounts(t *testing.T) {
	inner := &recordModel{}
	for name, m := range map[string]Model{"instrument": Instrument(inner, "instrument_test"), "redacting": Redacting(inner)} {
		rep, err := m.Call(context.Background(), Request{Turns: []Turn{{Role: "user", Text: "password=hunter2-very-secret"}}})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Redactions != 1 || strings.Contains(inner.got.Turns[0].Text, "hunter2") {
			t.Errorf("%s: redactions %d, sent %q", name, rep.Redactions, inner.got.Turns[0].Text)
		}
	}
	inst := Instrument(inner, "instrument_test")
	if Redacting(inst) != inst {
		t.Error("Redacting wrapped an instrumented model again")
	}
	if r := Redacting(inner); Redacting(r) != r {
		t.Error("Redacting wrapped itself")
	}
}
