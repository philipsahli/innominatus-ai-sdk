package redact

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// promptFields are the key=value and JSON field names Prompt removes.
var promptFields = Fields("password", "passwd", "secret", "token", "api_key", "apikey", "client_secret", "private_key", "access_key")

// maxJSONDepth is how many JSON string levels Prompt looks into: a module
// interface (one level) holding a JSON default (two) holding a key (three).
const maxJSONDepth = 3

// Prompt redacts text that goes into, or is shown as, a model prompt. On
// top of Patterns it removes what only a prompt needs removed, because a
// prompt is made of text people wrote (instructions, configuration,
// descriptions, requests), not log lines:
//   - key=value and JSON fields that name a credential (promptFields);
//   - assignments whose name is a credential (HCL, YAML, .env:
//     admin_password = "...", password: ..., DB_PASSWORD=...);
//   - the default of a Terraform variable whose name is a credential or
//     that is sensitive = true;
//   - "the password is ..." in prose;
//   - long random-looking strings (bare AWS secret keys, base64 blobs).
//
// Each rule also runs on the content of JSON strings that hold escapes
// (a configuration document as JSON in the prompt), up to
// maxJSONDepth levels, so "\n" or "\u0026" in front of a secret does not
// hide it.
func Prompt(text string) string { return promptAt(text, 0) }

// PromptCount is Prompt and how many parts it removed. The count is what
// a model call records; the values are never kept.
func PromptCount(text string) (string, int) {
	out := Prompt(text)
	return out, strings.Count(out, Redacted) - strings.Count(text, Redacted)
}

func promptAt(text string, depth int) string {
	if text == "" {
		return text
	}
	text = promptFields(Patterns(text))
	text = tfVarDefaults(text)
	text = assignments(text)
	text = proseSecrets(text)
	text = opaqueValues(text)
	if depth < maxJSONDepth {
		text = jsonStrings(text, depth)
	}
	return text
}

// jsonString is a JSON string literal on one line.
var jsonString = regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"`)

