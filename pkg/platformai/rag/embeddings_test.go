package rag

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// redirectTransport sends every request to target and records the URL the
// client asked for, because the endpoint URLs are fixed in the clients.
type redirectTransport struct {
	target    *url.URL
	base      http.RoundTripper
	requested []string
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requested = append(rt.requested, req.URL.String())
	out := req.Clone(req.Context())
	out.URL.Scheme = rt.target.Scheme
	out.URL.Host = rt.target.Host
	out.Host = rt.target.Host
	return rt.base.RoundTrip(out)
}

type embeddingClientCase struct {
	name         string
	wantURL      string
	defaultModel string
	build        func(model string, hc *http.Client) EmbeddingProvider
}

func embeddingClientCases() []embeddingClientCase {
	return []embeddingClientCase{
		{
			name:         "voyage",
			wantURL:      "https://api.voyageai.com/v1/embeddings",
			defaultModel: "voyage-3",
			build: func(model string, hc *http.Client) EmbeddingProvider {
				c := NewVoyageEmbeddingClient("test-key", model)
				c.httpClient = hc
				return c
			},
		},
		{
			name:         "openai",
			wantURL:      "https://api.openai.com/v1/embeddings",
			defaultModel: "text-embedding-3-small",
			build: func(model string, hc *http.Client) EmbeddingProvider {
				c := NewOpenAIEmbeddingClient("test-key", model)
				c.httpClient = hc
				return c
			},
		},
	}
}

// startServer returns a client whose requests reach handler instead of the network.
func startServer(t *testing.T, handler http.HandlerFunc) (*http.Client, *redirectTransport) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	rt := &redirectTransport{target: target, base: srv.Client().Transport}
	return &http.Client{Transport: rt}, rt
}

func TestEmbeddingClientsSendRequest(t *testing.T) {
	for _, cc := range embeddingClientCases() {
		for _, model := range []string{"", "custom-model"} {
			t.Run(cc.name+"/model="+model, func(t *testing.T) {
				wantModel := model
				if wantModel == "" {
					wantModel = cc.defaultModel
				}
				var gotBody struct {
					Input []string `json:"input"`
					Model string   `json:"model"`
				}
				hc, rt := startServer(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost {
						t.Errorf("method = %s, want POST", r.Method)
					}
					if r.URL.Path != "/v1/embeddings" {
						t.Errorf("path = %s, want /v1/embeddings", r.URL.Path)
					}
					if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
						t.Errorf("Authorization = %q, want Bearer test-key", got)
					}
					if got := r.Header.Get("Content-Type"); got != "application/json" {
						t.Errorf("Content-Type = %q, want application/json", got)
					}
					if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
						t.Errorf("decode body: %v", err)
					}
					_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[0.1,0.2]},{"index":1,"embedding":[0.3,0.4]}]}`)
				})

				got, err := cc.build(model, hc).GenerateEmbeddings(context.Background(), []string{"one", "two"})
				if err != nil {
					t.Fatalf("GenerateEmbeddings() error = %v", err)
				}
				if len(got) != 2 || got[0][0] != 0.1 || got[0][1] != 0.2 || got[1][0] != 0.3 || got[1][1] != 0.4 {
					t.Errorf("GenerateEmbeddings() = %v", got)
				}
				if strings.Join(gotBody.Input, ",") != "one,two" || gotBody.Model != wantModel {
					t.Errorf("body = %+v, want input [one two] model %s", gotBody, wantModel)
				}
				if len(rt.requested) != 1 || rt.requested[0] != cc.wantURL {
					t.Errorf("requested URLs = %v, want [%s]", rt.requested, cc.wantURL)
				}
			})
		}
	}
}

func TestEmbeddingClientsResponses(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		cancelCtx  bool
		wantErr    string
		wantSingle []float32
		wantBatch  int
	}{
		{name: "success", status: 200, body: `{"data":[{"index":0,"embedding":[1,2]}]}`, wantSingle: []float32{1, 2}, wantBatch: 1},
		{name: "non-200 with body", status: 429, body: `rate limited`, wantErr: "API request failed with status 429: rate limited"},
		{name: "malformed JSON", status: 200, body: `{"data":`, wantErr: "failed to decode response"},
		{name: "empty data", status: 200, body: `{"data":[]}`, wantErr: "got 0 embeddings for 1 inputs"},
		{name: "cancelled context", status: 200, body: `{"data":[]}`, cancelCtx: true, wantErr: "failed to send request"},
	}

	for _, cc := range embeddingClientCases() {
		for _, tt := range tests {
			t.Run(cc.name+"/"+tt.name, func(t *testing.T) {
				hc, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = io.WriteString(w, tt.body)
				})
				client := cc.build("", hc)
				ctx := context.Background()
				if tt.cancelCtx {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}

				single, err := client.GenerateEmbedding(ctx, "text")
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Errorf("GenerateEmbedding() error = %v, want containing %q", err, tt.wantErr)
					}
					if tt.cancelCtx && !errors.Is(err, context.Canceled) {
						t.Errorf("GenerateEmbedding() error = %v, want context.Canceled", err)
					}
				} else if err != nil || len(single) != len(tt.wantSingle) || single[0] != tt.wantSingle[0] {
					t.Errorf("GenerateEmbedding() = %v, %v, want %v", single, err, tt.wantSingle)
				}

				batch, err := client.GenerateEmbeddings(ctx, []string{"text"})
				if tt.wantErr != "" {
					if err == nil {
						t.Errorf("GenerateEmbeddings() error = nil, want error")
					}
					return
				}
				if err != nil || len(batch) != tt.wantBatch {
					t.Errorf("GenerateEmbeddings() = %v, %v, want %d embeddings", batch, err, tt.wantBatch)
				}
			})
		}
	}
}

func TestEmbeddingClientsPlaceByIndex(t *testing.T) {
	for _, cc := range embeddingClientCases() {
		t.Run(cc.name, func(t *testing.T) {
			hc, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"data":[{"index":2,"embedding":[3]},{"index":1,"embedding":[2]},{"index":0,"embedding":[1]}]}`)
			})
			got, err := cc.build("", hc).GenerateEmbeddings(context.Background(), []string{"first", "second", "third"})
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 3 || got[0][0] != 1 || got[1][0] != 2 || got[2][0] != 3 {
				t.Errorf("GenerateEmbeddings() = %v, want [[1] [2] [3]] (by index)", got)
			}
		})
	}
}

