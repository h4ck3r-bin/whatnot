package payload

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// corpusFixtures are the three real iOS payloads decrypted from
// freshwhatnot.har. See testdata/freshwhatnot/README.md.
var corpusFixtures = []string{
	"fp_register.json",
	"fp_setphone_1.json",
	"fp_setphone_2.json",
}

// unquotedFloat matches a JSON number with a fractional part appearing as a
// value (i.e. after `" : "`). Quoted values start with `"` so they can't
// match — that keeps the `"292.088"`-style performance strings out, since
// those are `%.3f` strings and not doubles.
var unquotedFloat = regexp.MustCompile(`:\s(-?\d+\.\d+(?:[eE][-+]?\d+)?)`)

func corpusPath(name string) string {
	return filepath.Join("..", "testdata", "freshwhatnot", name)
}

// TestFormatFloatMatchesAppleCorpus asserts that formatFloat reproduces every
// float literal in the real-device corpus byte-for-byte.
//
// This is the regression guard for the `%.17g` vs Go-shortest divergence: at
// the time of writing Go-shortest matched only 44 of 110 literals.
func TestFormatFloatMatchesAppleCorpus(t *testing.T) {
	total, mismatched := 0, 0

	for _, fixture := range corpusFixtures {
		raw, err := os.ReadFile(corpusPath(fixture))
		if err != nil {
			t.Fatalf("read corpus %s: %v", fixture, err)
		}

		matches := unquotedFloat.FindAllStringSubmatch(string(raw), -1)
		if len(matches) == 0 {
			t.Fatalf("%s: no float literals found — fixture corrupted?", fixture)
		}

		for _, m := range matches {
			literal := m[1]
			total++

			parsed, err := strconv.ParseFloat(literal, 64)
			if err != nil {
				t.Fatalf("%s: cannot parse %q: %v", fixture, literal, err)
			}
			if got := formatFloat(parsed); got != literal {
				mismatched++
				t.Errorf("%s: formatFloat(%s) = %s, want %s (Apple emits %%.17g)",
					fixture, literal, got, literal)
			}
		}
	}

	if total < 100 {
		t.Fatalf("only %d float literals scanned across %d fixtures; expected 100+",
			total, len(corpusFixtures))
	}
	t.Logf("%d/%d corpus float literals reproduced exactly", total-mismatched, total)
}

// TestFormatFloatEdgeCases pins the boundaries that the corpus doesn't reach:
// integral values, clean decimals, and the exponent-notation range. Expected
// values are C's `%.17g`, which is what NSJSONSerialization uses.
func TestFormatFloatEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  string
	}{
		{"zero", 0, "0"},
		{"negative zero", math0(), "0"},
		{"integral", 3, "3"},
		{"negative integral", -42, "-42"},
		{"clean two-decimal", 0.02, "0.02"},
		{"clean negative", -0.01, "-0.01"},
		{"needs 17 digits", 0.41, "0.40999999999999998"},
		{"corpus mag field", 887.31283091733587, "887.31283091733587"},
		{"corpus heading", 246.65010070800781, "246.65010070800781"},
		{"corpus metal duration", 0.065922737121582031, "0.065922737121582031"},
		{"small no exponent", 0.0001, "0.0001"},
		{"exponent lower bound", 1e-5, "1.0000000000000001e-05"},
		{"exponent small", 1e-7, "9.9999999999999995e-08"},
		{"exponent large", 1.5e20, "1.5e+20"},
		{"exponent upper", 1e21, "1e+21"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatFloat(tc.input); got != tc.want {
				t.Errorf("formatFloat(%v) = %s, want %s", tc.input, got, tc.want)
			}
		})
	}
}

// math0 returns negative zero without importing math just for Copysign.
func math0() float64 {
	z := 0.0
	return -z
}

// TestFormatFloatNotGoShortest documents the specific divergence this fix
// addresses: for these corpus values Go's shortest round-trip form differs
// from what a real device emits.
func TestFormatFloatNotGoShortest(t *testing.T) {
	diverging := []string{
		"0.40999999999999998",
		"0.90999999999999992",
		"887.31283091733587",
		"246.65010070800781",
		"0.065922737121582031",
		"0.0047557424133465543",
		"44.435556004755199",
	}

	for _, literal := range diverging {
		parsed, err := strconv.ParseFloat(literal, 64)
		if err != nil {
			t.Fatalf("parse %q: %v", literal, err)
		}
		if shortest := strconv.FormatFloat(parsed, 'g', -1, 64); shortest == literal {
			t.Errorf("%s: expected Go-shortest to diverge, but it matched — "+
				"update this list if Go's formatting changed", literal)
		}
		if got := formatFloat(parsed); got != literal {
			t.Errorf("formatFloat(%s) = %s, want %s", literal, got, literal)
		}
	}
}
