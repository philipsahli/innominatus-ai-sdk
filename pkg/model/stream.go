package model

import (
	"context"
	"time"
)

// StreamTimeout bounds one streamed call. It is longer than CallTimeout:
// a person watches the answer arrive, and a long answer takes minutes.
const StreamTimeout = 10 * time.Minute

// Streamer is an optional interface of a Model: Stream is Call that hands
// each piece of the answer text to onText as it arrives. The Reply is the
// whole answer, as Call returns it. The Anthropic backend and the
// wrappers (Guard, Instrument, Redacting) implement it.
type Streamer interface {
	Stream(ctx context.Context, req Request, onText func(string)) (Reply, error)
}

// CallStreaming calls m.Stream when m streams. Otherwise it calls m.Call
// and hands the whole answer text to onText once, so a caller can always
// stream.
func CallStreaming(ctx context.Context, m Model, req Request, onText func(string)) (Reply, error) {
	if onText == nil {
		onText = func(string) {}
	}
	if s, ok := m.(Streamer); ok {
		return s.Stream(ctx, req, onText)
	}
	rep, err := m.Call(ctx, req)
	if err == nil && rep.Text != "" {
		onText(rep.Text)
	}
	return rep, err
}
