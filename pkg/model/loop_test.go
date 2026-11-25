package model

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptModel answers each call with the next step of its script and
// records every request.
type scriptModel struct {
	mu    sync.Mutex
	steps []func(ctx context.Context, req Request) (Reply, error)
	reqs  []Request
}

func (m *scriptModel) Call(ctx context.Context, req Request) (Reply, error) {
	m.mu.Lock()
	n := len(m.reqs)
	m.reqs = append(m.reqs, req)
	m.mu.Unlock()
	if n >= len(m.steps) {
		return Reply{}, &CallError{Class: Bad, Msg: "script ran out"}
	}
	return m.steps[n](ctx, req)
}

func (m *scriptModel) Name() (string, string) { return "fake", "fake-1" }

func reply(text string, stop Stop) func(context.Context, Request) (Reply, error) {
	return func(context.Context, Request) (Reply, error) {
		return Reply{Text: text, Stop: stop, InputTokens: 10, OutputTokens: 5}, nil
	}
}

func toolReply(text string, uses ...ToolUse) func(context.Context, Request) (Reply, error) {
	return func(context.Context, Request) (Reply, error) {
		return Reply{Text: text, ToolUses: uses, InputTokens: 20, OutputTokens: 3}, nil
	}
}

func fail(ce *CallError) func(context.Context, Request) (Reply, error) {
	return func(context.Context, Request) (Reply, error) {
		return Reply{InputTokens: 7}, ce
	}
}

// fakeBudget allows the first allow reserves (all when allow < 0).
type fakeBudget struct {
	allow      int
	err        error
	reserved   []int64
	settled    []int64
	settledFor []Hold
	settleErr  []error
	settleDL   []bool
}

func (b *fakeBudget) Reserve(_ context.Context, est int64) (Hold, bool, error) {
	if b.err != nil {
		return Hold{}, false, b.err
	}
	if b.allow >= 0 && len(b.reserved) >= b.allow {
		return Hold{}, false, nil
	}
	b.reserved = append(b.reserved, est)
	return Hold{Day: "2026-11-21", Tokens: est}, true, nil
}

func (b *fakeBudget) Settle(ctx context.Context, h Hold, used int64) {
	b.settled = append(b.settled, used)
	b.settledFor = append(b.settledFor, h)
	_, dl := ctx.Deadline()
	b.settleErr = append(b.settleErr, ctx.Err())
	b.settleDL = append(b.settleDL, dl)
}

var firstReq = Request{System: "sys", Turns: []Turn{{Role: "user", Text: "question"}}, Schema: json.RawMessage(`{"type":"object"}`), MaxTokens: 100}

// okCheck accepts an answer that is the text "ok" and rejects the rest
// with a reason naming the try.
func okCheck(t Try) Verdict {
	if t.Reply.Stop == StopMaxTokens {
		return Verdict{Reason: "the answer was cut at the token limit"}
	}
	if t.Reply.Text == "ok" {
		return Verdict{Accept: true}
	}
	return Verdict{Reason: "not ok"}
}

func TestLoopRun_AcceptsFirstAnswer(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("ok", StopEnd)}}
	res, err := Loop{Model: m, MaxTries: 3, Check: okCheck}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAccepted, res.Outcome)
	require.Len(t, res.Tries, 1)
	assert.True(t, res.Tries[0].Accepted)
	assert.Equal(t, firstReq, m.reqs[0])
	assert.Equal(t, int64(10), res.InputTokens)
	assert.Equal(t, int64(5), res.OutputTokens)
}

func TestLoopRun_NilCheckAcceptsEverything(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("", StopMaxTokens)}}
	res, err := Loop{Model: m}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAccepted, res.Outcome)
}

func TestLoopRun_RetriesBadAnswerWithFeedback(t *testing.T) {
	tests := []struct {
		name     string
		loop     Loop
		answer   string
		wantText string
		wantFB   string
	}{
		{"default feedback, text as it came", Loop{}, "nope", "nope", RetryFeedback("not ok")},
		{"empty answer kept empty without EmptyAnswer", Loop{}, "", "", RetryFeedback("not ok")},
		{"empty answer replaced", Loop{EmptyAnswer: EmptyAnswerText}, "", "(empty answer)", RetryFeedback("not ok")},
		{"caller's feedback", Loop{Feedback: func(t Try) string { return "try " + t.Reply.Text + "!" }}, "x", "x", "try x!"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply(tt.answer, StopEnd), reply("ok", StopEnd)}}
			l := tt.loop
			l.Model, l.MaxTries, l.Check = m, 3, okCheck
			res, err := l.Run(context.Background(), firstReq)
			require.NoError(t, err)
			assert.Equal(t, OutcomeAccepted, res.Outcome)
			require.Len(t, m.reqs, 2)
			want := firstReq
			want.Turns = []Turn{firstReq.Turns[0], {Role: "assistant", Text: tt.wantText}, {Role: "user", Text: tt.wantFB}}
			assert.Equal(t, want, m.reqs[1])
			require.Len(t, res.Tries, 2)
			assert.Equal(t, "not ok", res.Tries[0].Reason)
			assert.False(t, res.Tries[0].Accepted)
			assert.Equal(t, 2, res.Tries[1].Try)
			// A try's Request is what was sent then, not the later turns.
			assert.Len(t, res.Tries[0].Request.Turns, 1)
			assert.Len(t, firstReq.Turns, 1)
		})
	}
}

