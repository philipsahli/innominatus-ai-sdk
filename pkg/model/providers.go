package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// FailoverNone is the failover_region of a provider that does not fail
// over to another region.
const FailoverNone = "none"

// ErrProviderNotAllowed is wrapped by the error of a call the allowlist
// refused before any data was sent.
var ErrProviderNotAllowed = errors.New("AI provider not allowed")

// ProviderAllowlist is the installation's list of model providers that
// may receive prompts, each with the contract terms the operator checked.
type ProviderAllowlist struct {
	// CHOnly restricts primary and failover regions to ch. Without it,
	// ch and eu are allowed.
	CHOnly    bool            `yaml:"ch_only"`
	Providers []ProviderEntry `yaml:"providers"`
	// File is where the list was read; "" when no file is configured.
	File string `yaml:"-"`
	// Wording names the program's own setting, docs and local mode in
	// Decide's reasons. The zero value uses neutral names.
	Wording AllowlistWording `yaml:"-"`
}

// AllowlistWording names the parts of the calling program that Decide's
// reasons point people to: they reach people who did not configure the
// program (a failed job, a 503), so they say what to set and where to read.
type AllowlistWording struct {
	// Setting is what points at the allowlist file, such as an env var.
	Setting string
	// Doc is where an operator reads how to write the list; "" omits it.
	Doc string
	// Local names the single-user mode in which unchecked providers are
	// allowed, such as "my-tool-local".
	Local string
}

func (w AllowlistWording) setting() string {
	if w.Setting == "" {
		return "the provider allowlist setting"
	}
	return w.Setting
}

func (w AllowlistWording) local() string {
	if w.Local == "" {
		return "local mode"
	}
	return w.Local
}

// see is ", see <doc>", or "" without a doc.
func (w AllowlistWording) see() string {
	if w.Doc == "" {
		return ""
	}
	return ", see " + w.Doc
}

// ProviderEntry is one allowed provider endpoint and its contract. Every
// field must be filled: an entry with a gap counts as not checked.
type ProviderEntry struct {
	// Name is the provider: ProviderAnthropic, ProviderOllama, ...
	Name string `yaml:"name"`
	// Endpoint is the base URL the backend calls; it must equal the
	// configured one (a trailing slash and case of scheme and host aside).
	Endpoint string `yaml:"endpoint"`
	// Models lists the allowed models; "*" allows any.
	Models []string `yaml:"models"`
	// Region and FailoverRegion are where the provider processes data
	// (ch, eu, us, ...); FailoverRegion is "none" when it does not fail over.
	Region         string `yaml:"region"`
	FailoverRegion string `yaml:"failover_region"`
	// DataRetention must be "none" (zero data retention).
	DataRetention string `yaml:"data_retention"`
	// Training must be "no": the provider does not train on the prompts.
	Training string `yaml:"training"`
	DPA      DPA    `yaml:"dpa"`
}

// DPA is the data processing agreement with the provider.
type DPA struct {
	// Signed is a pointer so that a missing value is told apart from false.
	Signed    *bool  `yaml:"signed"`
	Reference string `yaml:"reference"`
}

// ProviderDecision says whether prompts may go to the configured provider.
// A program can show it on a status page and write it to its audit log.
type ProviderDecision struct {
	Name    string `json:"name"`
	Region  string `json:"region,omitempty"`
	Allowed bool   `json:"allowed"`
	// ContractChecked is true only for a complete allowlist entry whose
	// terms pass; local mode may call a provider without it.
	ContractChecked bool   `json:"contract_checked"`
	Reason          string `json:"reason,omitempty"`
}

var knownProviders = []string{ProviderAnthropic, ProviderOllama, ProviderOpenAI, ProviderBedrock, ProviderCopilot}

// AllowlistSyntaxError is a ParseProviderAllowlist error from the YAML
// decoder, as opposed to a rule the entries break: callers name the file
// in the first case and only the setting in the second.
type AllowlistSyntaxError struct{ Err error }

