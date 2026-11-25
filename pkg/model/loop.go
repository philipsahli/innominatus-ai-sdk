package model

import (
	"context"
	"errors"
	"time"
)

// Loop runs one task against a model: one or more calls until an answer is
// accepted, the tries run out, the budget is spent or a call fails.
//
// Loop is the one place for budget reserve and settle, retry feedback, the
// handling of a refusal or a cut answer, and tool turns, so features that
// call a model do not each grow their own. Instrument stays the per-call
// chokepoint
// (allowlist, redaction, audit); Loop sits above it and owns what spans
// calls:
//
//   - the budget: Estimate, reserve before each call, settle after it
//     (also after a failed call or a panic, with a context that outlives
//     the caller's);
//   - tries: Check decides on each answer; a rejected one is sent back as
//     the model's turn plus a feedback turn, up to MaxTries;
//   - tool rounds: an answer that asks for tools gets them run by Tools
//     and sent back as tool_use and tool_result blocks, up to MaxRounds
//     calls in all.
//
// Loop never retries a failed call: a busy or rate-limited model API ends
// the run with the call's error: a person may be waiting. Long backoff
// belongs in a job queue that keeps its tries in storage and makes one
// call per claim; it can still use RetryFeedback and Estimate.
//
// The requests Loop sends are fully determined by the first Request and
// the caller's Feedback and EmptyAnswer, so a caller that moves onto Loop
// can keep sending the same bytes it sent before (replay keys and request
// hashes depend on them).
//
// Model should be wrapped by Instrument, so every call passes the
// allowlist, redaction and the audit trail; Loop does not add them.
type Loop struct {
	Model Model
	// Budget is reserved before and settled after each call; nil spends
	// nothing (for a caller that reserves elsewhere).
	Budget Budget
	// MaxTries is how many answers Check may see; at least 1.
	MaxTries int
	// MaxRounds caps the model calls of one Run, tool rounds included.
	// 0 means MaxTries. A caller with Tools sets it.
	MaxRounds int
	// Tools runs the tool calls an answer asks for. Nil: an answer with
	// tool calls goes to Check like any other.
	Tools ToolRunner
	// Check decides on an answer (not a tool round, not a refusal). Nil
	// accepts every answer.
	Check func(Try) Verdict
	// Feedback is the user turn sent after a rejected try. Nil sends
	// RetryFeedback(t.Reason).
	Feedback func(Try) string
	// EmptyAnswer replaces an empty answer text in the assistant turn of a
	// retry; "" sends the text as it came. The Messages API refuses an
	// empty text block, so most callers set EmptyAnswerText.
	EmptyAnswer string
	// Progress gets an event at each step; nil reports nothing.
	Progress func(Event)
}

// ToolRunner runs one tool call for Loop. A failed tool is a result for
// the model, not an error of the run: Run returns the text the model sees
// and isError.
type ToolRunner interface {
	Run(ctx context.Context, use ToolUse) (content string, isError bool)
}

// ToolFunc is a function as a ToolRunner.
type ToolFunc func(ctx context.Context, use ToolUse) (string, bool)

// Run implements ToolRunner.
func (f ToolFunc) Run(ctx context.Context, use ToolUse) (string, bool) { return f(ctx, use) }

// Verdict is Check's decision on one answer.
type Verdict struct {
	Accept bool
	// Reason says why the answer was not accepted; Feedback gets it on the
	// Try.
	Reason string
	// Final ends the run on a rejected answer without another try.
	Final bool
}

// Try is one model call of a Run.
type Try struct {
	// Call counts the calls of the run from 1; Try counts the answers
	// Check saw, from 1, and is the try a tool round belongs to.
	Call, Try int
	// Request is what was sent.
	Request  Request
	Reply    Reply
	Duration time.Duration
	// ToolRound: the answer asked for tools and Tools ran them.
	ToolRound bool
	// Accepted and Reason are Check's verdict.
	Accepted bool
	Reason   string
	// Err is the call's error; the run ended with it.
	Err error
}

// Outcome is how a Run ended.
type Outcome int

const (
	// OutcomeAccepted: Check accepted the last answer.
	OutcomeAccepted Outcome = iota
	// OutcomeRefused: the model declined (StopRefusal); Check did not see it.
	OutcomeRefused
	// OutcomeOutOfTries: MaxTries answers were rejected.
	OutcomeOutOfTries
	// OutcomeRejected: Check rejected the last answer as final.
	OutcomeRejected
	// OutcomeRoundCap: MaxRounds calls were made without an accepted answer.
	OutcomeRoundCap
	// OutcomeBudgetSpent: the budget refused the next call (ErrBudgetSpent).
	// With tries, the caller may answer with what it has.
	OutcomeBudgetSpent
	// OutcomeBudgetUnavailable: the budget could not be read (*BudgetError).
	OutcomeBudgetUnavailable
	// OutcomeCallFailed: a call failed; the run returns its error.
	OutcomeCallFailed
	// OutcomeCanceled: ctx ended before or during a call.
	OutcomeCanceled
)

