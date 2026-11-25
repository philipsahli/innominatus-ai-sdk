package llmtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/llmtest"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
)

// The backend answers from its script, records the redacted request, and
// pins it against testdata/ai-requests/example.json.
func TestBackend_RecordsRedactedRequest(t *testing.T) {
	b := llmtest.New(model.Reply{Text: `{"ok":true}`, Stop: model.StopEnd})
	ctx := model.WithPromptVersion(context.Background(), "example-v1")
	rep, err := b.Model().Call(ctx, model.Request{
		System:    "You answer in JSON.",
		Turns:     []model.Turn{{Role: "user", Text: "deploy with password=hunter2-very-secret"}},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Text != `{"ok":true}` {
		t.Errorf("reply = %q", rep.Text)
	}
	reqs := b.Requests()
	if len(reqs) != 1 || strings.Contains(reqs[0].Turns[0].Text, "hunter2") {
		t.Fatalf("requests = %+v, want one, redacted", reqs)
	}
	b.Assert(t, "example")
}

// A call past the script is an error, so a feature that calls more often
// than the test expects fails.
func TestBackend_CallPastScriptFails(t *testing.T) {
	b := llmtest.New()
	if _, err := b.Model().Call(context.Background(), model.Request{}); err == nil {
		t.Fatal("call past the script succeeded")
	}
}