func (e *AllowlistSyntaxError) Error() string { return e.Err.Error() }
func (e *AllowlistSyntaxError) Unwrap() error { return e.Err }

// ParseProviderAllowlist reads an allowlist file's content. Content that is
// not YAML, has an unknown field, an unknown provider, a bad endpoint or the
// same endpoint twice is an error: the caller should turn every AI feature
// off rather than guess. The caller sets File and Wording.
func ParseProviderAllowlist(data []byte) (ProviderAllowlist, error) {
	var a ProviderAllowlist
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&a); err != nil && !errors.Is(err, io.EOF) {
		return ProviderAllowlist{}, &AllowlistSyntaxError{Err: err}
	}
	seen := map[string]bool{}
	for i := range a.Providers {
		e := &a.Providers[i]
		e.normalise()
		if e.Name == "" {
			return ProviderAllowlist{}, fmt.Errorf("provider %d has no name", i+1)
		}
		if !slices.Contains(knownProviders, e.Name) {
			return ProviderAllowlist{}, fmt.Errorf("provider %q is not one of %s", e.Name, strings.Join(knownProviders, ", "))
		}
		if e.Endpoint != "" {
			if _, err := normaliseEndpoint(e.Endpoint); err != nil {
				return ProviderAllowlist{}, fmt.Errorf("provider %s: endpoint: %w", e.Name, err)
			}
			key := e.Name + " " + mustNormalise(e.Endpoint)
			if seen[key] {
				return ProviderAllowlist{}, fmt.Errorf("provider %s at %s is listed twice", e.Name, hostOf(e.Endpoint))
			}
			seen[key] = true
		}
	}
	return a, nil
}

func (e *ProviderEntry) normalise() {
	low := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	e.Name, e.Region, e.FailoverRegion = low(e.Name), low(e.Region), low(e.FailoverRegion)
	e.DataRetention, e.Training = low(e.DataRetention), low(e.Training)
	e.Endpoint = strings.TrimSpace(e.Endpoint)
	e.DPA.Reference = strings.TrimSpace(e.DPA.Reference)
}

// missing names the contract fields that are not filled in.
func (e ProviderEntry) missing() []string {
	var m []string
	add := func(empty bool, name string) {
		if empty {
			m = append(m, name)
		}
	}
	add(e.Endpoint == "", "endpoint")
	add(len(e.Models) == 0, "models")
	add(e.Region == "", "region")
	add(e.FailoverRegion == "", "failover_region")
	add(e.DataRetention == "", "data_retention")
	add(e.Training == "", "training")
	add(e.DPA.Signed == nil, "dpa.signed")
	add(e.DPA.Reference == "", "dpa.reference")
	return m
}

// violations lists the filled-in terms that do not meet the contract.
func (a ProviderAllowlist) violations(e ProviderEntry) []string {
	var v []string
	allowed := []string{"ch", "eu"}
	scope := "CH/EU"
	if a.CHOnly {
		allowed, scope = []string{"ch"}, "CH only"
	}
	if !slices.Contains(allowed, e.Region) {
		v = append(v, fmt.Sprintf("region %q is outside %s", e.Region, scope))
	}
	if e.FailoverRegion != FailoverNone && !slices.Contains(allowed, e.FailoverRegion) {
		v = append(v, fmt.Sprintf("failover_region %q is outside %s", e.FailoverRegion, scope))
	}
	if e.DataRetention != "none" {
		v = append(v, fmt.Sprintf("data_retention is %q, not none", e.DataRetention))
	}
	if e.Training != "no" {
		v = append(v, fmt.Sprintf("training is %q, not no", e.Training))
	}
	if !*e.DPA.Signed {
		v = append(v, "dpa is not signed")
	}
	return v
}

