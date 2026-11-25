package model

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/redact"
)

// ProviderReplay answers from recorded answers (a keyless demo): no HTTP
// client, no key, and a request nobody recorded gets NotFound instead of
// a live model.
const ProviderReplay = "replay"

// recordingVersion is the file format's v.
const recordingVersion = 1

// noPromptVersion is the folder of calls whose caller set no prompt
// version (WithPromptVersion).
const noPromptVersion = "none"

// replayReason is the allowlist reason for replay.
const replayReason = "recorded answers: no prompt leaves the process"

// recording is one file. Request holds the redacted request so a reviewer
// sees what the answer was for; it lives in a repository the operator
// controls, not in the ai_audit table, which keeps no prompt text.
type recording struct {
	V             int             `json:"v"`
	PromptVersion string          `json:"prompt_version"`
	PromptHash    string          `json:"prompt_hash"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	RecordedAt    time.Time       `json:"recorded_at"`
	Stop          string          `json:"stop"`
	Text          string          `json:"text"`
	TokensIn      int64           `json:"tokens_in"`
	TokensOut     int64           `json:"tokens_out"`
	Request       recordedRequest `json:"request"`
}

type recordedRequest struct {
	System string          `json:"system"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Turns  []recordedTurn  `json:"turns"`
}

type recordedTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func (r recordedRequest) request() Request {
	turns := make([]Turn, len(r.Turns))
	for i, t := range r.Turns {
		turns[i] = Turn{Role: t.Role, Text: t.Text}
	}
	return Request{System: r.System, Schema: r.Schema, Turns: turns}
}

// newRecording builds the file for one answered call. req is the request
// as sent (redacted by Instrument); the answer is redacted again, because
// a model can echo a value back.
func newRecording(version string, req Request, provider, model string, rep Reply, at time.Time) recording {
	turns := make([]recordedTurn, len(req.Turns))
	for i, t := range req.Turns {
		// Text only: a request with tool blocks is never recorded (recorder.Call).
		turns[i] = recordedTurn{Role: t.Role, Text: t.Text}
	}
	return recording{
		V: recordingVersion, PromptVersion: version, PromptHash: promptHash(req),
		Provider: provider, Model: model, RecordedAt: at.UTC(),
		Stop: stopName(rep.Stop), Text: redact.Prompt(rep.Text),
		TokensIn: rep.InputTokens, TokensOut: rep.OutputTokens,
		Request: recordedRequest{System: req.System, Schema: req.Schema, Turns: turns},
	}
}

// versionName is what a prompt version may look like as a folder name; it
// comes from code, but a path in it would write outside the folder.
var versionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var hashName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func versionDir(version string) (string, error) {
	if version == "" {
		return noPromptVersion, nil
	}
	if !versionName.MatchString(version) {
		return "", fmt.Errorf("prompt version %q is not a folder name", version)
	}
	return version, nil
}

