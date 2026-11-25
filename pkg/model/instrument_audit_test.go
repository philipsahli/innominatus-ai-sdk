package model

import (
	"context"
	"encoding/json"
	"errors"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingAudit keeps the calls it was given; startErr and endErr make
// the trail fail.
type recordingAudit struct {
	started, ended   []AuditedCall
	startErr, endErr error
}

func (a *recordingAudit) CallStarted(_ context.Context, c *AuditedCall) error {
	if a.startErr != nil {
		return a.startErr
	}
	c.StartSeq = int64(len(a.started) + 1)
	a.started = append(a.started, *c)
	return nil
}

func (a *recordingAudit) CallEnded(_ context.Context, c *AuditedCall) error {
	if a.endErr != nil {
		return a.endErr
	}
	a.ended = append(a.ended, *c)
	return nil
}

// countingModel records whether it was called.
type countingModel struct {
	stubModel
	calls *int
}

func (c countingModel) Call(ctx context.Context, r Request) (Reply, error) {
	*c.calls++
	return c.stubModel.Call(ctx, r)
}

func TestInstrument_Audit_WritesStartAndEndWithoutText(t *testing.T) {
	a := &recordingAudit{}
	m := Instrument(stubModel{rep: Reply{Text: ` {"ok":true} `, InputTokens: 11, OutputTokens: 4}}, "instrument_test", WithAudit(a))
	ctx := WithPromptVersion(WithAuditCaller(context.Background(), "sam", "POST /api/experimental/compose/draft-type"), "draft-type-v1")
	req := Request{System: "you draft types", Turns: []Turn{{Role: "user", Text: "token=abcdefghijklmnop0123 please"}}, Schema: json.RawMessage(`{"type":"object"}`)}

	_, err := m.Call(ctx, req)
	require.NoError(t, err)
	require.Len(t, a.started, 1)
	require.Len(t, a.ended, 1)
	s, e := a.started[0], a.ended[0]
	assert.Equal(t, "instrument_test", s.Component)
	assert.Equal(t, "stub", s.Provider)
	assert.Equal(t, "stub-1", s.Model)
	assert.Equal(t, "sam", s.Caller)
	assert.Equal(t, "POST /api/experimental/compose/draft-type", s.Via)
	assert.Equal(t, "draft-type-v1", s.PromptVersion)
	assert.Len(t, s.PromptHash, 64)
	assert.Equal(t, 1, s.RedactionCount, "the turn held a token")

	assert.Equal(t, int64(1), e.StartSeq)
	assert.Equal(t, "ok", e.Outcome)
	require.NotNil(t, e.Valid)
	assert.True(t, *e.Valid)
	assert.Equal(t, int64(11), e.TokensIn)
	assert.Equal(t, int64(4), e.TokensOut)

	// The hash is of what was sent: the redacted prompt.
	sent, _ := RedactRequest(req)
	assert.Equal(t, promptHash(sent), s.PromptHash)
	assert.NotEqual(t, promptHash(req), s.PromptHash)

	// No field holds prompt or answer text.
	b, err := json.Marshal([]AuditedCall{s, e})
	require.NoError(t, err)
	for _, text := range []string{"you draft types", "please", "abcdefghijklmnop0123", `"ok":true`} {
		assert.NotContains(t, string(b), text)
	}
}

func TestInstrument_Audit_Validity(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	for _, tt := range []struct {
		name  string
		req   Request
		rep   Reply
		err   error
		valid *bool
	}{
		{"json with schema", Request{Schema: schema}, Reply{Text: `{}`}, nil, ptr(true)},
		{"not json with schema", Request{Schema: schema}, Reply{Text: "sure, here"}, nil, ptr(false)},
		{"text without schema", Request{}, Reply{Text: "plain"}, nil, ptr(true)},
		{"cut at max tokens", Request{Schema: schema}, Reply{Text: `{}`, Stop: StopMaxTokens}, nil, ptr(false)},
		{"refusal", Request{}, Reply{Stop: StopRefusal}, nil, ptr(false)},
		{"failed call", Request{}, Reply{}, &CallError{Class: Bad}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := &recordingAudit{}
			m := Instrument(stubModel{rep: tt.rep, err: tt.err}, "instrument_test", WithAudit(a))
			_, _ = m.Call(context.Background(), tt.req)
			require.Len(t, a.ended, 1)
			assert.Equal(t, tt.valid, a.ended[0].Valid)
		})
	}
}

func TestInstrument_Audit_StartFails_NoModelCall(t *testing.T) {
	calls := 0
	a := &recordingAudit{startErr: errors.New("database is down: password=secretsecret")}
	m := Instrument(countingModel{stubModel: stubModel{rep: Reply{Text: "{}"}}, calls: &calls}, "instrument_test", WithAudit(a))

	_, err := m.Call(context.Background(), Request{})
	require.Error(t, err)
	assert.Equal(t, 0, calls, "no start row, no call")
	ce := AsCallError(err)
	assert.Equal(t, Busy, ce.Class, "worth a retry: nothing was spent")
	assert.True(t, ce.AuditFailed)
	assert.NotContains(t, err.Error(), "secretsecret", "the store's error is not passed on")
	assert.Empty(t, a.ended)
}

func TestInstrument_Audit_EndFails_AnswerDiscarded(t *testing.T) {
	a := &recordingAudit{endErr: errors.New("database is down")}
	m := Instrument(stubModel{rep: Reply{Text: `{"secret":"answer"}`, InputTokens: 5, OutputTokens: 2}}, "instrument_test", WithAudit(a))

	rep, err := m.Call(context.Background(), Request{})
	require.Error(t, err)
	ce := AsCallError(err)
	assert.Equal(t, Bad, ce.Class, "final: the answer is gone and the tokens are spent")
	assert.True(t, ce.AuditFailed)
	assert.Empty(t, rep.Text, "the answer is thrown away")
	assert.Equal(t, int64(5), rep.InputTokens, "the spend still settles the budget")
	assert.Equal(t, int64(2), rep.OutputTokens)
}

func TestInstrument_NoAudit_Unchanged(t *testing.T) {
	m := Instrument(stubModel{rep: Reply{Text: "{}"}}, "instrument_test", WithAudit(nil))
	rep, err := m.Call(context.Background(), Request{})
	require.NoError(t, err)
	assert.Equal(t, "{}", rep.Text)
}

func TestPromptHash_InvalidSchemaStillHashes(t *testing.T) {
	h := promptHash(Request{System: "s", Schema: json.RawMessage(`{not json`)})
	assert.Len(t, h, 64)
}

func ptr(b bool) *bool { return &b }
