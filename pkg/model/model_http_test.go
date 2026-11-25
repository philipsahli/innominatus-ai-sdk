package model

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPostJSON_SendsBodyAndPrepare(t *testing.T) {
	var gotCT, gotAuth, gotBody, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotCT, gotAuth = r.Method, r.Header.Get("Content-Type"), r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(`{"x":1}`))
	}))
	defer srv.Close()

	data, status, hdr, ce := postJSON(context.Background(), newHTTPClient(), srv.URL, []byte(`{"a":1}`), func(r *http.Request) *CallError {
		r.Header.Set("Authorization", "Bearer k")
		return nil
	})
	if ce != nil {
		t.Fatalf("postJSON: %v", ce)
	}
	if gotMethod != http.MethodPost || gotCT != "application/json" || gotAuth != "Bearer k" || gotBody != `{"a":1}` {
		t.Errorf("request: method %q, content-type %q, auth %q, body %q", gotMethod, gotCT, gotAuth, gotBody)
	}
	// A non-200 answer is returned to the caller, not mapped here.
	if status != http.StatusTeapot || string(data) != `{"x":1}` || hdr.Get("Retry-After") != "7" {
		t.Errorf("answer: status %d, body %q, retry-after %q", status, data, hdr.Get("Retry-After"))
	}
}

func TestPostJSON_PrepareErrorStopsTheCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()

	want := &CallError{Class: Refused, Msg: "no credentials"}
	_, _, _, ce := postJSON(context.Background(), newHTTPClient(), srv.URL, nil, func(*http.Request) *CallError { return want })
	if ce != want {
		t.Errorf("error = %v, want the prepare error as is", ce)
	}
	if called {
		t.Error("the server was called after prepare failed")
	}
}

func TestPostJSON_CapsTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxResponseBody+100)))
	}))
	defer srv.Close()

	data, _, _, ce := postJSON(context.Background(), newHTTPClient(), srv.URL, nil, nil)
	if ce != nil {
		t.Fatalf("postJSON: %v", ce)
	}
	if len(data) != maxResponseBody {
		t.Errorf("read %d bytes, want %d", len(data), maxResponseBody)
	}
}

func TestPostJSON_TransportErrors(t *testing.T) {
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
	}))
	defer redirect.Close()
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()

	tests := []struct {
		name string
		url  string
		want Class
	}{
		{"redirect refused", redirect.URL, Bad},
		{"connection refused", closed.URL, Busy},
		{"bad URL", "http://[::1", Bad},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, ce := postJSON(context.Background(), newHTTPClient(), tt.url, nil, nil)
			if ce == nil || ce.Class != tt.want {
				t.Errorf("error = %v, want class %s", ce, tt.want)
			}
		})
	}
}

func TestHTTPError(t *testing.T) {
	hdr := http.Header{"Retry-After": []string{"3"}}
	tests := []struct {
		name     string
		class    Class
		status   int
		hdr      http.Header
		msg      string
		notFound string
		wantMsg  string
	}{
		{"not found text wins", NotFound, 404, nil, "api text", "model m is missing", "model m is missing"},
		{"not found without own text", NotFound, 404, nil, "api text", "", "api text"},
		{"api message", Busy, 429, hdr, "slow down", "x", "slow down"},
		{"no message", Bad, 400, nil, "", "x", "HTTP 400"},
		{"long message cut", Bad, 400, nil, strings.Repeat("é", 400), "", strings.Repeat("é", maxErrBytes/2)},
		{"class from caller", Refused, 429, nil, "no credits", "x", "no credits"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := httpError(tt.class, tt.status, tt.hdr, tt.msg, tt.notFound)
			if ce.Class != tt.class || ce.Status != tt.status || ce.Msg != tt.wantMsg {
				t.Errorf("got %s %d %q, want %s %d %q", ce.Class, ce.Status, ce.Msg, tt.class, tt.status, tt.wantMsg)
			}
			if want := retryAfter(tt.hdr); ce.RetryAfter != want {
				t.Errorf("retry after %v, want %v", ce.RetryAfter, want)
			}
		})
	}
	if ce := httpError(Busy, 429, hdr, "", ""); ce.RetryAfter != 3*time.Second {
		t.Errorf("retry after %v, want 3s", ce.RetryAfter)
	}
}

func TestStopNames(t *testing.T) {
	tests := []struct {
		name   string
		want   Stop
		wantOK bool
	}{
		{"end", StopEnd, true},
		{"max_tokens", StopMaxTokens, true},
		{"refusal", StopRefusal, true},
		{"end_turn", StopEnd, false},
		{"", StopEnd, false},
		{"length", StopEnd, false},
	}
	for _, tt := range tests {
		got, ok := stopFromName(tt.name)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("stopFromName(%q) = %v, %v; want %v, %v", tt.name, got, ok, tt.want, tt.wantOK)
		}
	}
	// Replay writes stopName and reads stopFromName: every Stop must survive the round trip.
	for _, s := range []Stop{StopEnd, StopMaxTokens, StopRefusal} {
		if got, ok := stopFromName(stopName(s)); !ok || got != s {
			t.Errorf("round trip of %v gave %v, %v", s, got, ok)
		}
	}
	if got := stopName(Stop(99)); got != "end" {
		t.Errorf("stopName(unknown) = %q, want end", got)
	}
}