// jsonStrings runs promptAt on the decoded content of each JSON string
// that holds an escape, and writes it back encoded when it changed. A
// string without escapes is the same text the outer pass already saw.
func jsonStrings(text string, depth int) string {
	return jsonString.ReplaceAllStringFunc(text, func(lit string) string {
		if !strings.Contains(lit, `\`) {
			return lit
		}
		var dec string
		if json.Unmarshal([]byte(lit), &dec) != nil {
			return lit
		}
		out := promptAt(dec, depth+1)
		if out == dec {
			return lit
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if enc.Encode(out) != nil {
			return lit
		}
		return strings.TrimSpace(buf.String())
	})
}

// assignment is name = value or name: value at the start of a line or
// after a separator. Groups: what comes before, the name, the separator
// and the value (double-quoted, single-quoted or bare).
var assignment = regexp.MustCompile(`(?m)(^|[\s{,;(\["'])((?:export[ \t]+)?[A-Za-z_][A-Za-z0-9_.-]{0,63})([ \t]*[=:][ \t]*)("(?:[^"\\\n]|\\.)*"|'[^'\n]*'|[^\s"'\\,;}\])]+)`)

// assignments removes the value of an assignment whose name names a
// credential (CredentialName) and that looks like a secret, not like a
// reference, a type or a word.
//
// After a match it keeps, the search goes on at the match's value, not
// after it: in "Credentials: token: x" or "login: (password=x)" the
// first match's value ("token:", "(password=x") holds the assignment
// that matters, and a search after the match would skip it.
func assignments(text string) string {
	var b strings.Builder
	last, pos := 0, 0
	for pos < len(text) {
		m := assignment.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		name := strings.TrimSpace(strings.TrimPrefix(text[m[4]:m[5]], "export"))
		val, quote := unquote(text[m[8]:m[9]])
		// A bare value ending in ':' is the next name ("Credentials:
		// password: x"), not a value.
		nameNext := quote == "" && strings.HasSuffix(val, ":")
		if nameNext || !CredentialName(name) || identifierName(name) || !looksSecret(val) {
			pos = m[8]
			continue
		}
		b.WriteString(text[last:m[8]])
		b.WriteString(quote + Redacted + quote)
		last, pos = m[9], m[9]
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// prose is "password is X", "the secret was X", "token of X".
var prose = regexp.MustCompile(`(?i)\b(password|passphrase|passwd|secret|token|api[ _-]?key|access[ _-]?key)(\s+(?:is|was|of)\s+)(\S+)`)

func proseSecrets(text string) string {
	return prose.ReplaceAllStringFunc(text, func(m string) string {
		g := prose.FindStringSubmatch(m)
		if !proseSecret(strings.TrimRight(g[3], ".,;:!)")) {
			return m
		}
		return g[1] + g[2] + Redacted
	})
}

// identifierWords end a credential-like name that holds no secret:
// secret_name, token_ttl, key_vault_id, password_policy.
var identifierWords = []string{"name", "names", "id", "ids", "arn", "ref", "path", "file", "version", "ttl", "length", "count",
	"type", "mode", "kind", "mount", "store", "provider", "manager", "rotation", "enabled", "required", "policy", "url", "endpoint", "max", "min", "size"}

func identifierName(name string) bool {
	words := nameWords(name)
	return len(words) > 1 && slices.Contains(identifierWords, words[len(words)-1])
}

func unquote(v string) (string, string) {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1], v[:1]
	}
	return v, ""
}

// referencePrefixes start a value that points at a secret instead of
// holding one: Terraform references and functions, template variables,
// ARNs, secret manager URIs.
var referencePrefixes = []string{"var.", "local.", "module.", "data.", "each.", "self.", "count.", "path.", "terraform.",
	"${", "$(", "{{", "<", "[", "(", "%", "arn:", "env(", "file(", "process.env", "os.environ", "secrets.", "vault:", "ref+", "projects/"}

// looksSecret reports whether an assignment's value is likely a secret: 8
// bytes or more, one word, not a reference, and not a plain word (a
// digit, mixed case, a symbol, or 16 bytes or more).
func looksSecret(v string) bool { return looksSecretAt(v, 8) }

// proseSecret reports whether X in "password is X" is a secret. The words
// in front name X as one, so a short value (4 to 7 bytes, such as Hunter2)
// goes too when it holds a digit or a symbol; a short word, even
// capitalised ("the secret is Vault", "token is Bearer"), stays.
func proseSecret(v string) bool {
	if len(v) >= 8 {
		return looksSecret(v)
	}
	return len(v) >= 4 && looksSecretAt(v, 4) && strings.ContainsFunc(v, func(r rune) bool { return !unicode.IsLetter(r) })
}

func looksSecretAt(v string, minLen int) bool {
	if len(v) < minLen || strings.Contains(v, Redacted) || strings.ContainsAny(v, " \t\n") || strings.Contains(v, "://") {
		return false
	}
	lower := strings.ToLower(v)
	for _, p := range referencePrefixes {
		if strings.HasPrefix(lower, p) {
			return false
		}
	}
	var digit, up, low, sym bool
	for _, r := range v {
		switch {
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			low = true
		default:
			sym = true
		}
	}
	return digit || (up && low) || sym || len(v) >= 16
}

// tfVariable opens a Terraform variable block; the body is found by
// counting braces.
var (
	tfVariable    = regexp.MustCompile(`\bvariable\s+"([A-Za-z0-9_-]+)"\s*\{`)
	tfSensitive   = regexp.MustCompile(`\bsensitive\s*=\s*true\b`)
	tfDefault     = regexp.MustCompile(`\bdefault\s*=\s*`)
	tfHeredocOpen = regexp.MustCompile(`^<<-?([A-Za-z_][A-Za-z0-9_]*)\n`)
)

// tfVarDefaults removes the string default of a variable whose name
// names a credential or that is sensitive = true, in .tf text pasted into
// a prompt.
func tfVarDefaults(text string) string {
	var b strings.Builder
	last := 0
	for _, loc := range tfVariable.FindAllStringSubmatchIndex(text, -1) {
		if loc[0] < last {
			continue
		}
		bodyStart := loc[1]
		bodyEnd := blockEnd(text, bodyStart)
		body := text[bodyStart:bodyEnd]
		name := text[loc[2]:loc[3]]
		if !CredentialName(name) && !tfSensitive.MatchString(body) {
			continue
		}
		d := tfDefault.FindStringIndex(body)
		if d == nil {
			continue
		}
		vs, ve := stringValue(body, d[1])
		if vs == ve {
			continue
		}
		b.WriteString(text[last : bodyStart+vs])
		b.WriteString(Redacted)
		last = bodyStart + ve
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// blockEnd is the index of the '}' that closes the block whose body
// starts at start, or the end of text.
func blockEnd(text string, start int) int {
	depth := 1
	inString := false
	for i := start; i < len(text); i++ {
		switch c := text[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case !inString && c == '{':
			depth++
		case !inString && c == '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(text)
}

// stringValue returns the bounds of the content of a quoted string or a
// heredoc starting at i in s; equal bounds when there is none.
func stringValue(s string, i int) (int, int) {
	rest := s[i:]
	switch {
	case strings.HasPrefix(rest, `"`):
		for j := 1; j < len(rest); j++ {
			switch rest[j] {
			case '\\':
				j++
			case '"':
				return i + 1, i + j
			case '\n':
				return i, i
			}
		}
	case strings.HasPrefix(rest, "<<"):
		m := tfHeredocOpen.FindStringSubmatchIndex(rest)
		if m == nil {
			return i, i
		}
		marker := rest[m[2]:m[3]]
		start := m[1]
		for j := start; j < len(rest); {
			nl := strings.IndexByte(rest[j:], '\n')
			line := rest[j:]
			if nl >= 0 {
				line = rest[j : j+nl]
			}
			if strings.TrimSpace(line) == marker {
				return i + start, i + j
			}
			if nl < 0 {
				break
			}
			j += nl + 1
		}
		return i + start, len(s)
	}
	return i, i
}

// opaque is a long run of base64 or base64url characters.
var opaque = regexp.MustCompile(`[A-Za-z0-9+/_-]{32,}={0,2}`)

// opaqueValues removes long strings that look random: upper and lower
// case letters and digits, without long lower-case words in them, and
// with enough entropy. Hex (commit SHAs, digests) and UUIDs have no upper
// case; identifiers and paths are mostly words.
func opaqueValues(text string) string {
	return opaque.ReplaceAllStringFunc(text, func(m string) string {
		if looksRandom(strings.TrimRight(m, "=")) {
			return Redacted
		}
		return m
	})
}

func looksRandom(s string) bool {
	var up, low, digit, wordy, run int
	counts := map[rune]int{}
	flush := func() {
		if run >= 4 {
			wordy += run
		}
		run = 0
	}
	for _, r := range s {
		counts[r]++
		switch {
		case r >= 'a' && r <= 'z':
			low++
			run++
			continue
		case r >= 'A' && r <= 'Z':
			up++
		case r >= '0' && r <= '9':
			digit++
		}
		flush()
	}
	flush()
	if up < 2 || low < 2 || digit < 2 || wordy*2 > len(s) {
		return false
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(len(s))
		h -= p * math.Log2(p)
	}
	return h >= 3.5
}
