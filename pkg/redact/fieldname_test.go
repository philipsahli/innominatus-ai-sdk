package redact

import "testing"

// TestCredentialName: credential names are redacted, identifiers that only
// contain "key" or "auth" and *_env names are kept.
func TestCredentialName(t *testing.T) {
	redacted := []string{
		"api_key", "apikey", "API_KEY", "apiKey", "x-api-key", "X-Api-Key",
		"access_key", "secret_key", "private_key", "privateKey", "SECRET_KEY",
		"client_secret", "clientSecret", "secret", "secrets",
		"password", "db_password", "dbPassword", "passwd", "pwd", "passphrase",
		"token", "access_token", "refresh_token", "accessToken", "x-auth-token", "GH_TOKEN",
		"authorization", "Authorization", "auth", "basic_auth", "auth_header", "bearer",
		"credential", "credentials", "aws_credentials", "key", "access_key_id",
	}
	kept := []string{
		"list_key", "sort_key", "key_name", "primary_key", "partition_key", "public_key",
		"keyword", "monkey", "auth_mode", "auth_type", "authMode", "author", "authors",
		"auth_password_env", "AUTH_TOKEN_ENV", "api_key_env", "token_env",
		"name", "stack", "host", "username", "certificate", "private",
	}
	for _, n := range redacted {
		if !CredentialName(n) {
			t.Errorf("CredentialName(%q) = false, want true", n)
		}
	}
	for _, n := range kept {
		if CredentialName(n) {
			t.Errorf("CredentialName(%q) = true, want false", n)
		}
	}
}

// TestPatterns_FieldNameShapes: the field-name rule (SensitiveKey
// or CredentialName) in JSON, in YAML with quoted keys (the shape jsonField
// matches) and in YAML with plain keys, which only the value patterns reach.
// A token value is removed whatever the field is called.
func TestPatterns_FieldNameShapes(t *testing.T) {
	const pat = "ghp_abcdefghijklmnopqrstuvwxyz0123"
	tests := []struct {
		name, in, want string
	}{
		{"json names without key or auth kept", `{"name":"web","stack":"shop","host":"db.internal"}`, `{"name":"web","stack":"shop","host":"db.internal"}`},
		// SensitiveKey matches these, so logs and prompts do not keep them.
		{"json tls_key", `{"tls_key": "k-123456"}`, `{"tls_key": "[redacted]"}`},
		{"json cert", `{"cert": "c-123456"}`, `{"cert": "[redacted]"}`},
		{"json certificate and private", `{"certificate":"c-1","private":"p-1"}`, `{"certificate":"[redacted]","private":"[redacted]"}`},
		{"json list_key", `{"list_key": "repositories"}`, `{"list_key": "[redacted]"}`},
		{"json env name", `{"auth_password_env": "REGISTRY_PASSWORD"}`, `{"auth_password_env": "[redacted]"}`},
		{"json identifiers with key or auth", `{"sort_key":"id","auth_mode":"oidc","author":"ana"}`, `{"sort_key":"[redacted]","auth_mode":"[redacted]","author":"[redacted]"}`},
		// CredentialName catches names IsSensitiveKey does not.
		{"json pwd", `{"pwd": "hunter22"}`, `{"pwd": "[redacted]"}`},
		{"json api_key", `{"api_key": "k-123456"}`, `{"api_key": "[redacted]"}`},
		{"json x-api-key", `{"X-Api-Key":"k-123456"}`, `{"X-Api-Key":"[redacted]"}`},
		{"json authorization", `{"authorization":"Token abc"}`, `{"authorization":"[redacted]"}`},
		{"json credentials", `{"credentials":"u:p"}`, `{"credentials":"[redacted]"}`},
		{"json token under harmless name", `{"list_key":"` + pat + `"}`, `{"list_key":"[redacted]"}`},
		{"json jwt under env name", `{"token_env":"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.abc"}`, `{"token_env":"[redacted]"}`},
		{"json escaped env name", `{"m":"{\"api_key_env\":\"API_KEY\"}"}`, `{"m":"{\"api_key_env\":\"[redacted]\"}"}`},
		{"yaml quoted secret", "\"secret_key\": \"s-123456\"\n\"stack\": \"name\"", "\"secret_key\": \"[redacted]\"\n\"stack\": \"name\""},
		{"yaml quoted sort_key", "\"sort_key\": \"name\"", "\"sort_key\": \"[redacted]\""},
		{"yaml plain keys kept", "list_key: repositories\nauth_password_env: REGISTRY_PASSWORD", "list_key: repositories\nauth_password_env: REGISTRY_PASSWORD"},
		{"yaml token under harmless name", "list_key: " + pat, "list_key: [redacted]"},
		{"yaml aws key id", "partition_key: AKIAABCDEFGHIJKLMNOP", "partition_key: [redacted]"},
		{"key=value token", "sort_key=" + pat, "sort_key=[redacted]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Patterns(tt.in); got != tt.want {
				t.Errorf("Patterns(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}