// Decide says whether a prompt may go to provider at endpoint for model.
// local is the single-user mode (one developer): there a provider that is not
// listed, or whose entry has gaps, is allowed with "contract not checked";
// a shared server refuses it, also when no list is configured. A listed
// entry whose filled-in terms fail (region, retention, training, no DPA)
// is refused in both.
//
// Every reason that is not "allowed" names Wording.Setting and what the
// entry needs, because it reaches people who did not configure the program
// (a failed job, a 503).
//
// Replay is allowed without an entry: it sends nothing anywhere, so no
// contract applies (ContractChecked stays false). Whether a shared server
// may use it is the caller's call.
func (a ProviderAllowlist) Decide(provider, endpoint, model string, local bool) ProviderDecision {
	if provider == ProviderReplay {
		return ProviderDecision{Name: provider, Allowed: true, Reason: replayReason}
	}
	d := ProviderDecision{Name: provider}
	entry, ok := a.find(provider, endpoint, model)
	if !ok {
		w := a.Wording
		why := fmt.Sprintf("%s at %s with model %s is not on the provider allowlist (%s); add an entry with its contract terms%s",
			provider, hostOf(endpoint), model, a.where(), w.see())
		if !local && !a.configured() {
			why = fmt.Sprintf("%s is not set, so this shared server sends no prompt to %s at %s (model %s); write an allowlist file that lists it with its contract terms and set %s to its path%s",
				w.setting(), provider, hostOf(endpoint), model, w.setting(), w.see())
		}
		return a.unchecked(d, why, local)
	}
	d.Region = entry.Region
	if m := entry.missing(); len(m) > 0 {
		return a.unchecked(d, fmt.Sprintf("the allowlist entry of %s at %s (%s) is missing %s; fill them in%s",
			provider, hostOf(endpoint), a.where(), strings.Join(m, ", "), a.Wording.see()), local)
	}
	d.ContractChecked = true
	if v := a.violations(entry); len(v) > 0 {
		d.Reason = fmt.Sprintf("%s at %s is refused by the provider allowlist (%s): %s", provider, hostOf(endpoint), a.where(), strings.Join(v, "; "))
		return d
	}
	d.Allowed = true
	return d
}

// configured reports whether the installation has an allowlist: a file is
// set, or entries were given (tests build lists in memory).
func (a ProviderAllowlist) configured() bool {
	return a.File != "" || len(a.Providers) > 0
}

// where names the allowlist for messages.
func (a ProviderAllowlist) where() string {
	if a.Wording.Setting == "" {
		if a.File == "" {
			return "no allowlist file is set"
		}
		return "allowlist file " + a.File
	}
	if a.File == "" {
		return a.Wording.Setting + " is not set"
	}
	return a.Wording.Setting + "=" + a.File
}

// unchecked decides a provider without a complete entry: allowed in
// local mode, refused on a shared server.
func (a ProviderAllowlist) unchecked(d ProviderDecision, why string, local bool) ProviderDecision {
	if local {
		d.Allowed = true
		d.Reason = "contract not checked: " + why + "; allowed because this is " + a.Wording.local()
		return d
	}
	d.Reason = why
	return d
}

func (a ProviderAllowlist) find(provider, endpoint, model string) (ProviderEntry, bool) {
	want, err := normaliseEndpoint(endpoint)
	if err != nil {
		return ProviderEntry{}, false
	}
	for _, e := range a.Providers {
		if e.Name != provider {
			continue
		}
		// An entry without an endpoint still matches by name, so the
		// decision names the missing field instead of "not listed".
		if e.Endpoint != "" && mustNormalise(e.Endpoint) != want {
			continue
		}
		if len(e.Models) > 0 && !slices.Contains(e.Models, "*") && !slices.Contains(e.Models, model) {
			continue
		}
		return e, true
	}
	return ProviderEntry{}, false
}

// normaliseEndpoint lowercases scheme and host and drops a trailing slash,
// so https://API.example/v1/ and https://api.example/v1 are one endpoint.
func normaliseEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("not an http or https URL with a host")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/"), nil
}

func mustNormalise(raw string) string {
	s, _ := normaliseEndpoint(raw)
	return s
}

