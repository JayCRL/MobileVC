package adapter

import "testing"

func TestStripANSIColors(t *testing.T) {
	input := "\x1b[31mhello\x1b[0m\n"
	if got := StripANSI(input); got != "hello\n" {
		t.Fatalf("expected hello\\n, got %q", got)
	}
}

func TestStripANSIMultipleSequences(t *testing.T) {
	input := "\x1b[2J\x1b[HProceed\x1b[33m?\x1b[0m [y/N]"
	if got := StripANSI(input); got != "Proceed? [y/N]" {
		t.Fatalf("unexpected stripped output: %q", got)
	}
}

func TestStripANSIPreservesPlainText(t *testing.T) {
	input := "plain text\nnext line"
	if got := StripANSI(input); got != input {
		t.Fatalf("plain text changed: %q", got)
	}
}
