// Package redact removes credentials from text before it is written to a
// log file or sent to a model: the exact values a process knows to be
// secret, in the forms they take inside JSON and URLs, and fixed patterns
// of credentials it does not know (GitHub tokens, Bearer and Basic
// headers, PEM blocks, passwords in URLs, credential query parameters,
// JWTs, AWS access key IDs, Slack tokens and webhooks, Anthropic and
// OpenAI keys, DigitalOcean, GitLab, npm, Google, Vault and Stripe tokens,
// connection-string keys, and the values of JSON fields whose names look
// secret: SensitiveKey or CredentialName).
//
// Prompt (prompt.go) adds the rules only model prompts get: assignments
// and Terraform variable defaults named as credentials, "the password is
// ..." in prose, and long random-looking strings.
//
// PatternSources lists the fixed patterns, so a program that keeps its own
// copy of some of them can test that the copies agree.
package redact

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Redacted replaces every removed part of a text.
const Redacted = "[redacted]"

// MinValueLen is the shortest known value Text removes. A variable whose
// name only looks secret (AUTH_ENABLED=true) would otherwise remove "true"
// from every line.
const MinValueLen = 8

// basePatterns are the token and header patterns. The PEM pattern also
// catches a block cut before its END line.
var basePatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----(?s:.*?)(-----END [A-Z0-9 ]+-----|\z)`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9\-._~+/]+=*`),
}

// basicAuth is "Basic <base64>"; removed only when the base64 decodes to
// text with a colon (user:password), so "basic settings" stays.
var basicAuth = regexp.MustCompile(`(?i)\bbasic\s+([A-Za-z0-9+/]{4,}={0,2})`)

// userInfo is the user:password part of a URL ("https://u:p@host"). It
// needs a user name; a URL without one ("redis://:p@host") or with an '@'
// in the password ("u:a@b@host") would keep its password.
var userInfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/\s:@]+:[^/\s@]+@`)

// userInfoAny is userInfo for those two shapes: an empty user name, and a
// password that runs to the last '@' before the host.
var userInfoAny = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/\s:@]*:[^/\s]+@`)

// The patterns below catch what a log line or a prompt often holds and a
// short error body rarely does: URLs and JSON documents.
var (
	// credentialParam is a credential in a query string: ?token=,
	// &access_token=, &api_key=, a presigned URL's X-Amz-Signature.
	// A JSON encoder with HTML escaping (json.Marshal) writes '&' as
	// \u0026, as a prompt's module interface or catalog holds it.
	credentialParam = regexp.MustCompile(`(?i)((?:[?&]|\\u0026)(?:[a-z0-9_.-]*token|api_key|apikey|key|sig|signature|secret|password|pwd|x-amz-signature|x-amz-credential)=)[^&\s"'<>]+`)
	// jwt is a JSON Web Token: base64url header and payload, both JSON.
	jwt = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	// awsKeyID is an AWS access key ID (long-term AKIA, temporary ASIA).
	awsKeyID = regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`)
	// slackToken is a Slack bot, user, app or refresh token.
	slackToken = regexp.MustCompile(`\bxox[abeoprs]-[A-Za-z0-9-]{10,}`)
	// prefixedTokens are service tokens with a fixed prefix: DigitalOcean
	// (personal, OAuth, refresh), Slack app-level, GitLab, npm, Google API
	// keys, HashiCorp Vault, Stripe live keys.
	prefixedTokens = []*regexp.Regexp{
		regexp.MustCompile(`\bdo[opr]_v1_[a-f0-9]{64}\b`),
		regexp.MustCompile(`\bxapp-\d-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`),
		regexp.MustCompile(`\bhv[sbr]\.[A-Za-z0-9_-]{24,}`),
		regexp.MustCompile(`\b[rs]k_live_[A-Za-z0-9]{20,}`),
	}
	// skKey is an Anthropic (sk-ant-) or OpenAI (sk-, sk-proj-) API key.
	// Only a match with a digit counts: "sk-" before a long kebab-case
	// name is not a key.
	skKey = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`)
	// slackWebhook is a Slack incoming webhook; the path is the secret.
	slackWebhook = regexp.MustCompile(`(https://hooks\.slack\.com/)(?:services|workflows|triggers)/[A-Za-z0-9/_-]+`)
	// connStringKey is a secret in a ';'-separated connection string:
	// Azure Storage and Service Bus keys, a SQL Server password.
	// SharedAccessKeyName names the key and stays.
	connStringKey = regexp.MustCompile(`(?i)((?:^|[;"'\s])(?:AccountKey|SharedAccessKey|SharedAccessSignature|Password|Pwd|ClientSecret)=)[^;"'\s]+`)
	// jsonField is the start of a JSON field, "key": followed by a string
	// or a number, also inside a JSON string (\"key\":\"value\", escaped
	// once or more, as a log line that quotes a request body holds it).
	// Groups: the backslashes before the key's quotes and the value's
	// opening quote, the key, the separator, a number's first character.
	// RE2 has no back references, so jsonFields checks that the
	// backslash runs agree and finds the value's end itself.
	jsonField = regexp.MustCompile(`(\\*)"([^"\\]{1,64})(\\*)"(\s*:\s*)(?:(\\*)"|(-?[0-9]))`)
)

