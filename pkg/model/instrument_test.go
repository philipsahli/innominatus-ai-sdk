package model

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
)

type stubModel struct {
	rep Reply
	err error
}

func (s stubModel) Call(context.Context, Request) (Reply, error) { return s.rep, s.err }
func (s stubModel) Name() (string, string)                       { return "stub", "stub-1" }

// countingMetrics keeps the counts Instrument reports, under the series
// names a Prometheus export would show, so tests can name a sample.
type countingMetrics struct {
	mu sync.Mutex
	n  map[string]int64
}

func (c *countingMetrics) add(key string, v int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[key] += v
}

func (c *countingMetrics) RecordAIModelCall(component, provider, outcome string, in, out int64) {
	c.add(fmt.Sprintf(`innominatus_ai_model_calls_total{component=%q,provider=%q,outcome=%q}`, component, provider, outcome), 1)
	if in > 0 {
		c.add(fmt.Sprintf(`innominatus_ai_model_tokens_total{component=%q,provider=%q,direction="input"}`, component, provider), in)
	}
	if out > 0 {
		c.add(fmt.Sprintf(`innominatus_ai_model_tokens_total{component=%q,provider=%q,direction="output"}`, component, provider), out)
	}
}

func (c *countingMetrics) RecordAIPromptRedactions(component string, n int) {
	if n > 0 {
		c.add(fmt.Sprintf(`innominatus_ai_prompt_redactions_total{component=%q}`, component), int64(n))
	}
}

var testMetrics = &countingMetrics{n: map[string]int64{}}

func init() { defaultMetrics = testMetrics }

// series reads one sample of testMetrics; 0 when absent. The tests compare
// before and after, because the counters keep their values across tests
// and -count runs.
func series(t *testing.T, sample string) int64 {
	t.Helper()
	testMetrics.mu.Lock()
	defer testMetrics.mu.Unlock()
	return testMetrics.n[sample]
}

const (
	okCalls     = `innominatus_ai_model_calls_total{component="instrument_test",provider="stub",outcome="ok"}`
	inTokens    = `innominatus_ai_model_tokens_total{component="instrument_test",provider="stub",direction="input"}`
	canceledSeq = `innominatus_ai_model_calls_total{component="instrument_test",provider="stub",outcome="canceled"}`
	busyCalls   = `innominatus_ai_model_calls_total{component="instrument_test",provider="stub",outcome="busy"}`
)

func TestInstrument_PassesThroughAndCounts(t *testing.T) {
	calls0, tokens0 := series(t, okCalls), series(t, inTokens)
	want := Reply{Text: "{}", InputTokens: 7, OutputTokens: 3}
	m := Instrument(stubModel{rep: want}, "instrument_test")

	got, err := m.Call(context.Background(), Request{MaxTokens: 10})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Call() = %+v, %v; want %+v, nil", got, err, want)
	}
	if p, mod := m.Name(); p != "stub" || mod != "stub-1" {
		t.Errorf("Name() = %s, %s", p, mod)
	}
	if d := series(t, okCalls) - calls0; d != 1 {
		t.Errorf("ok calls grew by %d, want 1", d)
	}
	if d := series(t, inTokens) - tokens0; d != 7 {
		t.Errorf("input tokens grew by %d, want 7", d)
	}
}

func TestInstrument_CanceledIsNotBusy(t *testing.T) {
	canceled0, busy0 := series(t, canceledSeq), series(t, busyCalls)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := Instrument(stubModel{err: ctx.Err()}, "instrument_test")

	if _, err := m.Call(ctx, Request{}); err == nil {
		t.Fatal("want the inner error")
	}
	if d := series(t, canceledSeq) - canceled0; d != 1 {
		t.Errorf("canceled calls grew by %d, want 1", d)
	}
	if d := series(t, busyCalls) - busy0; d != 0 {
		t.Errorf("busy calls grew by %d, want 0", d)
	}
}

func TestInstrument_DeadlineIsBusy(t *testing.T) {
	busy0 := series(t, busyCalls)
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-ctx.Done()
	m := Instrument(stubModel{err: ctx.Err()}, "instrument_test")

	_, _ = m.Call(ctx, Request{})
	if d := series(t, busyCalls) - busy0; d != 1 {
		t.Errorf("busy calls grew by %d, want 1", d)
	}
}

func TestOutcomeOf(t *testing.T) {
	tests := []struct {
		name string
		rep  Reply
		err  error
		want string
	}{
		{"ok", Reply{}, nil, "ok"},
		{"cut", Reply{Stop: StopMaxTokens}, nil, "max_tokens"},
		{"refusal", Reply{Stop: StopRefusal}, nil, "refusal"},
		{"busy", Reply{}, &CallError{Class: Busy}, "busy"},
		{"refused", Reply{}, &CallError{Class: Refused}, "refused"},
		{"not found", Reply{}, &CallError{Class: NotFound}, "not_found"},
		{"bad", Reply{}, &CallError{Class: Bad}, "bad"},
		{"plain error is busy", Reply{}, context.DeadlineExceeded, "busy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outcomeOf(tt.rep, tt.err); got != tt.want {
				t.Errorf("outcomeOf() = %q, want %q", got, tt.want)
			}
		})
	}
}
