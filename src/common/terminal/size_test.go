package terminal

import "testing"

func TestModeForSize(t *testing.T) {
	cases := []struct {
		cols int
		rows int
		want SizeMode
	}{
		{0, 24, SizeStandard},
		{39, 24, SizeMicro},
		{40, 24, SizeMinimal},
		{59, 24, SizeMinimal},
		{60, 24, SizeCompact},
		{79, 24, SizeCompact},
		{80, 24, SizeStandard},
		{119, 24, SizeStandard},
		{120, 40, SizeWide},
		{199, 59, SizeWide},
		{200, 60, SizeUltrawide},
		{399, 79, SizeUltrawide},
		{400, 80, SizeMassive},
		{160, 39, SizeStandard},
		{350, 50, SizeWide},
		{350, 60, SizeUltrawide},
		{400, 79, SizeUltrawide},
		{1000, 100, SizeMassive},
	}
	for _, tc := range cases {
		if got := ModeForSize(tc.cols, tc.rows); got != tc.want {
			t.Errorf("ModeForSize(%d, %d) = %v, want %v", tc.cols, tc.rows, got, tc.want)
		}
	}
}

func TestModeForWidthUsesStandardRowFallback(t *testing.T) {
	if got := ModeForWidth(200); got != SizeStandard {
		t.Fatalf("ModeForWidth(200) = %v, want %v", got, SizeStandard)
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

func TestResponsivePresentationFlags(t *testing.T) {
	cases := []struct {
		mode                         SizeMode
		art, borders, sidebar, icons bool
	}{
		{SizeMicro, false, false, false, false},
		{SizeMinimal, false, false, false, true},
		{SizeCompact, false, true, false, true},
		{SizeWide, true, true, true, true},
	}
	for _, tc := range cases {
		if tc.mode.ShowASCIIArt() != tc.art || tc.mode.ShowBorders() != tc.borders || tc.mode.ShowSidebar() != tc.sidebar || tc.mode.ShowIcons() != tc.icons {
			t.Errorf("presentation flags for %s do not match", tc.mode)
		}
	}
	size := GetTerminalSize()
	if size.Cols <= 0 || size.Rows <= 0 || size.Mode != ModeForSize(size.Cols, size.Rows) {
		t.Fatalf("GetTerminalSize() = %+v", size)
	}
}
