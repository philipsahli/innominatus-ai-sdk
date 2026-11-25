package redacttest

import (
	"strings"
	"testing"
)

// A "<cut>" left in a value would make every check against the corpus
// pass trivially, because no real credential holds the marker.
func TestCorpus_NoMarkersLeft(t *testing.T) {
	entries, err := Corpus()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 30 {
		t.Fatalf("corpus has %d entries, want at least 30", len(entries))
	}
	for _, e := range entries {
		if e.Kind == "" || e.Text == "" || len(e.Secrets) == 0 {
			t.Errorf("entry %q is incomplete", e.Kind)
		}
		if strings.Contains(e.Text, "<cut>") {
			t.Errorf("%s: text still holds <cut>", e.Kind)
		}
		for _, s := range e.Secrets {
			if strings.Contains(s, "<cut>") {
				t.Errorf("%s: secret still holds <cut>", e.Kind)
			}
			if !strings.Contains(e.Text, s) {
				t.Errorf("%s: secret %q is not in the text", e.Kind, s)
			}
		}
	}
}