// pemBegin and pemEnd find the edges of a PEM block that spans lines, for
// callers that redact one line at a time (a line-based log writer).
var (
	pemBegin = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----`)
	pemEnd   = regexp.MustCompile(`-----END [A-Z0-9 ]+-----`)
)

// Redactor holds the secret values one process knows. It is safe for
// concurrent use: values are added while writers already run (a key file is
// read after the log file is open).
type Redactor struct {
	mu     sync.RWMutex
	values []string // with their encodings, longest first
}

// Add records values to remove: each value of MinValueLen bytes or more
// (after trimming spaces), with its JSON-escaped and URL-encoded forms.
func (r *Redactor) Add(values ...string) {
	var add []string
	for _, v := range values {
		if v = strings.TrimSpace(v); len(v) >= MinValueLen {
			add = append(add, v)
		}
	}
	if len(add) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range withEncodings(add) {
		if !slices.Contains(r.values, v) {
			r.values = append(r.values, v)
		}
	}
	// Longer first, so a secret that contains a shorter one goes whole.
	slices.SortStableFunc(r.values, func(a, b string) int { return len(b) - len(a) })
}

// Text returns text without the known values and the patterns.
func (r *Redactor) Text(text string) string {
	if text == "" {
		return text
	}
	r.mu.RLock()
	for _, v := range r.values {
		text = strings.ReplaceAll(text, v, Redacted)
	}
	r.mu.RUnlock()
	return Patterns(text)
}

// Patterns removes what the fixed patterns match, without known values.
func Patterns(text string) string {
	for _, re := range basePatterns {
		text = re.ReplaceAllString(text, Redacted)
	}
	text = basicAuth.ReplaceAllStringFunc(text, func(m string) string {
		enc := basicAuth.FindStringSubmatch(m)[1]
		if !isCredential(enc) {
			return m
		}
		return m[:len(m)-len(enc)] + Redacted
	})
	// userInfoAny first: userInfo would stop at the first '@' of a password.
	text = userInfoAny.ReplaceAllString(text, "${1}"+Redacted+"@")
	text = userInfo.ReplaceAllString(text, "${1}"+Redacted+"@")
	text = credentialParam.ReplaceAllString(text, "${1}"+Redacted)
	text = slackWebhook.ReplaceAllString(text, "${1}"+Redacted)
	text = connStringKey.ReplaceAllString(text, "${1}"+Redacted)
	for _, re := range append([]*regexp.Regexp{jwt, awsKeyID, slackToken}, prefixedTokens...) {
		text = re.ReplaceAllString(text, Redacted)
	}
	text = skKey.ReplaceAllStringFunc(text, func(m string) string {
		if strings.ContainsAny(m[3:], "0123456789") {
			return Redacted
		}
		return m
	})
	return jsonFields(text)
}

// jsonFields removes the values of JSON fields whose key looks secret
// (sensitiveField): string values at any escape level, and number values of
// keys that do not name a count or a limit.
func jsonFields(text string) string {
	var b strings.Builder
	last, pos := 0, 0
	for pos < len(text) {
		m := jsonField.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		pre := m[3] - m[2]
		key := text[m[4]:m[5]]
		level, ok := escapeLevel(pre)
		// Go on after the key's opening quote when this is not a field.
		next := m[3] + 1
		if !ok || m[7]-m[6] != pre || !sensitiveField(key) {
			pos = next
			continue
		}
		var start, end int
		if m[10] >= 0 { // string value
			if m[11]-m[10] != pre {
				pos = next
				continue
			}
			start = m[1]
			end = stringEnd(text, start, level)
		} else { // number value
			if isCount(key) {
				pos = next
				continue
			}
			start = m[12]
			end = start + 1
			for end < len(text) && strings.IndexByte("0123456789.eE+-", text[end]) >= 0 {
				end++
			}
		}
		b.WriteString(text[last:start])
		b.WriteString(Redacted)
		last, pos = end, end
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

// sensitiveField: a JSON field's value is removed when either rule matches.
// SensitiveKey is the broad rule (any name holding key, auth, cert,
// private, ...): an API that masks such fields must not have them leak
// through a log or prompt. CredentialName adds the names it does not catch
// (pwd, passwd). The price: identifiers such as "list_key" and
// "auth_password_env" are redacted as well.
func sensitiveField(key string) bool {
	return SensitiveKey(key) || CredentialName(key)
}

// sensitiveParts are the substrings that make a field name sensitive.
var sensitiveParts = []string{
	"password", "secret", "token", "key", "credential", "api_key", "apikey",
	"auth", "private", "bearer", "access_token", "refresh_token",
	"client_secret", "passphrase", "cert", "certificate",
}

// SensitiveKey reports whether the lower-cased name holds one of
// SensitiveParts. It is broader than CredentialName: "public_key" and
// "cert" match.
func SensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, p := range sensitiveParts {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// SensitiveParts returns a copy of the substrings SensitiveKey looks for, so
// a program that masks fields with its own copy of the list can test that
// the two agree.
func SensitiveParts() []string { return slices.Clone(sensitiveParts) }

// PatternSources returns the source of every fixed pattern Text and
// Patterns apply, so a program that keeps its own copy of some of them can
// test that each copy is still here.
func PatternSources() []string {
	out := []string{basicAuth.String(), userInfo.String(), userInfoAny.String()}
	for _, re := range basePatterns {
		out = append(out, re.String())
	}
	out = append(out, credentialParam.String(), jwt.String(), awsKeyID.String(), slackToken.String())
	for _, re := range prefixedTokens {
		out = append(out, re.String())
	}
	return append(out, skKey.String(), slackWebhook.String(), connStringKey.String(), jsonField.String())
}

// credentialWords name a credential wherever they stand in a field name,
// alone or as the end of a joined word (accesstoken, dbpassword).
var credentialWords = []string{"password", "passwd", "passphrase", "secret", "secrets", "token", "tokens", "credential", "credentials", "authorization", "bearer"}

// keyQualifiers make a following "key" a credential (api_key, x-api-key,
// AccessKey, privatekey). Without one, "key" is an identifier: list_key,
// sort_key, primary_key, partition_key, key_name, public_key.
var keyQualifiers = []string{"api", "access", "secret", "private", "signing", "encryption", "master", "client", "auth", "ssh", "deploy", "account", "storage", "shared", "session", "license"}

// CredentialName reports whether a field name names a credential, so its
// value is removed. Names are split into words at '_', '-', '.', other
// non-letters and camelCase. A name is a credential when
//   - a word is or ends with a credentialWords entry (client_secret,
//     access_token, refreshToken, x-auth-token, db_password);
//   - a "key" word follows a keyQualifiers word, or a word is such a
//     qualifier joined with "key" (api_key, apikey, secret_key);
//   - "auth" is the last word or comes before "header" (auth, basic_auth,
//     auth_header), not auth_mode or auth_type; "author" is another word;
//   - the whole name is "key" or "pwd".
//
// A name ending in "_env" (EnvName) is never one: its value names an
// environment variable, the secret stays there. JSON field redaction
// (sensitiveField) also applies SensitiveKey, so there such
// names are still redacted. The value patterns (GitHub tokens, JWTs, AWS
// key IDs, PEM blocks) apply to every value whatever its name.
func CredentialName(name string) bool {
	if EnvName(name) {
		return false
	}
	words := nameWords(name)
	if len(words) == 1 && (words[0] == "key" || words[0] == "pwd") {
		return true
	}
	for i, w := range words {
		for _, c := range credentialWords {
			if strings.HasSuffix(w, c) {
				return true
			}
		}
		if w == "key" || w == "keys" {
			if i > 0 && slices.Contains(keyQualifiers, words[i-1]) {
				return true
			}
		} else if q, ok := strings.CutSuffix(w, "key"); ok && slices.Contains(keyQualifiers, q) {
			return true
		}
		if w == "auth" && (i == len(words)-1 || words[i+1] == "header") {
			return true
		}
	}
	return false
}

// EnvName reports whether a field or parameter name holds the name of an
// environment variable (auth_password_env, API_KEY_ENV), not a secret.
func EnvName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), "_env")
}

// nameWords splits a field name into lower-case words at non-letters and
// at a lower-to-upper case change: "x-api-key" and "xApiKey" both give
// x, api, key.
func nameWords(name string) []string {
	var words []string
	var cur []rune
	prevLower := false
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for _, r := range name {
		if !unicode.IsLetter(r) {
			flush()
			prevLower = false
			continue
		}
		if unicode.IsUpper(r) && prevLower {
			flush()
		}
		cur = append(cur, r)
		prevLower = unicode.IsLower(r)
	}
	flush()
	return words
}

// escapeLevel is how often a JSON quote preceded by run backslashes was
// escaped: a quote escaped n times has 2^n-1 backslashes before it.
func escapeLevel(run int) (int, bool) {
	for n := 0; n <= 5; n++ {
		if run == 1<<n-1 {
			return n, true
		}
	}
	return 0, false
}

// stringEnd is where the body of a JSON string at escape level n, starting
// at start, ends: before the backslashes of its closing quote. Escaping a
// level doubles each backslash and adds one before each quote, so the
// closing quote's run is 2^n-1 modulo 2^(n+1); a quote inside the value
// has a longer run. Without a closing quote the value runs to the end.
func stringEnd(text string, start, n int) int {
	mod, want := 1<<(n+1), 1<<n-1
	for i := start; i < len(text); i++ {
		if text[i] != '"' {
			continue
		}
		run := 0
		for j := i - 1; j >= start && text[j] == '\\'; j-- {
			run++
		}
		if run%mod == want {
			return i - want
		}
	}
	return len(text)
}

// isCount reports whether a key that looks secret names a number that is
// not one: a count, a limit, a size or a time ("max_tokens", "input_tokens",
// "token_ttl").
func isCount(key string) bool {
	k := strings.ToLower(key)
	for _, w := range []string{"count", "max", "min", "limit", "num", "total", "size", "len", "tokens", "ttl", "expires", "_at", "seconds", "_ms", "version", "port", "index", "id"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// Fields returns a function that removes the values of the named fields,
// in a JSON line ("otp":"123456") and in zerolog's console format
// (otp=123456): fields that are not secret by name but are secret in one
// place, the one-time codes and links the console mail provider logs.
func Fields(names ...string) func(string) string {
	var res []*regexp.Regexp
	for _, n := range names {
		q := regexp.QuoteMeta(n)
		res = append(res,
			regexp.MustCompile(`("`+q+`"\s*:\s*)"(?:[^"\\]|\\.)*"`),
			regexp.MustCompile(`(^|\s)(`+q+`=)\S+`))
	}
	return func(text string) string {
		for i := 0; i < len(res); i += 2 {
			text = res[i].ReplaceAllString(text, `${1}"`+Redacted+`"`)
			text = res[i+1].ReplaceAllString(text, "${1}${2}"+Redacted)
		}
		return text
	}
}

