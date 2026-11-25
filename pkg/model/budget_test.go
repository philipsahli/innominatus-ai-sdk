package model

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimate(t *testing.T) {
	tool := ToolDef{Name: "list_applications", Description: "Lists the user's applications", InputSchema: json.RawMessage(`{"type":"object"}`)}
	def, err := json.Marshal(tool)
	require.NoError(t, err)

	tests := []struct {
		name string
		req  Request
		want int64
	}{
		{"empty request is the output allowance", Request{MaxTokens: 1000}, 1000},
		{"system and turn text at four bytes a token",
			Request{System: strings.Repeat("s", 400), Turns: []Turn{{Role: "user", Text: strings.Repeat("u", 400)}}, MaxTokens: 1000}, 1200},
		{"schema counts",
			Request{Schema: json.RawMessage(strings.Repeat("x", 80)), MaxTokens: 10}, 30},
		{"every turn counts",
			Request{Turns: []Turn{{Role: "user", Text: "aaaa"}, {Role: "assistant", Text: "bbbb"}, {Role: "user", Text: "cccc"}}}, 3},
		{"tool_use block: name and input",
			Request{Turns: []Turn{{Role: "assistant", Blocks: []Block{{Kind: BlockToolUse, ID: "toolu_1", Name: "abcd", Input: json.RawMessage(`{"x":1234}`)}}}}}, (4 + 10) / 4},
		{"tool_result block: content, not the ID",
			Request{Turns: []Turn{{Role: "user", Blocks: []Block{{Kind: BlockToolResult, ID: "toolu_1", Content: strings.Repeat("r", 40)}}}}}, 10},
		{"tool definitions as JSON",
			Request{Tools: []ToolDef{tool, tool}}, int64(2 * len(def) / 4)},
		{"bytes are summed before the division",
			Request{System: "abc", Turns: []Turn{{Role: "user", Text: "d"}}}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Estimate(tt.req))
		})
	}
}
