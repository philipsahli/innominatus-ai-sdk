package model

// ToolCapable is an optional interface of a Model: SupportsTools reports
// whether the backend can send a request with tools or tool blocks.
// Instrument asks it after the allowlist and before redaction and the
// audit start row, so a tool request to a backend without tools is refused
// without a trail row (outcome tools_unsupported). A Model that does not
// implement it is taken to support tools: the test fakes and the chat's
// own fakes run tool loops.
//
// The methods sit here, not in the model_*.go files, so a backend file
// holds only its wire format.
type ToolCapable interface {
	SupportsTools() bool
}

// SupportsTools reports whether m can run a request with tools: false only
// when m (or the backend under its wrappers) says so.
func SupportsTools(m Model) bool {
	if tc, ok := m.(ToolCapable); ok {
		return tc.SupportsTools()
	}
	return true
}

func (m *anthropicModel) SupportsTools() bool { return true }
func (m *openAIModel) SupportsTools() bool    { return true }
func (m *ollamaModel) SupportsTools() bool    { return true }

// Bedrock and copilot send text turns only.
func (m *bedrockModel) SupportsTools() bool { return false }
func (m *copilotModel) SupportsTools() bool { return false }

// Replay keeps refusing tool requests: recordings hold text turns only,
// and a tool loop's answer depends on tool output a recording cannot know.
func (r *replay) SupportsTools() bool { return false }

// The wrappers embed Model, which does not promote SupportsTools, so each
// forwards it to the backend it wraps.

func (g *guarded) SupportsTools() bool      { return SupportsTools(g.Model) }
func (i *instrumented) SupportsTools() bool { return SupportsTools(i.Model) }
func (r *redacting) SupportsTools() bool    { return SupportsTools(r.Model) }

// The recorder answers for its live backend: an anthropic, openai or
// ollama recorder passes tool requests through unrecorded (recorder.Call).
func (r *recorder) SupportsTools() bool { return SupportsTools(r.Model) }
