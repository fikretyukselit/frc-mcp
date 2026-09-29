package facts

import "testing"

func TestVendordepYear(t *testing.T) {
	for _, tc := range []struct {
		frc, wpilib any
		want        string
	}{{"2026", nil, "2026"}, {2026.0, nil, "2026"}, {nil, "2027_alpha7", "2027_alpha7"}, {"", "2027_alpha7", "2027_alpha7"}, {nil, nil, ""}} {
		if got := VendordepYear(tc.frc, tc.wpilib); got != tc.want {
			t.Errorf("VendordepYear(%v, %v) = %q, want %q", tc.frc, tc.wpilib, got, tc.want)
		}
	}
	for in, want := range map[string]string{"2026": "2026", "2027_alpha7": "2027", "": "", "latest": "", "20261": "", "1999": ""} {
		if got := DeclaredSeason(in); got != want {
			t.Errorf("DeclaredSeason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseInstalledWPILibYear(t *testing.T) {
	in, err := ParseInstalled(`{"name": "REVLib", "version": "2027.0.0-alpha-7", "uuid": "u", "wpilibYear": "2027_alpha7"}`)
	if err != nil || in.FRCYear != "2027_alpha7" {
		t.Fatalf("%+v %v", in, err)
	}
}