func TestEmbeddingClientsRejectMalformedIndexes(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "missing index", body: `{"data":[{"index":0,"embedding":[1]},{"embedding":[2]}]}`, wantErr: "missing index"},
		{name: "duplicate index", body: `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`, wantErr: "duplicate index 0"},
		{name: "index out of range", body: `{"data":[{"index":0,"embedding":[1]},{"index":2,"embedding":[2]}]}`, wantErr: "index 2 out of range"},
		{name: "negative index", body: `{"data":[{"index":-1,"embedding":[1]},{"index":0,"embedding":[2]}]}`, wantErr: "index -1 out of range"},
		{name: "fewer than inputs", body: `{"data":[{"index":0,"embedding":[1]}]}`, wantErr: "got 1 embeddings for 2 inputs"},
		{name: "more than inputs", body: `{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[2]},{"index":2,"embedding":[3]}]}`, wantErr: "got 3 embeddings for 2 inputs"},
	}
	for _, cc := range embeddingClientCases() {
		for _, tt := range tests {
			t.Run(cc.name+"/"+tt.name, func(t *testing.T) {
				hc, _ := startServer(t, func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, tt.body)
				})
				got, err := cc.build("", hc).GenerateEmbeddings(context.Background(), []string{"first", "second"})
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("GenerateEmbeddings() = %v, %v, want error containing %q", got, err, tt.wantErr)
				}
			})
		}
	}
}

func TestNewEmbeddingProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		want     string
		wantErr  bool
	}{
		{name: "voyageai", provider: "voyageai", want: "voyage"},
		{name: "voyage alias", provider: "voyage", want: "voyage"},
		{name: "openai", provider: "openai", want: "openai"},
		{name: "unknown", provider: "cohere", wantErr: true},
		{name: "empty", provider: "", wantErr: true},
		// Anthropic has no embeddings API.
		{name: "anthropic", provider: "anthropic", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewEmbeddingProvider(Config{EmbeddingProvider: tt.provider, APIKey: "test-key", Model: "m"})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "unsupported embedding provider: "+tt.provider) {
					t.Fatalf("NewEmbeddingProvider() error = %v, want unsupported provider", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewEmbeddingProvider() error = %v", err)
			}
			switch c := p.(type) {
			case *VoyageEmbeddingClient:
				if tt.want != "voyage" || c.apiKey != "test-key" || c.model != "m" || c.httpClient == nil {
					t.Errorf("got %+v, want %s client with key and model", c, tt.want)
				}
			case *OpenAIEmbeddingClient:
				if tt.want != "openai" || c.apiKey != "test-key" || c.model != "m" || c.httpClient == nil {
					t.Errorf("got %+v, want %s client with key and model", c, tt.want)
				}
			default:
				t.Errorf("got %T, want %s client", p, tt.want)
			}
		})
	}
}