// OpensPEM reports whether line starts a PEM block that it does not end:
// the lines after it, up to the END line, are part of the key.
func OpensPEM(line string) bool {
	locs := pemBegin.FindAllStringIndex(line, -1)
	if len(locs) == 0 {
		return false
	}
	return !pemEnd.MatchString(line[locs[len(locs)-1][1]:])
}

// ClosesPEM returns the part of line after a PEM END marker, and whether
// it has one.
func ClosesPEM(line string) (rest string, ok bool) {
	loc := pemEnd.FindStringIndex(line)
	if loc == nil {
		return "", false
	}
	return line[loc[1]:], true
}

// withEncodings returns values with, for each, the forms it takes inside a
// JSON string (with and without HTML escaping) and URL-encoded (query and
// path), where those differ from it. A log line is often JSON (zerolog) or
// holds a URL.
func withEncodings(values []string) []string {
	out := slices.Clone(values)
	for _, v := range values {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		forms := []string{url.QueryEscape(v), url.PathEscape(v)}
		if enc.Encode(v) == nil {
			forms = append(forms, jsonInner(buf.Bytes()))
		}
		if b, err := json.Marshal(v); err == nil {
			forms = append(forms, jsonInner(b))
		}
		for _, f := range forms {
			if f != "" && f != v && !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// jsonInner is an encoded JSON string without its quotes and newline.
func jsonInner(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) < 2 {
		return ""
	}
	return s[1 : len(s)-1]
}

// isCredential reports whether enc is base64 of UTF-8 text holding a colon.
func isCredential(enc string) bool {
	dec, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		dec, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(enc, "="))
	}
	return err == nil && utf8.Valid(dec) && bytes.ContainsRune(dec, ':')
}