// hostOf is the host of an endpoint for messages: the full URL may carry
// a password.
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "an unparsable endpoint"
	}
	return u.Host
}

// ProviderEventKind says what a ProviderEvent reports.
type ProviderEventKind string

const (
	// ProviderRefused: the allowlist refused a call; nothing was sent.
	ProviderRefused ProviderEventKind = "refused"
	// ProviderUncheckedUsed: a prompt went to a provider whose contract
	// was not checked (local mode). Reported once per Guard.
	ProviderUncheckedUsed ProviderEventKind = "unchecked_used"
)

// ProviderEvent is what Guard reports for the caller's audit log: the
// provider, model, region and reason, never the prompt.
type ProviderEvent struct {
	Kind ProviderEventKind
	// Component is the Instrument component of the call, "" when the
	// guarded model is called without Instrument.
	Component string
	Model     string
	Decision  ProviderDecision
}

// Guard returns m behind the allowlist: every Call is decided again, and a
// refused one returns ErrProviderNotAllowed without m seeing the request.
// endpoint is the base URL m calls; local allows providers whose contract
// is not checked. report, which may be nil, gets each ProviderEvent.
func Guard(m Model, list ProviderAllowlist, endpoint string, local bool, report func(context.Context, ProviderEvent)) Model {
	return &guarded{Model: m, list: list, endpoint: endpoint, local: local, report: report}
}

type guarded struct {
	Model
	list     ProviderAllowlist
	endpoint string
	local    bool
	report   func(context.Context, ProviderEvent)
	// unchecked reports ProviderUncheckedUsed once: the provider and its
	// contract do not change while the program runs, and an event per
	// call would bury the audit log in local mode.
	unchecked sync.Once
}

func (g *guarded) Call(ctx context.Context, req Request) (Reply, error) {
	if err := g.admit(ctx); err != nil {
		return Reply{}, err
	}
	return g.Model.Call(ctx, req)
}

// Stream is Call, streaming the answer text to onText.
func (g *guarded) Stream(ctx context.Context, req Request, onText func(string)) (Reply, error) {
	if err := g.admit(ctx); err != nil {
		return Reply{}, err
	}
	return CallStreaming(ctx, g.Model, req, onText)
}

// admit decides one call and reports its event: an error wrapping
// ErrProviderNotAllowed when the allowlist refuses it. Instrument calls it
// before redaction and the audit trail's start row, then calls g.Model
// itself, so a refused provider gets no trail row and the decision is made
// once per call.
func (g *guarded) admit(ctx context.Context) error {
	provider, model := g.Name()
	d := g.list.Decide(provider, g.endpoint, model, g.local)
	switch {
	case !d.Allowed:
		g.record(ctx, ProviderRefused, model, d)
		// Bad: final, a retry must not repeat a call policy refuses.
		return fmt.Errorf("%w: %w", ErrProviderNotAllowed, &CallError{Class: Bad, Msg: cut(d.Reason, maxErrBytes)})
	case provider == ProviderReplay:
		// An unchecked contract matters when a prompt leaves the process;
		// a replayed call sends nothing.
	case !d.ContractChecked:
		g.unchecked.Do(func() { g.record(ctx, ProviderUncheckedUsed, model, d) })
	}
	return nil
}

func (g *guarded) record(ctx context.Context, kind ProviderEventKind, model string, d ProviderDecision) {
	if g.report == nil {
		return
	}
	g.report(ctx, ProviderEvent{Kind: kind, Component: componentFrom(ctx), Model: model, Decision: d})
}

// Close closes the guarded backend (the copilot CLI process).
func (g *guarded) Close() error { return CloseModel(g.Model) }

type componentKey struct{}

// withComponent records the Instrument component on ctx, for the guard's
// audit record.
func withComponent(ctx context.Context, component string) context.Context {
	return context.WithValue(ctx, componentKey{}, component)
}

func componentFrom(ctx context.Context) string {
	s, _ := ctx.Value(componentKey{}).(string)
	return s
}
