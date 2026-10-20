package version

import "testing"

func TestVersionCodeFor(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"5.6.3", "01050603"},
		{"6.0.0", "01060000"},
		{"12.34.5", "01123405"},
		{"5.6", "01050600"},   // missing patch → 0
		{"5", "01050000"},     // missing minor + patch
		{"", "01000000"},      // empty
		{"99.99.99", "01999999"},
		{"100.200.300", "01000000"}, // >99 wraps via % 100 → 0
	}
	for _, c := range cases {
		if got := VersionCodeFor(c.in); got != c.want {
			t.Errorf("VersionCodeFor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSourceFor(t *testing.T) {
	if got, want := SourceFor("5.6.3"), "ios-5.6.3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := SourceFor("6.0"), "ios-6.0"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultsMatchExpected(t *testing.T) {
	if Source != "ios-5.7.1" {
		t.Errorf("Source = %q, want ios-5.7.1 for SDK 5.7.1", Source)
	}
	if VersionCode != "01050701" {
		t.Errorf("VersionCode = %q, want 01050701 for SDK 5.7.1", VersionCode)
	}
}
