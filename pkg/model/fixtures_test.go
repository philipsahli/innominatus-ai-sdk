package model_test

import "encoding/json"

// Fixtures shared by the backend tests. The keys must never show up in an
// error, a log line or a recording.
const (
	apiKey   = "sk-test-key-must-not-leak-4711"
	claudeM  = "claude-sonnet-5-5"
	ollamaM  = "llama3.1:8b"
	validFix = `{"cpu":"250m","replicas":2,"reason":"Low traffic."}`
)

// sizingSchema is the JSON schema of a small sizing answer.
var sizingSchema = json.RawMessage(`{"additionalProperties":false,"properties":{"cpu":{"description":"Kubernetes CPU request","type":"string"},"reason":{"type":"string"},"replicas":{"maximum":10,"minimum":1,"type":"integer"}},"required":["cpu","replicas","reason"],"type":"object"}`)