func (o Outcome) String() string {
	switch o {
	case OutcomeAccepted:
		return "accepted"
	case OutcomeRefused:
		return "refused"
	case OutcomeOutOfTries:
		return "out_of_tries"
	case OutcomeRejected:
		return "rejected"
	case OutcomeRoundCap:
		return "round_cap"
	case OutcomeBudgetSpent:
		return "budget_spent"
	case OutcomeBudgetUnavailable:
		return "budget_unavailable"
	case OutcomeCallFailed:
		return "call_failed"
	}
	return "canceled"
}

// Result is what a Run did.
type Result struct {
	Outcome Outcome
	// Tries are the calls made, in order; the last one holds the answer.
	Tries                     []Try
	InputTokens, OutputTokens int64
	// Estimate is the estimate of the last call reserved or refused: with
	// OutcomeBudgetSpent and no tries, a caller can tell "one call is
	// larger than the daily budget" from "today's budget is spent".
	Estimate int64
}

// Last returns the last try; ok is false when no call was made.
func (r Result) Last() (t Try, ok bool) {
	if len(r.Tries) == 0 {
		return Try{}, false
	}
	return r.Tries[len(r.Tries)-1], true
}

// ErrBudgetSpent is the error of a Run whose next call did not fit in
// today's budget.
var ErrBudgetSpent = errors.New("the token budget for today is spent")

// ErrBudgetUnavailable is matched by a *BudgetError.
var ErrBudgetUnavailable = errors.New("the token budget cannot be read")

// BudgetError is the error of a Run whose budget reserve failed; Err is
// the store's error.
type BudgetError struct{ Err error }

func (e *BudgetError) Error() string { return ErrBudgetUnavailable.Error() + ": " + e.Err.Error() }

// Unwrap matches ErrBudgetUnavailable and the store's error.
func (e *BudgetError) Unwrap() []error { return []error{ErrBudgetUnavailable, e.Err} }

// EmptyAnswerText is the usual Loop.EmptyAnswer.
const EmptyAnswerText = "(empty answer)"

// RetryFeedback is the feedback turn sent after a rejected answer: Loop's
// default, exported for job queues that retry outside Loop.
func RetryFeedback(reason string) string {
	return "That answer was not accepted: " + reason + ". Answer again with one JSON object that matches the schema."
}

// EventKind is the kind of a progress Event.
type EventKind int

const (
	// EventCallStarted: a call is about to be made (after its reserve).
	EventCallStarted EventKind = iota
	// EventAnswered: the call returned an answer; Record is the try.
	EventAnswered
	// EventToolStarted: Tools is about to run Tool.
	EventToolStarted
	// EventChecked: Check decided; Record holds the verdict.
	EventChecked
	// EventRetrying: a rejected answer goes back with feedback; Try is
	// the next try.
	EventRetrying
)

// Event is one step of a Run for Loop.Progress.
type Event struct {
	Kind      EventKind
	Call, Try int
	MaxTries  int
	Tool      ToolUse
	Record    *Try
}

// settleTimeout bounds the settle after a call. It runs on a context the
// caller's cancel does not reach: a canceled run must still give its hold
// back.
const settleTimeout = 10 * time.Second

