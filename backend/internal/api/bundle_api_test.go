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

func TestNormalizeBundleListTotal(t *testing.T) {
	tests := []struct {
		name          string
		reportedTotal int
		loaded        int
		limit         int
		offset        int
		want          int
	}{
		{
			name:          "underfilled first page uses visible rows",
			reportedTotal: 5998,
			loaded:        1600,
			limit:         2000,
			offset:        0,
			want:          1600,
		},
		{
			name:          "full page keeps larger reported total",
			reportedTotal: 5998,
			loaded:        2000,
			limit:         2000,
			offset:        0,
			want:          5998,
		},
		{
			name:          "later underfilled page includes offset",
			reportedTotal: 5998,
			loaded:        600,
			limit:         2000,
			offset:        2000,
			want:          2600,
		},
		{
			name:          "reported total cannot be smaller than visible rows",
			reportedTotal: 100,
			loaded:        200,
			limit:         200,
			offset:        0,
			want:          200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeBundleListTotal(tt.reportedTotal, tt.loaded, tt.limit, tt.offset); got != tt.want {
				t.Fatalf("normalizeBundleListTotal() = %d, want %d", got, tt.want)
			}
		})
	}
}
