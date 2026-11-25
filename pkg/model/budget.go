package model

import (
	"context"
	"encoding/json"
)

// Hold is one reserved estimate: the UTC day it was reserved on and how
// many tokens it holds.
type Hold struct {
	Day    string
	Tokens int64
}

// Budget is a daily token budget, possibly shared by several processes. A caller reserves
// a call's Estimate before the call and settles the hold to what the call
// used after it, also after a failed call: its tokens may be spent, and
// the hold must not stay all day.
type Budget interface {
	// Reserve holds est tokens; ok is false when they do not fit today.
	Reserve(ctx context.Context, est int64) (h Hold, ok bool, err error)
	// Settle gives h back and counts used. It logs its own errors: a
	// failed settle keeps the hold counted until the UTC day ends, so the
	// budget errs on the side of spending less.
	Settle(ctx context.Context, h Hold, used int64)
}

// Estimate is what one call may spend: its input at about four bytes a
// token, plus the whole output allowance. The input is the system prompt,
// the schema, each turn's text, the tool_use and tool_result blocks and
// the tool definitions as sent (JSON). It is reserved before the call and
// settled to the real count after, so it only has to be in the right
// range.
func Estimate(req Request) int64 {
	n := len(req.System) + len(req.Schema)
	for _, t := range req.Turns {
		n += len(t.Text)
		for _, b := range t.Blocks {
			n += len(b.Content) + len(b.Input) + len(b.Name)
		}
	}
	for _, td := range req.Tools {
		def, _ := json.Marshal(td)
		n += len(def)
	}
	return int64(n/4) + int64(req.MaxTokens)
}