// writeRecordingFile writes rec under dir and returns its path. It writes
// a temporary file and renames it, so a replay never reads half a file.
func writeRecordingFile(dir string, rec recording) (string, error) {
	sub, err := versionDir(rec.PromptVersion)
	if err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	folder := filepath.Join(dir, sub)
	if err := os.MkdirAll(folder, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(folder, rec.PromptHash+".json")
	tmp, err := os.CreateTemp(folder, ".rec-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// replay is the replay backend: a map from <version>/<hash> to the
// recorded answer, read once at start.
type replay struct {
	dir     string
	answers map[string]recording
	model   string
}

// NewReplay reads every recording under dir. A folder without recordings,
// a file that is not JSON, has another v, or whose name, folder or stored
// request does not match its key refuses the start: a demo that would
// answer some requests wrongly is worse than one that does not start.
func NewReplay(dir string) (Model, error) {
	fail := func(format string, args ...any) (Model, error) {
		return nil, fmt.Errorf("replay dir %s: %s", dir, fmt.Sprintf(format, args...))
	}
	r := &replay{dir: dir, answers: map[string]recording{}}
	models := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			// README files and the like are allowed next to the recordings.
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 2 {
			return fmt.Errorf("%s: a recording sits in <prompt version>/<hash>.json", rel)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- the operator's recordings folder
		if err != nil {
			return err
		}
		var rec recording
		if err := json.Unmarshal(data, &rec); err != nil {
			return fmt.Errorf("%s: not a recording: %w", rel, err)
		}
		hash := strings.TrimSuffix(parts[1], ".json")
		sub, verr := versionDir(rec.PromptVersion)
		switch {
		case rec.V != recordingVersion:
			return fmt.Errorf("%s: v is %d, this runtime reads v %d; record again", rel, rec.V, recordingVersion)
		case verr != nil || sub != parts[0]:
			return fmt.Errorf("%s: prompt_version %q does not match its folder", rel, rec.PromptVersion)
		case !hashName.MatchString(hash) || rec.PromptHash != hash:
			return fmt.Errorf("%s: prompt_hash does not match the file name", rel)
		case promptHash(rec.Request.request()) != hash:
			return fmt.Errorf("%s: the stored request does not hash to the file name (edited after recording?)", rel)
		}
		if _, ok := stopFromName(rec.Stop); !ok {
			return fmt.Errorf("%s: stop %q is not end, max_tokens or refusal", rel, rec.Stop)
		}
		r.answers[parts[0]+"/"+hash] = rec
		models[rec.Model] = true
		return nil
	})
	if err != nil {
		return fail("%v", err)
	}
	if len(r.answers) == 0 {
		return fail("no recordings (<prompt version>/<hash>.json) in the folder")
	}
	names := make([]string, 0, len(models))
	for m := range models {
		names = append(names, m)
	}
	sort.Strings(names)
	r.model = strings.Join(names, ",")
	return r, nil
}

// Call answers from the recording of this exact request, or NotFound.
// NotFound is final (callers should not retry it), and
// there is no live model behind this backend to fall through to.
func (r *replay) Call(ctx context.Context, req Request) (Reply, error) {
	version := promptVersionOf(ctx)
	sub, err := versionDir(version)
	hash := promptHash(req)
	rec, ok := r.answers[sub+"/"+hash]
	if err != nil || !ok {
		shown := version
		if shown == "" {
			shown = noPromptVersion
		}
		return Reply{}, &CallError{Class: NotFound, Msg: cut(fmt.Sprintf(
			"no recorded answer for this request (prompt %s, hash %s…); this demo answers its starter requests only", shown, hash[:12]), maxErrBytes)}
	}
	stop, _ := stopFromName(rec.Stop)
	// Tokens 0: nothing was spent, so the daily budget stays untouched.
	return Reply{Text: rec.Text, Stop: stop}, nil
}

// Name is replay and the model the answers were recorded with (several,
// comma-separated, when a folder mixes them).
func (r *replay) Name() (string, string) { return ProviderReplay, r.model }

// recorder writes every answered call of a live backend to dir. It sits
// under Guard, so Instrument has redacted the request it sees, and its
// file name is the prompt hash the audit row carries.
type recorder struct {
	Model
	dir string
	now func() time.Time
}

// NewRecorder wraps a live m so that each answered call is also written to
// dir in the layout NewReplay reads: <dir>/<prompt version>/<prompt
// hash>.json. Tool requests pass through unrecorded.
func NewRecorder(m Model, dir string) Model { return newRecorder(m, dir) }

func newRecorder(m Model, dir string) Model {
	return &recorder{Model: m, dir: dir, now: time.Now}
}

func (r *recorder) Call(ctx context.Context, req Request) (Reply, error) {
	version := promptVersionOf(ctx)
	if _, err := versionDir(version); err != nil {
		// Checked before the call: a record run must not spend tokens on
		// an answer it cannot keep.
		return Reply{}, &CallError{Class: Bad, Msg: cut("record mode: "+err.Error(), maxErrBytes)}
	}
	if usesTools(req) {
		// Replay refuses tool requests, so a recording of one could never
		// be served: the call goes to the live backend unrecorded.
		return r.Model.Call(ctx, req)
	}
	rep, err := r.Model.Call(ctx, req)
	if err != nil || outcomeOf(rep, nil) != "ok" {
		return rep, err
	}
	provider, model := r.Name()
	if _, werr := writeRecordingFile(r.dir, newRecording(version, req, provider, model, rep, r.now())); werr != nil {
		// Final, so the person recording sees it on the request; a record
		// run that silently misses a file gives a demo that fails later.
		return rep, &CallError{Class: Bad, Msg: cut("the answer could not be recorded in the record dir: "+werr.Error(), maxErrBytes)}
	}
	return rep, nil
}

// Close closes the live backend.
func (r *recorder) Close() error { return CloseModel(r.Model) }
