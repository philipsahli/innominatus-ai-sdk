package model

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fullEntry is an entry whose contract is complete and acceptable.
func fullEntry() ProviderEntry {
	yes := true
	return ProviderEntry{
		Name: ProviderAnthropic, Endpoint: "https://api.anthropic.com", Models: []string{"claude-sonnet-5-5"},
		Region: "eu", FailoverRegion: "eu", DataRetention: "none", Training: "no",
		DPA: DPA{Signed: &yes, Reference: "DPA-2026-01"},
	}
}

func TestParseProviderAllowlist(t *testing.T) {
	valid := `ch_only: true
providers:
  - name: anthropic
    endpoint: https://api.anthropic.com/
    models: ["*"]
    region: CH
    failover_region: none
    data_retention: none
    training: "no"
    dpa: {signed: true, reference: DPA-1}
`
	tests := []struct {
		name    string
		body    string
		wantErr string
		check   func(t *testing.T, a ProviderAllowlist)
	}{
		{name: "empty is an empty list", check: func(t *testing.T, a ProviderAllowlist) {
			if a.File != "" || len(a.Providers) != 0 || a.CHOnly {
				t.Errorf("got %+v, want empty", a)
			}
		}},
		{name: "valid file", body: valid, check: func(t *testing.T, a ProviderAllowlist) {
			if !a.CHOnly || len(a.Providers) != 1 {
				t.Fatalf("got %+v", a)
			}
			e := a.Providers[0]
			if e.Region != "ch" || e.FailoverRegion != "none" || e.Training != "no" {
				t.Errorf("not normalised: %+v", e)
			}
		}},
		{name: "unquoted no is the string no", body: "providers:\n  - name: ollama\n    training: no\n", check: func(t *testing.T, a ProviderAllowlist) {
			if a.Providers[0].Training != "no" {
				t.Errorf("training = %q", a.Providers[0].Training)
			}
		}},
		{name: "unknown field", body: "providers:\n  - name: anthropic\n    regoin: eu\n", wantErr: "regoin"},
		{name: "unknown provider", body: "providers:\n  - name: mistral\n", wantErr: `"mistral"`},
		{name: "empty name", body: "providers:\n  - region: eu\n", wantErr: "name"},
		{name: "endpoint not a URL", body: "providers:\n  - name: openai\n    endpoint: ftp://x\n", wantErr: "endpoint"},
		{name: "duplicate", body: "providers:\n  - name: openai\n    endpoint: https://a.example\n  - name: openai\n    endpoint: https://a.example/\n", wantErr: "twice"},
		{name: "not yaml", body: "providers: [", wantErr: "yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseProviderAllowlist([]byte(tt.body))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, a)
		})
	}
}

