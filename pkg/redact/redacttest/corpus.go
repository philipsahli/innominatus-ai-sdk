// Package redacttest holds the secret corpus: fake credentials of the kinds
// that reach a model prompt or a log line. redact.Prompt must remove every
// secret of every entry. A caller that builds prompts from user text can run
// the same corpus through its own path and check that no secret reaches the
// model.
package redacttest

import (
	_ "embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// secret_corpus.yaml splits every fake value with "<cut>", so the file
// itself matches no secret-scanner rule (gitleaks, GitHub push protection).
// Corpus removes the markers, so the values have their real shapes at run
// time.
//
//go:embed secret_corpus.yaml
var corpusYAML []byte

// Entry is one fake credential.
type Entry struct {
	// Kind names the credential shape, such as "aws-access-key-id".
	Kind string `yaml:"kind"`
	// Text is what lands in a prompt, as a person would paste it.
	Text string `yaml:"text"`
	// Secrets are distinctive substrings of Text that must not survive.
	Secrets []string `yaml:"secrets"`
}

// Corpus returns the entries with the "<cut>" markers removed.
func Corpus() ([]Entry, error) {
	var entries []Entry
	if err := yaml.Unmarshal(corpusYAML, &entries); err != nil {
		return nil, fmt.Errorf("redacttest: parse corpus: %w", err)
	}
	for i := range entries {
		entries[i].Text = strings.ReplaceAll(entries[i].Text, "<cut>", "")
		for j, s := range entries[i].Secrets {
			entries[i].Secrets[j] = strings.ReplaceAll(s, "<cut>", "")
		}
	}
	return entries, nil
}
