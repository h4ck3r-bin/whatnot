package payload

import (
	"testing"
)

// TestCFStringHashASCIIStable pins the CFString hash of each stable-dict
// key. Run values captured from the initial implementation; if this test
// changes, our port of `__CFStrHashEightBit` has drifted from Apple's
// algorithm and every downstream fingerprint's implicit_device_hash will
// shift. When Frida samples arrive, compare the "observed" column to the
// values below — any mismatch means our ASCII hash is wrong.
func TestCFStringHashASCIIStable(t *testing.T) {
	cases := []struct {
		key  string
		want uint64
	}{
		{"device_name", 13098410116585134459},
		{"ios_device_name", 6073356760000529705},
		{"cpu_type", 1757430780807056497},
		{"cpu_count", 8131504423790311641},
		{"user_interface_style", 2846122328658633843},
		{"is_biometrics_enabled", 442418142619196843},
		{"date_format", 17215781382492733073},
		{"physical_memory", 11083186235125859172},
		{"region_language", 1917069726299147062},
		{"region_country", 12357949089559346181},
		{"calendar_identifier", 14150412872726625231},
		{"can_send_mail", 12470608283639951690},
		{"can_send_text", 12470609259346042220},
		{"is_darker_system_colors_enabled", 15080286357052471794},
		{"is_bold_text_enabled", 3116075567334925118},
		{"is_reduce_motion_enabled", 7251844631017034986},
		{"is_video_autoplay_enabled", 1017728793694827363},
		{"is_reduce_transparency_enabled", 15614862529596790388},
		{"preferred_content_size", 16806306676252283465},
		{"screen_size", 6070343760615610021},
	}
	for _, c := range cases {
		got := CFStringHashASCII(c.key)
		if got != c.want {
			t.Errorf("CFStringHashASCII(%q) = %d, want %d", c.key, got, c.want)
		}
	}
}

// TestCFStableDictOrderPinned pins the computed iteration order of the
// 20-field stable dict. This is the order `NSJSONSerialization options=0`
// will walk. If this test flips, every implicit_device_hash output
// changes. Verify against Frida when samples land.
//
// Derivation: StableDictInsertionOrder (IDA 0x1a9b4–0x1accc) runs through
// CFBucketOrder with 41 buckets and linear probing.
func TestCFStableDictOrderPinned(t *testing.T) {
	want := []string{
		"cpu_count",                       //  0 → bucket 0
		"region_language",                 //  1 → bucket 1
		"user_interface_style",            //  2 → bucket 2
		"screen_size",                     //  3 → bucket 3 (probed from 2)
		"region_country",                  //  4 → bucket 5
		"can_send_mail",                   //  5 → bucket 11
		"is_reduce_motion_enabled",        //  6 → bucket 12
		"device_name",                     //  7 → bucket 13
		"cpu_type",                        //  8 → bucket 14
		"date_format",                     //  9 → bucket 15 (probed 13→14→15)
		"can_send_text",                   // 10 → bucket 16
		"is_darker_system_colors_enabled", // 11 → bucket 17 (probed from 16)
		"is_biometrics_enabled",           // 12 → bucket 19
		"calendar_identifier",             // 13 → bucket 22
		"preferred_content_size",          // 14 → bucket 23 (probed from 22)
		"is_reduce_transparency_enabled",  // 15 → bucket 32
		"ios_device_name",                 // 16 → bucket 37
		"is_bold_text_enabled",            // 17 → bucket 38
		"physical_memory",                 // 18 → bucket 39
		"is_video_autoplay_enabled",       // 19 → bucket 40 (probed 37→38→39→40)
	}
	got := CFStableDictOrder
	if len(got) != len(want) {
		t.Fatalf("CFStableDictOrder len: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestCFBucketCountForSize verifies the capacity → bucket-count mapping
// for the level-5 size that matters (20 elements → 41 buckets).
func TestCFBucketCountForSize(t *testing.T) {
	cases := []struct {
		n    int
		want uint64
	}{
		{0, 0},
		{1, 3},
		{3, 3},
		{4, 7},
		{6, 7},
		{7, 13},
		{19, 23},
		{20, 41}, // stable dict
		{32, 41},
		{33, 71},
	}
	for _, c := range cases {
		if got := cfBucketCountForSize(c.n); got != c.want {
			t.Errorf("cfBucketCountForSize(%d) = %d, want %d", c.n, got, c.want)
		}
	}
}

// TestMarshalCFCompactShape — sanity-check the byte format of the
// compact-JSON writer: commas between entries, no whitespace, colons
// without spaces, and booleans / numbers / strings formatted the way
// NSJSONSerialization would.
func TestMarshalCFCompactShape(t *testing.T) {
	dict := map[string]interface{}{
		"a": "hello",
		"b": 42,
		"c": true,
		"d": nil,
	}
	order := []string{"b", "d", "a", "c"}

	got, err := MarshalCFCompact(dict, order)
	if err != nil {
		t.Fatalf("MarshalCFCompact: %v", err)
	}
	want := `{"b":42,"d":null,"a":"hello","c":true}`
	if string(got) != want {
		t.Errorf("MarshalCFCompact = %s, want %s", string(got), want)
	}
}

// TestMarshalCFCompactNoHTMLEscape — `<`, `>`, `&` must stay literal
// (NSJSONSerialization never HTML-escapes).
func TestMarshalCFCompactNoHTMLEscape(t *testing.T) {
	got, err := MarshalCFCompact(
		map[string]interface{}{"x": "a<b>c&d"},
		[]string{"x"},
	)
	if err != nil {
		t.Fatalf("MarshalCFCompact: %v", err)
	}
	want := `{"x":"a<b>c&d"}`
	if string(got) != want {
		t.Errorf("got %s, want %s", string(got), want)
	}
}
