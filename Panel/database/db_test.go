package database

import "testing"

func TestNormalizeMiningStatus(t *testing.T) {
	tests := map[string]string{
		"active":     "active",
		"ACTIVE":     "active",
		"  paused  ": "paused",
		"error":      "error",
		"disabled":   "disabled",
		"unknown":    "unknown",
		"":           "unknown",
		"garbage":    "unknown",
		"<script>":   "unknown",
	}
	for in, want := range tests {
		if got := NormalizeMiningStatus(in); got != want {
			t.Errorf("NormalizeMiningStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
