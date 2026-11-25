package redact

import (
	"regexp"
	"testing"
)

func TestRedactor_Text(t *testing.T) {
	r := &Redactor{}
	r.Add("s3cret+value/with&chars", "0123456789abcdef", "short", "  padded-secret-value  ", `pa"ss\word-1`)
	tests := []struct {
		name, in, want string
	}{
		{"raw", "key=0123456789abcdef end", "key=[redacted] end"},
		{"json escaped", `{"v":"pa\"ss\\word-1"}`, `{"v":"[redacted]"}`},
		{"json with html escape", `{"v":"s3cret+value/with\u0026chars"}`, `{"v":"[redacted]"}`},
		{"query escaped", "u?p=s3cret%2Bvalue%2Fwith%26chars", "u?p=[redacted]"},
		{"path escaped", "/x/s3cret+value%2Fwith&chars/y", "/x/[redacted]/y"},
		{"trimmed value", "padded-secret-value!", "[redacted]!"},
		{"too short is kept", "a short word", "a short word"},
		{"twice", "0123456789abcdef 0123456789abcdef", "[redacted] [redacted]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Text(tt.in); got != tt.want {
				t.Errorf("Text(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactor_LongerValueFirst(t *testing.T) {
	r := &Redactor{}
	r.Add("abcdefgh", "abcdefgh-and-more")
	if got := r.Text("x abcdefgh-and-more y"); got != "x [redacted] y" {
		t.Errorf("got %q", got)
	}
}

func TestPatterns_Credentials(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"gh token", "token ghp_abcdefghijklmnopqrstuvwxyz0123 used", "token [redacted] used"},
		{"fine-grained pat", "github_pat_11ABCDEFG0123456789_abcdefghij", "[redacted]"},
		{"bearer", "Authorization: Bearer abc.def-ghi", "Authorization: [redacted]"},
		{"bearer json", `{"h":"Bearer abcdef=="}`, `{"h":"[redacted]"}`},
		{"basic credential", "Authorization: Basic dXNlcjpwYXNz", "Authorization: Basic [redacted]"},
		{"basic word", "basic settings apply", "basic settings apply"},
		{"url password", "dial postgres://app:hunter2@db:5432/x", "dial postgres://[redacted]@db:5432/x"},
		{"url without password", "https://example.com/a@b", "https://example.com/a@b"},
		{"token query", "GET /cb?code=1&token=abc123&x=2", "GET /cb?code=1&token=[redacted]&x=2"},
		{"access_token query", "u?access_token=zzz", "u?access_token=[redacted]"},
		{"api_key query", "GET /x?API_KEY=abc&page=2", "GET /x?API_KEY=[redacted]&page=2"},
		{"presigned url", "s3/o?X-Amz-Credential=AK%2F2026&X-Amz-Signature=deadbeef ok", "s3/o?X-Amz-Credential=[redacted]&X-Amz-Signature=[redacted] ok"},
		{"sig and password", "?sig=s1&password=p w", "?sig=[redacted]&password=[redacted] w"},
		{"other query kept", "?page=2&keyword=x", "?page=2&keyword=x"},
		{"jwt", "cookie eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abc-_def end", "cookie [redacted] end"},
		{"aws key id", "id AKIAABCDEFGHIJKLMNOP used", "id [redacted] used"},
		{"aws temporary key id", "ASIAABCDEFGHIJKLMNOP", "[redacted]"},
		{"slack token", "xoxb-1234567890-abcdefghij", "[redacted]"},
		{"json secret field", `{"client_secret":"s3","name":"x"}`, `{"client_secret":"[redacted]","name":"x"}`},
		{"json escaped secret field", `{"password" : "a\"b"}`, `{"password" : "[redacted]"}`},
		{"json plain field kept", `{"stack":"demo","phase":"active"}`, `{"stack":"demo","phase":"active"}`},
		{"pem one line", `k="-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----" ok`, `k="[redacted]" ok`},
		{"pem cut", "x -----BEGIN PRIVATE KEY-----MIIE", "x [redacted]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Patterns(tt.in); got != tt.want {
				t.Errorf("Patterns(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPatterns_JSONFieldsAtAnyEscapeLevel covers a JSON document quoted in
// a JSON string, once or twice, as a log line holding a request body has it.
func TestPatterns_JSONFieldsAtAnyEscapeLevel(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"escaped once", `{"body":"{\"password\":\"hunter22\"}"}`, `{"body":"{\"password\":\"[redacted]\"}"}`},
		{"escaped twice", `{"m":"{\"body\":\"{\\\"token\\\":\\\"abc123\\\"}\"}"}`, `{"m":"{\"body\":\"{\\\"token\\\":\\\"[redacted]\\\"}\"}"}`},
		{"escaped quote in escaped value", `"{\"secret\":\"a\\\"b\",\"n\":\"x\"}"`, `"{\"secret\":\"[redacted]\",\"n\":\"x\"}"`},
		{"escaped with spaces", `"{\"api_key\" : \"k1\"}"`, `"{\"api_key\" : \"[redacted]\"}"`},
		{"escaped plain field kept", `{"body":"{\"user\":\"ana\",\"stack\":\"demo\"}"}`, `{"body":"{\"user\":\"ana\",\"stack\":\"demo\"}"}`},
		{"number of a secret key", `{"password":123456,"x":1}`, `{"password":[redacted],"x":1}`},
		{"escaped number", `"{\"secret\":-42.5e3}"`, `"{\"secret\":[redacted]}"`},
		{"counts kept", `{"max_tokens":4096,"input_tokens":12,"token_count":3,"key_id":7}`, `{"max_tokens":4096,"input_tokens":12,"token_count":3,"key_id":7}`},
		{"number of a plain key kept", `{"pin":123456}`, `{"pin":123456}`},
		{"bool kept", `{"auth":true}`, `{"auth":true}`},
		{"escaped backslash ends value", `{"a":"x\\","password":"p1"}`, `{"a":"x\\","password":"[redacted]"}`},
		{"unclosed value", `"{\"token\":\"abc`, `"{\"token\":\"[redacted]`},
		{"mismatched escapes kept", `{"token\":"x"}`, `{"token\":"x"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Patterns(tt.in); got != tt.want {
				t.Errorf("Patterns(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestOpensPEM_ClosesPEM_BlockEdges(t *testing.T) {
	if !OpensPEM("key: -----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Error("BEGIN without END must open a block")
	}
	if OpensPEM("-----BEGIN CERTIFICATE-----x-----END CERTIFICATE-----") {
		t.Error("a block closed on the same line must not open one")
	}
	if rest, ok := ClosesPEM("-----END CERTIFICATE----- tail"); !ok || rest != " tail" {
		t.Errorf("ClosesPEM = %q, %v", rest, ok)
	}
}

// Every pattern source compiles and appears once, so a program that checks
// its own copies against PatternSources gets a usable list.
func TestPatternSources(t *testing.T) {
	seen := map[string]bool{}
	for _, src := range PatternSources() {
		if _, err := regexp.Compile(src); err != nil {
			t.Errorf("pattern %q does not compile: %v", src, err)
		}
		if seen[src] {
			t.Errorf("pattern %q listed twice", src)
		}
		seen[src] = true
	}
	if len(seen) < 20 {
		t.Errorf("PatternSources has %d patterns, want at least 20", len(seen))
	}
}

func TestFields_OneTimeCodes(t *testing.T) {
	f := Fields("otp", "link")
	tests := []struct{ in, want string }{
		{`{"to":"a@b.c","otp":"123456"}`, `{"to":"a@b.c","otp":"[redacted]"}`},
		{"INF OTP EMAIL otp=123456 to=a@b.c", "INF OTP EMAIL otp=[redacted] to=a@b.c"},
		{"INF MAGIC LINK link=http://127.0.0.1:1/auth/verify?t=x", "INF MAGIC LINK link=[redacted]"},
		{"hyperlink=keep", "hyperlink=keep"},
	}
	for _, tt := range tests {
		if got := f(tt.in); got != tt.want {
			t.Errorf("Fields(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
