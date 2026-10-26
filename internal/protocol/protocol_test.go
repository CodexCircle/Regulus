package protocol

import "testing"

func TestSupported(t *testing.T) {
	tests := []struct {
		version uint32
		want    bool
	}{
		{0, false},
		{MinSupported, true},
		{Version, true},
		{Version + 1, false},
	}
	for _, tt := range tests {
		if got := Supported(tt.version); got != tt.want {
			t.Errorf("Supported(%d) = %v, want %v", tt.version, got, tt.want)
		}
	}
}

func TestNegotiate(t *testing.T) {
	tests := []struct {
		min, max uint32
		want     uint32
		ok       bool
	}{
		{0, Version, Version, true},                // an agent that predates the min field
		{MinSupported, Version, Version, true},     // same range
		{MinSupported, Version + 3, Version, true}, // a newer agent that still speaks ours
		{0, 0, 0, false},
		{0, Version + 1, 0, false}, // a newer agent that speaks only newer versions
		{Version + 1, Version + 2, 0, false},
		{Version + 1, Version, Version, true}, // inverted range: treated as only max
	}
	for _, tt := range tests {
		got, ok := Negotiate(tt.min, tt.max)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("Negotiate(%d, %d) = %d, %v; want %d, %v", tt.min, tt.max, got, ok, tt.want, tt.ok)
		}
	}
}
