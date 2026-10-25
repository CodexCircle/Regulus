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
