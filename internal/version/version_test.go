package version

import (
	"strings"
	"testing"
)

func TestUserAgent(t *testing.T) {
	if got := UserAgent(""); !strings.HasPrefix(got, "connie/"+Version+" ") || strings.Contains(got, "contact") {
		t.Fatalf("UserAgent(\"\") = %q", got)
	}
	if got := UserAgent("you@example.com"); !strings.Contains(got, "contact: you@example.com") {
		t.Fatalf("UserAgent(contact) = %q", got)
	}
}