func TestLoopRun_CutAnswerIsCheckedAndRetried(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("{\"a\":", StopMaxTokens), reply("ok", StopEnd)}}
	res, err := Loop{Model: m, MaxTries: 2, Check: okCheck}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAccepted, res.Outcome)
	assert.Equal(t, RetryFeedback("the answer was cut at the token limit"), m.reqs[1].Turns[2].Text)
}

func TestLoopRun_MaxTriesExhausted(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("a", StopEnd), reply("b", StopEnd), reply("c", StopEnd), reply("ok", StopEnd)}}
	res, err := Loop{Model: m, MaxTries: 3, Check: okCheck}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeOutOfTries, res.Outcome)
	assert.Len(t, m.reqs, 3)
	require.Len(t, res.Tries, 3)
	last, ok := res.Last()
	require.True(t, ok)
	assert.Equal(t, "c", last.Reply.Text)
	// Turns grow by two per rejected try.
	assert.Len(t, m.reqs[2].Turns, 5)
}

func TestLoopRun_FinalVerdictStops(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("a", StopEnd), reply("ok", StopEnd)}}
	check := func(Try) Verdict { return Verdict{Reason: "unsafe", Final: true} }
	res, err := Loop{Model: m, MaxTries: 3, Check: check}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejected, res.Outcome)
	assert.Len(t, m.reqs, 1)
}

func TestLoopRun_RefusalStopsWithoutCheck(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("I won't", StopRefusal), reply("ok", StopEnd)}}
	checked := 0
	check := func(t Try) Verdict { checked++; return okCheck(t) }
	res, err := Loop{Model: m, MaxTries: 3, Check: check}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRefused, res.Outcome)
	assert.Zero(t, checked)
	require.Len(t, res.Tries, 1)
	assert.Equal(t, "I won't", res.Tries[0].Reply.Text)
}

func TestLoopRun_BusyErrorIsNotRetried(t *testing.T) {
	busy := &CallError{Class: Busy, Status: 429, Msg: "rate limited"}
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){fail(busy), reply("ok", StopEnd)}}
	b := &fakeBudget{allow: -1}
	res, err := Loop{Model: m, Budget: b, MaxTries: 3, Check: okCheck}.Run(context.Background(), firstReq)
	require.ErrorIs(t, err, busy)
	assert.Equal(t, OutcomeCallFailed, res.Outcome)
	assert.Len(t, m.reqs, 1)
	require.Len(t, res.Tries, 1)
	assert.ErrorIs(t, res.Tries[0].Err, busy)
	assert.Equal(t, FailureBusy, Classify(err).Kind)
	// The failed call's tokens are settled.
	assert.Equal(t, []int64{7}, b.settled)
}

func TestLoopRun_BudgetReservedAndSettledEachTry(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("a", StopEnd), reply("ok", StopEnd)}}
	b := &fakeBudget{allow: -1}
	res, err := Loop{Model: m, Budget: b, MaxTries: 3, Check: okCheck}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAccepted, res.Outcome)
	require.Len(t, m.reqs, 2)
	assert.Equal(t, []int64{Estimate(m.reqs[0]), Estimate(m.reqs[1])}, b.reserved)
	assert.Greater(t, b.reserved[1], b.reserved[0], "the second call carries the feedback turns")
	assert.Equal(t, []int64{15, 15}, b.settled)
	assert.Equal(t, b.reserved[1], b.settledFor[1].Tokens)
	assert.Equal(t, b.reserved[1], res.Estimate)
}

func TestLoopRun_BudgetSpentBeforeFirstCall(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("ok", StopEnd)}}
	b := &fakeBudget{allow: 0}
	res, err := Loop{Model: m, Budget: b, MaxTries: 3}.Run(context.Background(), firstReq)
	require.ErrorIs(t, err, ErrBudgetSpent)
	assert.Equal(t, OutcomeBudgetSpent, res.Outcome)
	assert.Empty(t, m.reqs)
	assert.Empty(t, res.Tries)
	assert.Equal(t, Estimate(firstReq), res.Estimate)
	assert.Empty(t, b.settled)
	assert.Equal(t, FailureBudgetSpent, Classify(err).Kind)
}

