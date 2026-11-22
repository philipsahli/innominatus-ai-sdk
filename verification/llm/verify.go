package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai"
	"github.com/philipsahli/innominatus-ai-sdk/pkg/platformai/llm"
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
	Name      string                  `json:"name"`
	Passed    bool                    `json:"passed"`
	Error     string                  `json:"error,omitempty"`
	Responses []*llm.GenerateResponse `json:"responses,omitempty"`
}

func main() {
	common.PrintHeader("🔧 LLM Client Verification")

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		common.Fail("ANTHROPIC_API_KEY not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sdk, err := platformai.New(ctx, &platformai.Config{
		LLM: platformai.LLMConfig{
			Provider: "anthropic",
			APIKey:   apiKey,
			Model:    "claude-sonnet-4-5-20250929",
		},
	})
	if err != nil {
		common.Fail("SDK initialization failed: %v", err)
	}
	client := sdk.LLM()

	steps := []struct {
		name string
		run  func(context.Context, llm.Client) ([]*llm.GenerateResponse, error)
	}{
		{"Generate", verifyGenerate},
		{"GenerateWithContext", verifyGenerateWithContext},
		{"GenerateWithTools round trip", verifyToolRoundTrip},
	}

	var results []stepResult
	failed := false
	for i, step := range steps {
		fmt.Printf("\n%d. %s...\n", i+1, step.name)
		responses, err := step.run(ctx, client)
		result := stepResult{Name: step.name, Passed: err == nil, Responses: responses}
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
		"steps":        results,
	})
	if err != nil {
		common.Fail("Failed to save artifact: %v", err)
	}
	fmt.Printf("  Saved: %s\n", path)

	if failed {
		common.Fail("LLM verification failed")
	}
	fmt.Println("\n✅ PASS: LLM verification completed successfully")
}

func verifyGenerate(ctx context.Context, client llm.Client) ([]*llm.GenerateResponse, error) {
	resp, err := client.Generate(ctx, llm.GenerateRequest{
		UserPrompt: "Reply with only the word OK and nothing else.",
		MaxTokens:  16,
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	responses := []*llm.GenerateResponse{resp}

	if !strings.Contains(strings.ToUpper(resp.Text), "OK") {
		return responses, fmt.Errorf("expected text containing OK, got %q", resp.Text)
	}
	if resp.Usage.TotalTokens <= 0 {
		return responses, fmt.Errorf("expected Usage.TotalTokens > 0, got %d", resp.Usage.TotalTokens)
	}
	if resp.StopReason != "end_turn" {
		return responses, fmt.Errorf("expected StopReason end_turn, got %q", resp.StopReason)
	}
	fmt.Printf("  text=%q tokens=%d stop=%s\n", resp.Text, resp.Usage.TotalTokens, resp.StopReason)
	return responses, nil
}

func verifyGenerateWithContext(ctx context.Context, client llm.Client) ([]*llm.GenerateResponse, error) {
	additionalContext := fmt.Sprintf("Internal note: the deployment code for the staging cluster is %s.", contextFact)
	resp, err := client.GenerateWithContext(ctx, llm.GenerateRequest{
		UserPrompt: "What is the deployment code for the staging cluster? Reply with only the code.",
		MaxTokens:  32,
	}, additionalContext)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	responses := []*llm.GenerateResponse{resp}

	if !strings.Contains(resp.Text, contextFact) {
		return responses, fmt.Errorf("expected text containing %s from the context, got %q", contextFact, resp.Text)
	}
	fmt.Printf("  text=%q\n", resp.Text)
	return responses, nil
}

func verifyToolRoundTrip(ctx context.Context, client llm.Client) ([]*llm.GenerateResponse, error) {
	tools := []llm.Tool{{
		Name:        toolName,
		Description: "Returns the team that owns a service.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"service_name": map[string]interface{}{
					"type":        "string",
					"description": "Name of the service",
				},
			},
			"required": []string{"service_name"},
		},
	}}
	question := llm.Message{
		Role: "user",
		Content: []llm.ContentBlock{{
			Type: "text",
			Text: fmt.Sprintf("Which team owns the %s service? Use the tool.", serviceName),
		}},
	}

	first, err := client.GenerateWithTools(ctx, llm.GenerateWithToolsRequest{
		Messages:  []llm.Message{question},
		Tools:     tools,
		MaxTokens: 512,
	})
	if err != nil {
		return nil, fmt.Errorf("first request failed: %w", err)
	}
	responses := []*llm.GenerateResponse{first}

	if first.StopReason != "tool_use" {
		return responses, fmt.Errorf("expected StopReason tool_use, got %q", first.StopReason)
	}
	if len(first.ToolUses) != 1 {
		return responses, fmt.Errorf("expected 1 tool use, got %d", len(first.ToolUses))
	}
	toolUse := first.ToolUses[0]
	if toolUse.Name != toolName {
		return responses, fmt.Errorf("expected tool %s, got %s", toolName, toolUse.Name)
	}
	if got, _ := toolUse.Input["service_name"].(string); got != serviceName {
		return responses, fmt.Errorf("expected tool input service_name=%s, got input %v", serviceName, toolUse.Input)
	}
	fmt.Printf("  tool_use: %s(%v)\n", toolUse.Name, toolUse.Input)

	// Replay the whole assistant turn: any text it produced plus the tool_use block.
	var assistantContent []llm.ContentBlock
	if first.Text != "" {
		assistantContent = append(assistantContent, llm.ContentBlock{Type: "text", Text: first.Text})
	}
	assistantContent = append(assistantContent, llm.ContentBlock{
		Type:  "tool_use",
		ID:    toolUse.ID,
		Name:  toolUse.Name,
		Input: toolUse.Input,
	})

	second, err := client.GenerateWithTools(ctx, llm.GenerateWithToolsRequest{
		Messages: []llm.Message{
			question,
			{Role: "assistant", Content: assistantContent},
			{Role: "user", Content: []llm.ContentBlock{{
				Type:      "tool_result",
				ToolUseID: toolUse.ID,
				Content:   serviceOwner,
			}}},
		},
		Tools:     tools,
		MaxTokens: 512,
	})
	if err != nil {
		return responses, fmt.Errorf("second request failed: %w", err)
	}
	responses = append(responses, second)

	if !strings.Contains(second.Text, serviceOwner) {
		return responses, fmt.Errorf("expected final text containing %s, got %q", serviceOwner, second.Text)
	}
	fmt.Printf("  final text=%q\n", second.Text)
	return responses, nil
}
