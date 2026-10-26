package update

import "testing"

func TestCheckCompat(t *testing.T) {
	for _, tc := range []struct {
		hub, agent string
		want       Compat
	}{
		{"1.4.0", "1.4.0", CompatCurrent},
		{"1.4.2", "1.4.0", CompatBehind},
		{"1.4.0", "1.4.0-beta.1", CompatBehind},
		{"1.4.0", "1.3.9", CompatBehind},
		{"1.4.0", "1.2.0", CompatTooOld},
		{"1.4.0", "0.9.0", CompatBehind}, // previous major, the protocol decides
		{"2.0.0", "1.9.9", CompatBehind},
		{"3.0.0", "1.9.9", CompatTooOld},
		{"1.4.0", "1.4.1", CompatAhead},
		{"1.4.0", "1.5.0", CompatAhead},
		{"1.4.0", "2.0.0", CompatAhead},
		{"0.3.0", "0.2.0", CompatBehind},
		{"0.3.0", "0.1.0", CompatTooOld},
		{"dev", "1.0.0", CompatUnknown},
		{"1.0.0", "dev", CompatUnknown},
		{"1.0.0", "", CompatUnknown},
		{"v1.0.0-3-gabc1234", "1.0.0", CompatUnknown},
		{"v1.4.0", "1.3.0", CompatBehind},
	} {
		if got := CheckCompat(tc.hub, tc.agent); got != tc.want {
			t.Errorf("CheckCompat(hub %q, agent %q) = %q, want %q", tc.hub, tc.agent, got, tc.want)
		}
	}
}
