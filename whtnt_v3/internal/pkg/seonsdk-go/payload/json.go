package payload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// MarshalSortedJSON emits the Apple-style "key" : value pretty-sorted JSON.
// Strings are never HTML-escaped — NSJSONSerialization on iOS leaves `<`, `>`,
// and `&` literal, so Go's default `\u003c`/`\u003e`/`\u0026` escaping would
// be an immediate byte-level tell. Callers do not get to choose otherwise.
func MarshalSortedJSON(v map[string]interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeValue(&buf, v, 0); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeValue(buf *bytes.Buffer, v interface{}, depth int) error {
	switch val := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case int:
		buf.WriteString(strconv.Itoa(val))
	case int64:
		buf.WriteString(strconv.FormatInt(val, 10))
	case uint64:
		buf.WriteString(strconv.FormatUint(val, 10))
	case float64:
		buf.WriteString(formatFloat(val))
	case string:
		b, err := marshalStringNoHTML(val)
		if err != nil {
			return err
		}
		buf.Write(b)
	case json.Number:
		buf.WriteString(val.String())
	case map[string]interface{}:
		return writeMap(buf, val, depth)
	case []interface{}:
		return writeArray(buf, val, depth)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("unsupported type %T: %w", val, err)
		}
		buf.Write(b)
	}
	return nil
}

func writeMap(buf *bytes.Buffer, m map[string]interface{}, depth int) error {
	if len(m) == 0 {
		buf.WriteString("{\n\n")
		writeIndent(buf, depth)
		buf.WriteByte('}')
		return nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteString("{\n")
	for i, k := range keys {
		writeIndent(buf, depth+1)
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteString(" : ")
		if err := writeValue(buf, m[k], depth+1); err != nil {
			return err
		}
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	writeIndent(buf, depth)
	buf.WriteByte('}')
	return nil
}

func writeArray(buf *bytes.Buffer, arr []interface{}, depth int) error {
	if len(arr) == 0 {
		buf.WriteString("[\n\n")
		writeIndent(buf, depth)
		buf.WriteByte(']')
		return nil
	}

	buf.WriteString("[\n")
	for i, v := range arr {
		writeIndent(buf, depth+1)
		if err := writeValue(buf, v, depth+1); err != nil {
			return err
		}
		if i < len(arr)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	writeIndent(buf, depth)
	buf.WriteByte(']')
	return nil
}

func writeIndent(buf *bytes.Buffer, depth int) {
	for i := 0; i < depth; i++ {
		buf.WriteString("  ")
	}
}

// marshalStringNoHTML emits a JSON-quoted string matching Apple's
// NSJSONSerialization byte output:
//
//   - `<`, `>`, `&` left literal (Go's json default escapes to `\u003c`, etc.)
//   - `/` escaped to `\/`  (Apple always escapes forward slashes; Go never
//     does — this is the single most common byte-level tell because every
//     `timezone_identifier` like `"Europe/Warsaw"` hits it)
//   - all other standard JSON escapes (\", \\, \n, \r, \t, \u control) kept
//
// Verified against a real iPhone 14 Pro (iOS 18.5) fingerprint dump where
// `timezone_identifier` emits `"Europe\/Warsaw"` — 14 raw bytes including
// the `\/` escape, not `/`.
func marshalStringNoHTML(s string) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return nil, err
	}
	out := b.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return appleEscapeSlash(out), nil
}

// appleEscapeSlash converts every literal `/` inside a JSON string to `\/`.
// Input must be a JSON-encoded string (surrounded by quotes, already escaped
// for control chars and HTML if applicable). We only mutate bytes that
// appear outside of already-existing escape sequences — since json.Encoder
// never emits a bare `\` followed by `/`, a simple scan is safe.
func appleEscapeSlash(in []byte) []byte {
	slashes := 0
	for _, c := range in {
		if c == '/' {
			slashes++
		}
	}
	if slashes == 0 {
		return in
	}
	out := make([]byte, 0, len(in)+slashes)
	for _, c := range in {
		if c == '/' {
			out = append(out, '\\', '/')
		} else {
			out = append(out, c)
		}
	}
	return out
}

// formatFloat renders a double the way Apple's NSJSONSerialization does:
// C's `%.17g` — 17 significant digits with trailing zeros removed.
//
// Go's `strconv.FormatFloat(f, 'g', -1, 64)` instead emits the *shortest*
// representation that round-trips, which is a different string for most
// doubles. Measured against testdata/freshwhatnot (three real iOS payloads,
// 110 distinct float literals):
//
//	%.17g          110/110 match
//	Go-shortest     44/110 match
//
// e.g. real `0.40999999999999998` vs Go-shortest `0.41`, and real
// `887.31283091733587` vs Go-shortest `887.3128309173359`. Emitting the
// shortest form is a byte-level tell on every payload carrying
// behaviour_results or metal_info.
//
// Go's 'g' verb with an explicit precision counts significant digits and
// strips trailing zeros, matching C's %g. The integral shortcut below keeps
// whole numbers as `3` rather than `3.0000000000000000`, which is also what
// NSJSONSerialization emits.
func formatFloat(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 17, 64)
}
