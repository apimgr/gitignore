package terminal

import "testing"

func TestModeForWidth(t *testing.T) {
	cases := []struct {
		cols int
		want SizeMode
	}{
		{0, SizeMicro},
		{39, SizeMicro},
		{40, SizeMinimal},
		{59, SizeMinimal},
		{60, SizeCompact},
		{79, SizeCompact},
		{80, SizeStandard},
		{119, SizeStandard},
		{120, SizeWide},
		{159, SizeWide},
		{160, SizeUltrawide},
		{319, SizeUltrawide},
		{320, SizeMassive},
		{1000, SizeMassive},
	}
	for _, tc := range cases {
		if got := ModeForWidth(tc.cols); got != tc.want {
			t.Errorf("ModeForWidth(%d) = %v, want %v", tc.cols, got, tc.want)
		}
	}
}

func TestSizeModeString(t *testing.T) {
	cases := []struct {
		mode SizeMode
		want string
	}{
		{SizeMicro, "micro"},
		{SizeMinimal, "minimal"},
		{SizeCompact, "compact"},
		{SizeStandard, "standard"},
		{SizeWide, "wide"},
		{SizeUltrawide, "ultrawide"},
		{SizeMassive, "massive"},
		{SizeMode(999), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.mode.String(); got != tc.want {
			t.Errorf("SizeMode(%d).String() = %q, want %q", tc.mode, got, tc.want)
		}
	}
}
