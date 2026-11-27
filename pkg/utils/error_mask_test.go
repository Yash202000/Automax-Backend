package utils

import (
	"strings"
	"testing"
)

func TestSanitizeErrorText(t *testing.T) {
	in := `login failed password=Secret123 token: abc.def Authorization: Bearer xyz.123 phone +971501234567 id 1234567890`
	out := SanitizeErrorText(in)
	for _, leak := range []string{"Secret123", "abc.def", "xyz.123", "501234567", "1234567890"} {
		if strings.Contains(out, leak) {
			t.Errorf("output still contains %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "+971 50 *** 4567") {
		t.Errorf("phone not masked as expected: %s", out)
	}
}