func TestProviderAllowlist_Decide(t *testing.T) {
	no := false
	type want struct {
		allowed, checked bool
		reason           string
	}
	tests := []struct {
		name   string
		list   func() ProviderAllowlist
		model  string
		local  bool
		expect want
	}{
		{"complete eu entry", func() ProviderAllowlist { return ProviderAllowlist{Providers: []ProviderEntry{fullEntry()}} },
			"claude-sonnet-5-5", false, want{true, true, ""}},
		{"unlisted on a shared server with a file", func() ProviderAllowlist { return ProviderAllowlist{File: "/etc/ai.yaml"} },
			"claude-sonnet-5-5", false, want{false, false, "allowlist file /etc/ai.yaml"}},
		{"no allowlist on a shared server", func() ProviderAllowlist { return ProviderAllowlist{} },
			"claude-sonnet-5-5", false, want{false, false, "the provider allowlist setting is not set"}},
		{"unlisted in local mode", func() ProviderAllowlist { return ProviderAllowlist{} },
			"claude-sonnet-5-5", true, want{true, false, "contract not checked"}},
		{"other model", func() ProviderAllowlist { return ProviderAllowlist{Providers: []ProviderEntry{fullEntry()}} },
			"claude-opus-5-5", false, want{false, false, "not on the provider allowlist"}},
		{"any model", func() ProviderAllowlist {
			e := fullEntry()
			e.Models = []string{"*"}
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-opus-5-5", false, want{true, true, ""}},
		{"incomplete on a shared server", func() ProviderAllowlist {
			e := fullEntry()
			e.Region, e.DPA.Reference = "", ""
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", false, want{false, false, "missing region, dpa.reference"}},
		{"incomplete in local mode", func() ProviderAllowlist {
			e := fullEntry()
			e.Training = ""
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", true, want{true, false, "contract not checked"}},
		{"no models listed is incomplete", func() ProviderAllowlist {
			e := fullEntry()
			e.Models = nil
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", false, want{false, false, "missing models"}},
		{"us region", func() ProviderAllowlist {
			e := fullEntry()
			e.Region = "us"
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", true, want{false, true, `region "us"`}},
		{"us failover", func() ProviderAllowlist {
			e := fullEntry()
			e.FailoverRegion = "us"
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", false, want{false, true, `failover_region "us"`}},
		{"ch only refuses eu", func() ProviderAllowlist {
			return ProviderAllowlist{CHOnly: true, Providers: []ProviderEntry{fullEntry()}}
		}, "claude-sonnet-5-5", false, want{false, true, "CH only"}},
		{"ch only allows ch with no failover", func() ProviderAllowlist {
			e := fullEntry()
			e.Region, e.FailoverRegion = "ch", "none"
			return ProviderAllowlist{CHOnly: true, Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", false, want{true, true, ""}},
		{"data kept", func() ProviderAllowlist {
			e := fullEntry()
			e.DataRetention = "30d"
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", true, want{false, true, "data_retention"}},
		{"trains on data", func() ProviderAllowlist {
			e := fullEntry()
			e.Training = "yes"
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", false, want{false, true, "training"}},
		{"no dpa", func() ProviderAllowlist {
			e := fullEntry()
			e.DPA.Signed = &no
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}, "claude-sonnet-5-5", false, want{false, true, "dpa"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.list().Decide(ProviderAnthropic, "https://api.anthropic.com/", tt.model, tt.local)
			if d.Allowed != tt.expect.allowed || d.ContractChecked != tt.expect.checked {
				t.Fatalf("Decide() = %+v, want allowed=%t checked=%t", d, tt.expect.allowed, tt.expect.checked)
			}
			if tt.expect.reason != "" && !strings.Contains(d.Reason, tt.expect.reason) {
				t.Errorf("reason %q does not mention %q", d.Reason, tt.expect.reason)
			}
			if tt.expect.allowed && tt.expect.checked && d.Reason != "" {
				t.Errorf("reason %q for a checked provider, want none", d.Reason)
			}
			if d.Name != ProviderAnthropic {
				t.Errorf("name = %q", d.Name)
			}
		})
	}
}

// The reason names the host, never the full endpoint, which can carry a
// password.
func TestProviderAllowlist_DecideHidesEndpointCredentials(t *testing.T) {
	d := ProviderAllowlist{}.Decide(ProviderOllama, "http://user:secret@gpu.example:11434", "llama", false)
	if strings.Contains(d.Reason, "secret") {
		t.Fatalf("reason leaks the password: %q", d.Reason)
	}
	if !strings.Contains(d.Reason, "gpu.example") {
		t.Errorf("reason %q does not name the host", d.Reason)
	}
}

// recordedAudit keeps the events the guard reports.
type recordedAudit struct {
	mu     sync.Mutex
	events []ProviderEvent
}

func (a *recordedAudit) report(_ context.Context, e ProviderEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
}

// recordingModel records whether a prompt reached it.
type recordingModel struct {
	called   int
	provider string
}

func (r *recordingModel) Call(context.Context, Request) (Reply, error) {
	r.called++
	return Reply{Text: "{}"}, nil
}
func (r *recordingModel) Name() (string, string) { return r.provider, "claude-sonnet-5-5" }

// The done-criterion of the allowlist: a call to a provider that is not
// allowed is refused before the backend sees the prompt, it is audited and
// counted, and the error is final for the worker.
func TestGuard_RefusedCallNeverReachesProvider(t *testing.T) {
	cases := []struct {
		name string
		list ProviderAllowlist
	}{
		{"unlisted", ProviderAllowlist{File: "/etc/ai.yaml"}},
		{"incomplete", func() ProviderAllowlist {
			e := fullEntry()
			e.DataRetention = ""
			return ProviderAllowlist{Providers: []ProviderEntry{e}}
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &recordingModel{provider: ProviderAnthropic}
			a := &recordedAudit{}
			refused0 := series(t, `innominatus_ai_model_calls_total{component="guard_test",provider="anthropic",outcome="provider_refused"}`)
			m := Instrument(Guard(fake, tc.list, "https://api.anthropic.com", false, a.report), "guard_test")

			_, err := m.Call(context.Background(), Request{System: "secret plan", Turns: []Turn{{Role: "user", Text: "hello"}}})
			if !errors.Is(err, ErrProviderNotAllowed) {
				t.Fatalf("err = %v, want ErrProviderNotAllowed", err)
			}
			if fake.called != 0 {
				t.Fatalf("the provider was called %d times", fake.called)
			}
			if c := AsCallError(err).Class; c != Bad {
				t.Errorf("class = %s, want bad (final, never retried)", c)
			}
			if d := series(t, `innominatus_ai_model_calls_total{component="guard_test",provider="anthropic",outcome="provider_refused"}`) - refused0; d != 1 {
				t.Errorf("provider_refused grew by %d, want 1", d)
			}
			if len(a.events) != 1 {
				t.Fatalf("audit events = %d, want 1", len(a.events))
			}
			e := a.events[0]
			if e.Kind != ProviderRefused {
				t.Errorf("event = %+v", e)
			}
			if e.Component != "guard_test" || e.Decision.Name != ProviderAnthropic || e.Decision.ContractChecked {
				t.Errorf("event = %+v", e)
			}
			for _, s := range []string{e.Model, e.Decision.Reason, e.Decision.Region} {
				if strings.Contains(s, "secret plan") || strings.Contains(s, "hello") {
					t.Errorf("event holds prompt text: %+v", e)
				}
			}
		})
	}
}

// Local mode calls a provider whose contract is not checked, and
// the audit says so.
func TestGuard_LocalUncheckedCallIsAuditedAndMade(t *testing.T) {
	fake := &recordingModel{provider: ProviderAnthropic}
	a := &recordedAudit{}
	m := Instrument(Guard(fake, ProviderAllowlist{}, "https://api.anthropic.com", true, a.report), "guard_test")
	for i := 0; i < 3; i++ {
		if _, err := m.Call(context.Background(), Request{}); err != nil {
			t.Fatal(err)
		}
	}
	if fake.called != 3 {
		t.Fatalf("called = %d, want 3", fake.called)
	}
	// Once per start (per guarded backend), not per call: the provider
	// and its unchecked contract do not change while the server runs.
	if len(a.events) != 1 || a.events[0].Kind != ProviderUncheckedUsed {
		t.Fatalf("events = %+v", a.events)
	}
	if r := a.events[0].Decision.Reason; !strings.Contains(r, "contract not checked") {
		t.Errorf("reason = %q", r)
	}
}

// A shared server without an allowlist file refuses every call before
// the model sees it, and writes ai.provider.refused, not unchecked_used.
func TestGuard_SharedServerWithoutAllowlistRefuses(t *testing.T) {
	fake := &recordingModel{provider: ProviderAnthropic}
	a := &recordedAudit{}
	m := Instrument(Guard(fake, ProviderAllowlist{}, "https://api.anthropic.com", false, a.report), "guard_test")
	_, err := m.Call(context.Background(), Request{})
	if !errors.Is(err, ErrProviderNotAllowed) || fake.called != 0 {
		t.Fatalf("err=%v called=%d", err, fake.called)
	}
	if !strings.Contains(err.Error(), "the provider allowlist setting is not set") {
		t.Errorf("error %q does not say the allowlist is not set", err)
	}
	if len(a.events) != 1 || a.events[0].Kind != ProviderRefused || a.events[0].Decision.Allowed {
		t.Errorf("events = %+v", a.events)
	}
}

// A shared server with an allowlist file refuses a provider whose terms
// fail, and the call error names the file.
func TestGuard_ConfiguredAllowlistRefusesWithFileInError(t *testing.T) {
	e := fullEntry()
	e.Region = "us"
	fake := &recordingModel{provider: ProviderAnthropic}
	a := &recordedAudit{}
	m := Guard(fake, ProviderAllowlist{File: "/etc/ai.yaml", Providers: []ProviderEntry{e}}, "https://api.anthropic.com", false, a.report)
	_, err := m.Call(context.Background(), Request{})
	if !errors.Is(err, ErrProviderNotAllowed) || fake.called != 0 {
		t.Fatalf("err=%v called=%d", err, fake.called)
	}
	if !strings.Contains(err.Error(), "allowlist file /etc/ai.yaml") || !strings.Contains(err.Error(), `region "us"`) {
		t.Errorf("error %q does not name the allowlist and the failed term", err)
	}
	if len(a.events) != 1 || a.events[0].Kind != ProviderRefused {
		t.Errorf("events = %+v", a.events)
	}
}

// A checked provider passes through without an extra audit record: the
// caller's own record covers the call.
func TestGuard_CheckedProviderPassesThrough(t *testing.T) {
	fake := &recordingModel{provider: ProviderAnthropic}
	a := &recordedAudit{}
	m := Guard(fake, ProviderAllowlist{Providers: []ProviderEntry{fullEntry()}}, "https://api.anthropic.com", false, a.report)
	if _, err := m.Call(context.Background(), Request{}); err != nil || fake.called != 1 || len(a.events) != 0 {
		t.Fatalf("err=%v called=%d audit=%d", err, fake.called, len(a.events))
	}
	if p, mod := m.Name(); p != ProviderAnthropic || mod != "claude-sonnet-5-5" {
		t.Errorf("Name() = %s %s", p, mod)
	}
}