// Run sends req and continues the conversation until the run ends; see
// Outcome. It returns an error for OutcomeBudgetSpent (ErrBudgetSpent),
// OutcomeBudgetUnavailable (*BudgetError), OutcomeCallFailed (the call's
// error, a *CallError from an instrumented model) and OutcomeCanceled
// (ctx.Err() before a call, else the call's error). The Result holds the
// tries made either way.
func (l Loop) Run(ctx context.Context, req Request) (Result, error) {
	maxTries := max(l.MaxTries, 1)
	maxRounds := l.MaxRounds
	if maxRounds <= 0 {
		maxRounds = maxTries
	}
	var res Result
	turns := append([]Turn(nil), req.Turns...)
	tries := 0
	for call := 1; ; call++ {
		if err := ctx.Err(); err != nil {
			res.Outcome = OutcomeCanceled
			return res, err
		}
		r := req
		// Clipped, so the turns appended later never show up in a Try's
		// Request.
		r.Turns = turns[:len(turns):len(turns)]

		hold, err := l.reserve(ctx, r, &res)
		if err != nil {
			return res, err
		}
		l.report(Event{Kind: EventCallStarted, Call: call, Try: tries + 1, MaxTries: maxTries})
		rep, took, err := l.call(ctx, r, hold)
		res.InputTokens += rep.InputTokens
		res.OutputTokens += rep.OutputTokens
		t := Try{Call: call, Try: tries + 1, Request: r, Reply: rep, Duration: took}
		if err != nil {
			t.Err = err
			res.Tries = append(res.Tries, t)
			res.Outcome = OutcomeCallFailed
			if ctx.Err() != nil {
				res.Outcome = OutcomeCanceled
			}
			return res, err
		}

		if len(rep.ToolUses) > 0 && l.Tools != nil {
			t.ToolRound = true
			res.Tries = append(res.Tries, t)
			l.report(Event{Kind: EventAnswered, Call: call, Try: tries + 1, MaxTries: maxTries, Record: &res.Tries[len(res.Tries)-1]})
			turns = append(turns, l.runTools(ctx, call, tries+1, maxTries, rep)...)
			// The tools of the last round run too, although no call reads
			// their results: tools are expected to only read, and a caller
			// may show what they found.
			if call >= maxRounds {
				res.Outcome = OutcomeRoundCap
				return res, nil
			}
			continue
		}

		l.report(Event{Kind: EventAnswered, Call: call, Try: tries + 1, MaxTries: maxTries, Record: &t})
		if rep.Stop == StopRefusal {
			res.Tries = append(res.Tries, t)
			res.Outcome = OutcomeRefused
			return res, nil
		}
		v := Verdict{Accept: true}
		if l.Check != nil {
			v = l.Check(t)
		}
		t.Accepted, t.Reason = v.Accept, v.Reason
		tries++
		res.Tries = append(res.Tries, t)
		l.report(Event{Kind: EventChecked, Call: call, Try: tries, MaxTries: maxTries, Record: &res.Tries[len(res.Tries)-1]})
		switch {
		case v.Accept:
			res.Outcome = OutcomeAccepted
			return res, nil
		case v.Final:
			res.Outcome = OutcomeRejected
			return res, nil
		case tries >= maxTries:
			res.Outcome = OutcomeOutOfTries
			return res, nil
		case call >= maxRounds:
			res.Outcome = OutcomeRoundCap
			return res, nil
		}
		l.report(Event{Kind: EventRetrying, Call: call, Try: tries + 1, MaxTries: maxTries, Record: &res.Tries[len(res.Tries)-1]})
		turns = append(turns, l.retryTurns(t)...)
	}
}

// reserve holds r's estimate; a refusal or a failed read ends the run.
func (l Loop) reserve(ctx context.Context, r Request, res *Result) (Hold, error) {
	if l.Budget == nil {
		return Hold{}, nil
	}
	res.Estimate = Estimate(r)
	hold, ok, err := l.Budget.Reserve(ctx, res.Estimate)
	if err != nil {
		res.Outcome = OutcomeBudgetUnavailable
		return Hold{}, &BudgetError{Err: err}
	}
	if !ok {
		res.Outcome = OutcomeBudgetSpent
		return Hold{}, ErrBudgetSpent
	}
	return hold, nil
}

// call makes one call and settles its hold, also when the call fails or
// panics.
func (l Loop) call(ctx context.Context, r Request, hold Hold) (rep Reply, took time.Duration, err error) {
	start := time.Now()
	if l.Budget != nil {
		defer func() {
			settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
			defer cancel()
			l.Budget.Settle(settleCtx, hold, rep.InputTokens+rep.OutputTokens)
		}()
	}
	rep, err = l.Model.Call(ctx, r)
	return rep, time.Since(start), err
}

// runTools returns the assistant turn that asked for the tools (its text,
// then one tool_use block per call) and the user turn with their results.
func (l Loop) runTools(ctx context.Context, call, try, maxTries int, rep Reply) []Turn {
	asked := Turn{Role: "assistant", Text: rep.Text}
	results := Turn{Role: "user"}
	for _, use := range rep.ToolUses {
		asked.Blocks = append(asked.Blocks, Block{Kind: BlockToolUse, ID: use.ID, Name: use.Name, Input: use.Input})
	}
	for _, use := range rep.ToolUses {
		l.report(Event{Kind: EventToolStarted, Call: call, Try: try, MaxTries: maxTries, Tool: use})
		content, isErr := l.Tools.Run(ctx, use)
		results.Blocks = append(results.Blocks, Block{Kind: BlockToolResult, ID: use.ID, Content: content, IsError: isErr})
	}
	return []Turn{asked, results}
}

// retryTurns are the model's rejected answer and the feedback on it.
func (l Loop) retryTurns(t Try) []Turn {
	text := t.Reply.Text
	if text == "" && l.EmptyAnswer != "" {
		text = l.EmptyAnswer
	}
	fb := RetryFeedback(t.Reason)
	if l.Feedback != nil {
		fb = l.Feedback(t)
	}
	return []Turn{{Role: "assistant", Text: text}, {Role: "user", Text: fb}}
}

func (l Loop) report(e Event) {
	if l.Progress != nil {
		l.Progress(e)
	}
}
