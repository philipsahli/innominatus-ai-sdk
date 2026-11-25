// Command verify calls the real Anthropic API through pkg/model: a plain
// answer, an answer that must use the system prompt, a JSON answer to a
// schema, a tool round trip through model.Loop and a streamed answer. It
// saves what it saw to docs/verification/.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/model"
	"github.com/philipsahli/innominatus-ai-sdk/verification/common"
)

const (
	// Facts the model cannot know, so a correct answer proves the context or tool result was used.
	contextFact  = "ZEPHYR-7731"
	serviceName  = "payment-gateway"
	serviceOwner = "team-kestrel"
	toolName     = "get_service_owner"
)

type stepResult struct {
	Name    string        `json:"name"`
	Passed  bool          `json:"passed"`
	Error   string        `json:"error,omitempty"`
	Replies []model.Reply `json:"replies,omitempty"`
}

func main() {
	common.PrintHeader("🔧 Model Verification (Anthropic)")

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		common.Fail("ANTHROPIC_API_KEY not set")
	}
	name := os.Getenv("ANTHROPIC_MODEL")
	if name == "" {
		name = "claude-sonnet-5-5"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	m := model.Instrument(model.NewAnthropic(apiKey, model.DefaultAnthropicBaseURL, name), "verification")

	steps := []struct {
		name string
		run  func(context.Context, model.Model) ([]model.Reply, error)
	}{
		{"Plain answer", verifyPlain},
		{"System prompt context", verifyContext},
		{"JSON schema answer", verifySchema},
		{"Tool round trip (Loop)", verifyToolRoundTrip},
		{"Streamed answer", verifyStream},
	}

	var results []stepResult
	failed := false
	for i, step := range steps {
		fmt.Printf("\n%d. %s...\n", i+1, step.name)
		replies, err := step.run(ctx, m)
		result := stepResult{Name: step.name, Passed: err == nil, Replies: replies}
		if err != nil {
			result.Error = err.Error()
			failed = true
			fmt.Printf("❌ %s: %v\n", step.name, err)
		} else {
			fmt.Printf("✅ %s\n", step.name)
		}
		results = append(results, result)
	}

	fmt.Println("\nSaving artifact...")
	path, err := common.SaveJSON("llm", map[string]any{
		"timestamp":    time.Now(),
		"verification": "llm",
		"model":        name,
		"steps":        results,
	})
	if err != nil {
		common.Fail("Failed to save artifact: %v", err)
	}
	fmt.Printf("  Saved: %s\n", path)

	if failed {
		common.Fail("model verification failed")
	}
	fmt.Println("\n✅ PASS: model verification completed successfully")
}

