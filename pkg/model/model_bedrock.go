package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// ProviderBedrock is Amazon Bedrock Runtime with an Anthropic model.
const ProviderBedrock = "bedrock"

// bedrockAnthropicVersion is the Messages API version Bedrock expects in
// the body of an Anthropic model.
const bedrockAnthropicVersion = "bedrock-2023-05-31"

// bedrockTool is the one tool the request forces: its input schema is the
// answer schema, so the tool input is the structured answer.
const bedrockToolName = "answer"

type bedrockModel struct {
	http     *http.Client
	creds    aws.CredentialsProvider
	signer   *v4.Signer
	endpoint string
	region   string
	model    string
	now      func() time.Time
}

// NewBedrock returns the Bedrock backend: one SigV4-signed POST
// {endpoint}/model/{model}/invoke per call with an Anthropic Messages body.
// The JSON schema is sent as a forced tool (tool_choice), because forced
// tool use is the structured-output path Bedrock documents for Anthropic
// models. endpoint is https://bedrock-runtime.{region}.amazonaws.com
// unless set (a VPC endpoint, or a test server). model is a model ID or an
// inference profile ID or ARN.
func NewBedrock(creds aws.CredentialsProvider, region, endpoint, model string) Model {
	if endpoint == "" {
		endpoint = "https://bedrock-runtime." + region + ".amazonaws.com"
	}
	return &bedrockModel{
		http: newHTTPClient(), creds: creds, signer: v4.NewSigner(),
		endpoint: strings.TrimRight(endpoint, "/"), region: region, model: model, now: time.Now,
	}
}

func (m *bedrockModel) Name() (string, string) { return ProviderBedrock, m.model }

type bedrockContent struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type bedrockMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type bedrockTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type bedrockRequest struct {
	AnthropicVersion string           `json:"anthropic_version"`
	MaxTokens        int              `json:"max_tokens"`
	System           string           `json:"system,omitempty"`
	Messages         []bedrockMessage `json:"messages"`
	Tools            []bedrockTool    `json:"tools,omitempty"`
	ToolChoice       *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"tool_choice,omitempty"`
}

type bedrockResponse struct {
	Content    []bedrockContent `json:"content"`
	StopReason string           `json:"stop_reason"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

func (m *bedrockModel) Call(ctx context.Context, req Request) (Reply, error) {
	in := bedrockRequest{AnthropicVersion: bedrockAnthropicVersion, MaxTokens: req.MaxTokens, System: req.System}
	if in.MaxTokens <= 0 {
		in.MaxTokens = DefaultMaxTokens
	}
	for _, t := range req.Turns {
		in.Messages = append(in.Messages, bedrockMessage{Role: t.Role, Content: t.Text})
	}
	if len(req.Schema) > 0 {
		in.Tools = []bedrockTool{{Name: bedrockToolName, Description: "Return the answer in this shape.", InputSchema: req.Schema}}
		in.ToolChoice = &struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}{Type: "tool", Name: bedrockToolName}
	}
	body, err := json.Marshal(in)
	if err != nil {
		return Reply{}, &CallError{Class: Bad, Msg: "encode request: " + err.Error()}
	}
	// Escape ':' too (url.PathEscape keeps it), as the AWS SDK does for
	// model and inference profile IDs; the signer encodes the escaped path
	// once more, which is what SigV4 expects outside S3.
	modelPath := strings.ReplaceAll(url.PathEscape(m.model), ":", "%3A")
	data, status, hdr, ce := postJSON(ctx, m.http, m.endpoint+"/model/"+modelPath+"/invoke", body, func(httpReq *http.Request) *CallError {
		httpReq.Header.Set("Accept", "application/json")
		creds, err := m.creds.Retrieve(ctx)
		if err != nil {
			// No credentials in the chain, or an expired SSO session: the
			// operator must fix it, a retry would not.
			return &CallError{Class: Refused, Msg: "AWS credentials: " + cut(err.Error(), maxErrBytes)}
		}
		sum := sha256.Sum256(body)
		if err := m.signer.SignHTTP(ctx, creds, httpReq, hex.EncodeToString(sum[:]), "bedrock", m.region, m.now()); err != nil {
			return &CallError{Class: Bad, Msg: "sign request: " + cut(err.Error(), maxErrBytes)}
		}
		return nil
	})
	if ce != nil {
		return Reply{}, ce
	}
	if status != http.StatusOK {
		return Reply{}, bedrockError(status, hdr, data, m.model)
	}
	var out bedrockResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return Reply{}, &CallError{Class: Bad, Status: status, Msg: "the answer is not Anthropic Messages JSON"}
	}
	rep := Reply{InputTokens: out.Usage.InputTokens, OutputTokens: out.Usage.OutputTokens}
	var text []string
	for _, c := range out.Content {
		switch c.Type {
		case "tool_use":
			// The forced tool's input is the answer; text blocks around it
			// are the model's commentary.
			rep.Text = string(c.Input)
		case "text":
			text = append(text, c.Text)
		}
	}
	if rep.Text == "" {
		rep.Text = strings.Join(text, "")
	}
	// end_turn and the other normal ends are not in the table: StopEnd.
	rep.Stop, _ = stopFromName(out.StopReason)
	return rep, nil
}

// bedrockError maps a Bedrock error. Throttling (429), a model not ready
// (424 ModelNotReadyException) and 5xx are busy; access denied is refused.
func bedrockError(status int, hdr http.Header, data []byte, model string) *CallError {
	class := classOf(status)
	if status == http.StatusFailedDependency {
		class = Busy
	}
	var body struct {
		Message string `json:"message"`
	}
	var msg string
	if json.Unmarshal(data, &body) == nil {
		msg = body.Message
	}
	return httpError(class, status, hdr, msg, fmt.Sprintf("model %s is not available in this region or account", model))
}
