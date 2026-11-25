package model

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// maxResponseBody caps the answer read from a model API.
const maxResponseBody = 1 << 20

// postJSON sends body as a JSON POST to url and reads at most
// maxResponseBody bytes of the answer. prepare, when set, runs after the
// Content-Type header is set and before the request is sent: it adds auth
// headers or signs the request, and its error is returned as is.
//
// A non-200 answer is not an error here: the caller maps status, header
// and body to a CallError with its own body shape (see httpError).
func postJSON(ctx context.Context, client *http.Client, url string, body []byte, prepare func(*http.Request) *CallError) ([]byte, int, http.Header, *CallError) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, &CallError{Class: Bad, Msg: cut(err.Error(), maxErrBytes)}
	}
	req.Header.Set("Content-Type", "application/json")
	if prepare != nil {
		if ce := prepare(req); ce != nil {
			return nil, 0, nil, ce
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		// Connection refused, a timeout, DNS: busy. A refused redirect: bad.
		return nil, 0, nil, transportError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, &CallError{Class: Busy, Status: resp.StatusCode, Msg: cut(err.Error(), maxErrBytes)}
	}
	return data, resp.StatusCode, resp.Header, nil
}

// httpError builds the CallError of a non-200 answer. class is usually
// classOf(status); a backend passes another one for a status its API uses
// differently (Bedrock's 424, OpenAI's 429 without credits). The message
// is notFound for a NotFound class when notFound is set, else msg (the
// API's own error text, cut), else "HTTP <status>". hdr may be nil.
func httpError(class Class, status int, hdr http.Header, msg, notFound string) *CallError {
	ce := &CallError{Class: class, Status: status, RetryAfter: retryAfter(hdr)}
	switch {
	case class == NotFound && notFound != "":
		ce.Msg = notFound
	case msg != "":
		ce.Msg = cut(msg, maxErrBytes)
	default:
		ce.Msg = fmt.Sprintf("HTTP %d", status)
	}
	return ce
}

// stopNames are the stop reason names of the Anthropic Messages API (also
// on Bedrock) and of replay recordings. "end" stands for every normal end;
// the API's own names for it (end_turn, stop_sequence, tool_use) are not
// listed and map to StopEnd too.
var stopNames = map[string]Stop{
	"end":        StopEnd,
	"max_tokens": StopMaxTokens,
	"refusal":    StopRefusal,
}

// stopFromName returns the Stop of name; false (and StopEnd) for a name
// not in stopNames.
func stopFromName(name string) (Stop, bool) {
	s, ok := stopNames[name]
	return s, ok
}

// stopName is the name of s in stopNames.
func stopName(s Stop) string {
	for name, v := range stopNames {
		if v == s {
			return name
		}
	}
	return "end"
}