func verifyPlain(ctx context.Context, m model.Model) ([]model.Reply, error) {
	rep, err := m.Call(ctx, model.Request{
		Turns:     []model.Turn{{Role: "user", Text: "Reply with only the word OK and nothing else."}},
		MaxTokens: 16,
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	replies := []model.Reply{rep}
	if !strings.Contains(strings.ToUpper(rep.Text), "OK") {
		return replies, fmt.Errorf("expected text containing OK, got %q", rep.Text)
	}
	if rep.InputTokens <= 0 || rep.OutputTokens <= 0 {
		return replies, fmt.Errorf("expected token counts > 0, got %d in, %d out", rep.InputTokens, rep.OutputTokens)
	}
	if rep.Stop != model.StopEnd {
		return replies, fmt.Errorf("expected a normal stop, got %v", rep.Stop)
	}
	fmt.Printf("  text=%q tokens=%d/%d\n", rep.Text, rep.InputTokens, rep.OutputTokens)
	return replies, nil
}

func verifyContext(ctx context.Context, m model.Model) ([]model.Reply, error) {
	rep, err := m.Call(ctx, model.Request{
		System:    "The deployment code of the current release is " + contextFact + ".",
		Turns:     []model.Turn{{Role: "user", Text: "What is the deployment code of the current release? Reply with the code only."}},
		MaxTokens: 32,
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if !strings.Contains(rep.Text, contextFact) {
		return []model.Reply{rep}, fmt.Errorf("expected %s in the answer, got %q", contextFact, rep.Text)
	}
	fmt.Printf("  text=%q\n", rep.Text)
	return []model.Reply{rep}, nil
}

func verifySchema(ctx context.Context, m model.Model) ([]model.Reply, error) {
	rep, err := m.Call(ctx, model.Request{
		Turns:     []model.Turn{{Role: "user", Text: "What is 6 times 7?"}},
		Schema:    json.RawMessage(`{"type":"object","properties":{"answer":{"type":"integer"}},"required":["answer"],"additionalProperties":false}`),
		MaxTokens: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	var out struct{ Answer int }
	if err := json.Unmarshal([]byte(rep.Text), &out); err != nil {
		return []model.Reply{rep}, fmt.Errorf("answer is not the schema's JSON: %w (%q)", err, rep.Text)
	}
	if out.Answer != 42 {
		return []model.Reply{rep}, fmt.Errorf("expected 42, got %d", out.Answer)
	}
	fmt.Printf("  text=%s\n", rep.Text)
	return []model.Reply{rep}, nil
}

func verifyToolRoundTrip(ctx context.Context, m model.Model) ([]model.Reply, error) {
	calls := 0
	loop := model.Loop{
		Model:     m,
		MaxTries:  1,
		MaxRounds: 3,
		Tools: model.ToolFunc(func(_ context.Context, use model.ToolUse) (string, bool) {
			calls++
			var in struct{ Service string }
			if use.Name != toolName || json.Unmarshal(use.Input, &in) != nil || in.Service != serviceName {
				return "unknown service", true
			}
			return serviceOwner, false
		}),
	}
	res, err := loop.Run(ctx, model.Request{
		Turns: []model.Turn{{Role: "user", Text: "Who owns the service " + serviceName + "? Use the tool, then reply with the owner only."}},
		Tools: []model.ToolDef{{
			Name:        toolName,
			Description: "Returns the team that owns a service.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"service":{"type":"string"}},"required":["service"]}`),
		}},
		MaxTokens: 256,
	})
	replies := make([]model.Reply, 0, len(res.Tries))
	for _, t := range res.Tries {
		replies = append(replies, t.Reply)
	}
	if err != nil {
		return replies, fmt.Errorf("loop failed: %w", err)
	}
	last, _ := res.Last()
	if calls == 0 {
		return replies, fmt.Errorf("the model did not call %s", toolName)
	}
	if !strings.Contains(last.Reply.Text, serviceOwner) {
		return replies, fmt.Errorf("expected %s in the final answer, got %q", serviceOwner, last.Reply.Text)
	}
	fmt.Printf("  tool calls=%d rounds=%d final=%q\n", calls, len(res.Tries), last.Reply.Text)
	return replies, nil
}

func verifyStream(ctx context.Context, m model.Model) ([]model.Reply, error) {
	var pieces []string
	rep, err := model.CallStreaming(ctx, m, model.Request{
		Turns:     []model.Turn{{Role: "user", Text: "Count from 1 to 20, separated by spaces, and nothing else."}},
		MaxTokens: 128,
	}, func(s string) { pieces = append(pieces, s) })
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if len(pieces) < 2 {
		return []model.Reply{rep}, fmt.Errorf("expected the answer in several pieces, got %d", len(pieces))
	}
	if strings.Join(pieces, "") != rep.Text {
		return []model.Reply{rep}, fmt.Errorf("the pieces do not add up to the reply text")
	}
	if !strings.Contains(rep.Text, "20") {
		return []model.Reply{rep}, fmt.Errorf("expected the count to reach 20, got %q", rep.Text)
	}
	fmt.Printf("  pieces=%d tokens=%d/%d\n", len(pieces), rep.InputTokens, rep.OutputTokens)
	return []model.Reply{rep}, nil
}
