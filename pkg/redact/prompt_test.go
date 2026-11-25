package redact

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/philipsahli/innominatus-ai-sdk/pkg/redact/redacttest"
)

// loadCorpus returns the secret corpus with the "<cut>" markers removed.
func loadCorpus(t *testing.T) []redacttest.Entry {
	t.Helper()
	entries, err := redacttest.Corpus()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 30 {
		t.Fatalf("corpus has %d entries, want at least 30", len(entries))
	}
	return entries
}

// jsonQuoted is s as a JSON string body, the way a prompt holds a
// configuration document sent as JSON.
func jsonQuoted(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Every corpus secret is removed by Prompt, as plain text, inside a JSON
// string (escaped once, with HTML escaping as json.Marshal does) and
// inside a JSON string inside a JSON string.
func TestPromptCorpus(t *testing.T) {
	for _, e := range loadCorpus(t) {
		t.Run(e.Kind, func(t *testing.T) {
			once := jsonQuoted(t, e.Text)
			forms := map[string]string{
				"plain":        e.Text,
				"json":         `{"default": ` + once + `}`,
				"json in json": `{"interface": ` + jsonQuoted(t, `{"default": `+once+`}`) + `}`,
			}
			for form, in := range forms {
				out, n := PromptCount(in)
				for _, s := range e.Secrets {
					if strings.Contains(out, s) {
						t.Errorf("%s: %q survived:\n%s", form, s, out)
					}
				}
				if n == 0 {
					t.Errorf("%s: count 0 for a text with a secret", form)
				}
			}
		})
	}
}

// Prompt text that holds no secret stays as it is: Terraform, skill
// prose, YAML and identifiers that look a little like credentials.
func TestPrompt_KeepsOrdinaryText(t *testing.T) {
	keep := []string{
		`variable "db_password" {` + "\n  type      = string\n  sensitive = true\n}",
		"password = var.db_password\napi_key = data.vault_generic_secret.app.data[\"key\"]",
		"secret_name = \"orders-db-credentials\"\nsecret_arn  = \"arn:aws:secretsmanager:eu-west-1:123456789012:secret:prod/db-AbCdEf\"",
		"token_ttl = 3600\nmax_tokens: 4096\ntoken_type: Bearer",
		"auth_mode: oidc\nsort_key: created_at\nkey_name: deployer-2026",
		"password: required\nsecret: optional\ntoken: string",
		"The password must be at least 12 characters and is stored in Vault.",
		"The secret is Vault, the password was rotated and the token of Admin stays.",
		"the password is required and the token of type JWT",
		"source = \"git::https://github.com/acme/terraform-modules.git//bucket?ref=v1.0.0\"",
		"pinned to 3f9a1c7e5b2d8f4a6c0e9b3d1f7a5c2e8b4d0f6a (commit sha) and image sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		"request id 123e4567-e89b-12d3-a456-426614174000",
		"ResourceGroupNameForProductionWestEurope2026 is the resource group",
		"platform/infrastructure/stacks/terraform-apply.yaml",
		"A bucket named [logs] in [eu-west] that keeps old versions",
		"risk-assessment-for-production-environment-2026 and task-runner-for-nightly-builds-01",
		"auth_password_env = \"REGISTRY_PASSWORD\"",
		"https://hooks.example.com/services/x and https://example.com/a@b",
		`{"type":"demo/database","params":[{"name":"db_url","type":"string"}]}`,
		"Credentials: password: required\nlogin: token: optional",
		"Our password policy: rotate every 90 days. The token is required.",
		"Note: secret_name: orders-db-credentials",
	}
	for _, in := range keep {
		if out, n := PromptCount(in); out != in || n != 0 {
			t.Errorf("Prompt changed ordinary text (%d):\n in %q\nout %q", n, in, out)
		}
	}
}

func TestPromptCount_CountsEachRedaction(t *testing.T) {
	in := "password=hunter2-very-secret and AKIA" + "ABCDEFGHIJKLMNOP and [redacted] already"
	out, n := PromptCount(in)
	if n != 2 {
		t.Errorf("count = %d, want 2: %s", n, out)
	}
	if out != Prompt(in) {
		t.Errorf("PromptCount and Prompt disagree")
	}
}

// The new token shapes are in Patterns too, so logs lose them as well.
func TestPatterns_ProviderTokens(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"anthropic", "k sk-ant-" + "api03-Ab1Cd2Ef3Gh4Ij5Kl6Mn7Op8 end", "k [redacted] end"},
		{"openai", "sk-" + "proj-Ab1Cd2Ef3Gh4Ij5Kl6Mn7Op8", "[redacted]"},
		{"sk without digit kept", "sk-abcdefghijklmnopqrstuvwxyz", "sk-abcdefghijklmnopqrstuvwxyz"},
		{"digitalocean", "dop_v1_" + strings.Repeat("a1", 32), "[redacted]"},
		{"slack app", "xapp-" + "1-A05BCD7EF9G-601234567890", "[redacted]"},
		{"slack webhook", "https://hooks.slack.com/services/" + "T0/B0/Xy7Zq2", "https://hooks.slack.com/[redacted]"},
		{"gitlab", "glpat-" + "Xq7Lm2Pv9Rt4Wn8Zb3Yc", "[redacted]"},
		{"stripe", "sk_live_" + "51Hq7Wm2Xp9Lr4Tn8Vb3Zc6Y", "[redacted]"},
		{"azure account key", "AccountName=a;AccountKey=" + "Ab1+Cd2/Ef3==;EndpointSuffix=x", "AccountName=a;AccountKey=[redacted];EndpointSuffix=x"},
		{"azure key name kept", "SharedAccessKeyName=RootManageSharedAccessKey;", "SharedAccessKeyName=RootManageSharedAccessKey;"},
		{"url empty user", "redis://:" + "p4ss-word@cache:6379", "redis://[redacted]@cache:6379"},
		{"url password with @", "mongodb://u:" + "a@b#c@host/db", "mongodb://[redacted]@host/db"},
		{"json-escaped query", `x?a=1&sig=` + "Ab1Cd2", `x?a=1&sig=[redacted]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Patterns(tt.in); got != tt.want {
				t.Errorf("Patterns(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// An assignment that is the value of another one ("login: password: x")
// was skipped: the outer match swallowed the inner name.
func TestPrompt_AssignmentInsideAValue(t *testing.T) {
	const secret = "Bench-Hunter2-Secret"
	for _, in := range []string{
		"Credentials: token: " + secret,
		"Admin login: password: " + secret,
		"login: (password=" + secret + ")",
		"db: shop, DB_PASSWORD=" + secret,
	} {
		if out := Prompt(in); strings.Contains(out, secret) {
			t.Errorf("Prompt kept the secret:\n in %q\nout %q", in, out)
		}
	}
}