func TestLoopRun_BudgetSpentOnRetryKeepsTries(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("a", StopEnd), reply("ok", StopEnd)}}
	b := &fakeBudget{allow: 1}
	res, err := Loop{Model: m, Budget: b, MaxTries: 3, Check: okCheck}.Run(context.Background(), firstReq)
	require.ErrorIs(t, err, ErrBudgetSpent)
	assert.Equal(t, OutcomeBudgetSpent, res.Outcome)
	assert.Len(t, m.reqs, 1)
	assert.Len(t, res.Tries, 1)
	assert.Equal(t, []int64{15}, b.settled)
}

func TestLoopRun_BudgetUnavailable(t *testing.T) {
	store := errors.New("connection refused")
	m := &scriptModel{}
	res, err := Loop{Model: m, Budget: &fakeBudget{err: store}}.Run(context.Background(), firstReq)
	require.ErrorIs(t, err, ErrBudgetUnavailable)
	require.ErrorIs(t, err, store)
	var be *BudgetError
	require.ErrorAs(t, err, &be)
	assert.Equal(t, store, be.Err)
	assert.Equal(t, OutcomeBudgetUnavailable, res.Outcome)
	assert.Empty(t, m.reqs)
	assert.Equal(t, FailureBudgetUnavailable, Classify(err).Kind)
}

func TestLoopRun_SettlesOnPanicWithLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){func(context.Context, Request) (Reply, error) {
		cancel()
		panic("backend bug")
	}}}
	b := &fakeBudget{allow: -1}
	func() {
		defer func() { assert.Equal(t, "backend bug", recover()) }()
		_, _ = Loop{Model: m, Budget: b}.Run(ctx, firstReq)
	}()
	require.Len(t, b.settled, 1)
	assert.Equal(t, int64(0), b.settled[0])
	// The settle outlives the caller's cancel and has its own deadline.
	assert.NoError(t, b.settleErr[0])
	assert.True(t, b.settleDL[0])
}

// toolLog is a ToolRunner that answers "result of <name>" and fails the
// tool named "broken".
type toolLog struct{ ran []string }

func (r *toolLog) Run(_ context.Context, use ToolUse) (string, bool) {
	r.ran = append(r.ran, use.Name)
	if use.Name == "broken" {
		return "Error executing tool: boom", true
	}
	return "result of " + use.Name, false
}

func TestLoopRun_ToolRounds(t *testing.T) {
	lookup := ToolUse{ID: "t1", Name: "list_apps", Input: json.RawMessage(`{}`)}
	broken := ToolUse{ID: "t2", Name: "broken", Input: json.RawMessage(`{"x":1}`)}
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){
		toolReply("let me look", lookup, broken),
		reply("here you go", StopEnd),
	}}
	tools := &toolLog{}
	b := &fakeBudget{allow: -1}
	var events []EventKind
	req := firstReq
	req.Schema = nil
	req.Tools = []ToolDef{{Name: "list_apps"}, {Name: "broken"}}
	res, err := Loop{Model: m, Budget: b, MaxRounds: 6, Tools: tools, Progress: func(e Event) { events = append(events, e.Kind) }}.Run(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAccepted, res.Outcome)
	assert.Equal(t, []string{"list_apps", "broken"}, tools.ran)
	require.Len(t, m.reqs, 2)
	want := req
	want.Turns = []Turn{req.Turns[0],
		{Role: "assistant", Text: "let me look", Blocks: []Block{
			{Kind: BlockToolUse, ID: "t1", Name: "list_apps", Input: json.RawMessage(`{}`)},
			{Kind: BlockToolUse, ID: "t2", Name: "broken", Input: json.RawMessage(`{"x":1}`)},
		}},
		{Role: "user", Blocks: []Block{
			{Kind: BlockToolResult, ID: "t1", Content: "result of list_apps"},
			{Kind: BlockToolResult, ID: "t2", Content: "Error executing tool: boom", IsError: true},
		}},
	}
	assert.Equal(t, want, m.reqs[1])
	require.Len(t, res.Tries, 2)
	assert.True(t, res.Tries[0].ToolRound)
	assert.Equal(t, 1, res.Tries[1].Try, "a tool round is not a try")
	assert.Equal(t, 2, res.Tries[1].Call)
	assert.Len(t, b.reserved, 2)
	assert.Equal(t, []EventKind{EventCallStarted, EventAnswered, EventToolStarted, EventToolStarted, EventCallStarted, EventAnswered, EventChecked}, events)
}

