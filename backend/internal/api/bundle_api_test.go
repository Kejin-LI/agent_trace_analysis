package api

import "testing"

func TestBundleIdentityKeyUsesSessionAndArtifact(t *testing.T) {
	gotA := bundleIdentityKey("ses_123", "art_a")
	gotB := bundleIdentityKey("ses_123", "art_b")
	if gotA == gotB {
		t.Fatalf("bundle identity key should distinguish artifacts under the same session: gotA=%q gotB=%q", gotA, gotB)
	}
}

func TestBundleIdentityKeyFallsBackSafely(t *testing.T) {
	if got := bundleIdentityKey(" ses_123 ", " "); got != "session::ses_123" {
		t.Fatalf("unexpected session-only key: %q", got)
	}
	if got := bundleIdentityKey("", " art_123 "); got != "artifact::art_123" {
		t.Fatalf("unexpected artifact-only key: %q", got)
	}
	if got := bundleIdentityKey("", ""); got != "" {
		t.Fatalf("expected empty key when both identifiers are missing, got %q", got)
	}
}
