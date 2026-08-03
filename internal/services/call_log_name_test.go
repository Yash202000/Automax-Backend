package services

import "testing"

func TestOtherPartyName(t *testing.T) {
	if got := otherPartyName("Acme Corp", "", "224"); got != "Acme Corp" {
		t.Errorf("display name should win, got %q", got)
	}
	if got := otherPartyName("", "Jane Agent", "1006"); got != "Jane Agent" {
		t.Errorf("user name should be used when no display name, got %q", got)
	}
	if got := otherPartyName("", "", "0555123456"); got != "0555123456" {
		t.Errorf("phone fallback, got %q", got)
	}
	if got := otherPartyName("", "", ""); got != "" {
		t.Errorf("empty stays empty (frontend renders Unknown), got %q", got)
	}
}