func TestLoopRun_ToolUsesWithoutRunnerGoToCheck(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){toolReply("x", ToolUse{ID: "t1", Name: "a"})}}
	var seen []ToolUse
	check := func(t Try) Verdict { seen = t.Reply.ToolUses; return Verdict{Accept: true} }
	res, err := Loop{Model: m, Check: check}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAccepted, res.Outcome)
	assert.Len(t, seen, 1)
}

func TestLoopRun_RoundCap(t *testing.T) {
	use := ToolUse{ID: "t", Name: "list_apps"}
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){
		toolReply("", use), toolReply("", use), toolReply("", use), reply("never", StopEnd),
	}}
	tools := &toolLog{}
	res, err := Loop{Model: m, MaxRounds: 3, Tools: tools}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRoundCap, res.Outcome)
	assert.Len(t, m.reqs, 3)
	// The last round's tools run too, as the chat assistant's loop did.
	assert.Len(t, tools.ran, 3)
}

func TestLoopRun_RoundCapAlsoBoundsRetries(t *testing.T) {
	use := ToolUse{ID: "t", Name: "list_apps"}
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){toolReply("", use), reply("bad", StopEnd), reply("ok", StopEnd)}}
	res, err := Loop{Model: m, MaxTries: 3, MaxRounds: 2, Tools: &toolLog{}, Check: okCheck}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRoundCap, res.Outcome)
	assert.Len(t, m.reqs, 2)
}

func TestLoopRun_PartialAnswerWhenBudgetRunsOutMidTurn(t *testing.T) {
	use := ToolUse{ID: "t", Name: "list_apps"}
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){toolReply("looking", use), reply("answer", StopEnd)}}
	tools := &toolLog{}
	res, err := Loop{Model: m, Budget: &fakeBudget{allow: 1}, MaxRounds: 6, Tools: tools}.Run(context.Background(), firstReq)
	require.ErrorIs(t, err, ErrBudgetSpent)
	assert.Equal(t, OutcomeBudgetSpent, res.Outcome)
	// What the caller answers with: the lookups done so far.
	require.Len(t, res.Tries, 1)
	assert.True(t, res.Tries[0].ToolRound)
	assert.Equal(t, []string{"list_apps"}, tools.ran)
	assert.Len(t, m.reqs, 1)
}

func TestLoopRun_ContextCanceled(t *testing.T) {
	t.Run("before the first call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		m := &scriptModel{}
		b := &fakeBudget{allow: -1}
		res, err := Loop{Model: m, Budget: b}.Run(ctx, firstReq)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, OutcomeCanceled, res.Outcome)
		assert.Empty(t, m.reqs)
		assert.Empty(t, b.reserved)
		assert.Equal(t, FailureCanceled, Classify(err).Kind)
	})
	t.Run("during a call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){func(context.Context, Request) (Reply, error) {
			cancel()
			return Reply{InputTokens: 3}, &CallError{Class: Busy, Msg: "context canceled"}
		}}}
		b := &fakeBudget{allow: -1}
		res, err := Loop{Model: m, Budget: b, MaxTries: 3}.Run(ctx, firstReq)
		require.Error(t, err)
		assert.Equal(t, OutcomeCanceled, res.Outcome)
		assert.Equal(t, []int64{3}, b.settled)
		assert.Len(t, res.Tries, 1)
	})
	t.Run("between tool round and next call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){toolReply("", ToolUse{ID: "t", Name: "a"}), reply("ok", StopEnd)}}
		tools := ToolFunc(func(context.Context, ToolUse) (string, bool) { cancel(); return "r", false })
		res, err := Loop{Model: m, MaxRounds: 6, Tools: tools}.Run(ctx, firstReq)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, OutcomeCanceled, res.Outcome)
		assert.Len(t, m.reqs, 1)
	})
}

func TestLoopRun_ProgressOnRetry(t *testing.T) {
	m := &scriptModel{steps: []func(context.Context, Request) (Reply, error){reply("a", StopEnd), reply("ok", StopEnd)}}
	type step struct {
		kind           EventKind
		call, try, max int
	}
	var got []step
	progress := func(e Event) { got = append(got, step{e.Kind, e.Call, e.Try, e.MaxTries}) }
	_, err := Loop{Model: m, MaxTries: 3, Check: okCheck, Progress: progress}.Run(context.Background(), firstReq)
	require.NoError(t, err)
	assert.Equal(t, []step{
		{EventCallStarted, 1, 1, 3}, {EventAnswered, 1, 1, 3}, {EventChecked, 1, 1, 3}, {EventRetrying, 1, 2, 3},
		{EventCallStarted, 2, 2, 3}, {EventAnswered, 2, 2, 3}, {EventChecked, 2, 2, 3},
	}, got)
}
