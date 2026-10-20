package payload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Compact JSON writer used by MarshalCFCompact. Matches
// `NSJSONSerialization options=0` byte output:
//   - `{"k":v,"k":v}` — no spaces anywhere
//   - No HTML escaping of `<`, `>`, `&`
//   - Numbers: integers printed without decimals, floats via Apple's
//     shortest-round-trip double formatter (equivalent to Go's 'g'
//     formatting with sufficient precision)

type cfBuf struct {
	b bytes.Buffer
}

func (c *cfBuf) WriteByte(b byte)         { _ = c.b.WriteByte(b) }
func (c *cfBuf) WriteString(s string)     { _, _ = c.b.WriteString(s) }
func (c *cfBuf) Write(p []byte)           { _, _ = c.b.Write(p) }
func (c *cfBuf) Bytes() []byte            { return c.b.Bytes() }

func writeCompactValue(buf *cfBuf, v interface{}) error {
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
		writeCompactString(buf, val)
	case json.Number:
		buf.WriteString(val.String())
	case map[string]interface{}:
		// Nested map: NSJSONSerialization would recurse in its own bucket
		// order. The 20-field stable dict has no nested maps, so we don't
		// encounter this in the hash path. Emit a bucket-ordered child in
		// case a caller extends usage.
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		ordered := CFBucketOrder(keys)
		buf.WriteByte('{')
		for i, k := range ordered {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCompactString(buf, k)
			buf.WriteByte(':')
			if err := writeCompactValue(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	case []interface{}:
		buf.WriteByte('[')
		for i, x := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCompactValue(buf, x); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	default:
		// Fallback for rarer types (int32, uint, etc.) — json.Marshal with
		// HTML escape off so `<>&` stay literal.
		b, err := marshalStringNoHTMLAny(val)
		if err != nil {
			return fmt.Errorf("writeCompactValue: unsupported type %T: %w", v, err)
		}
		buf.Write(b)
	}
	return nil
}

func writeCompactString(buf *cfBuf, s string) {
	// Use the shared no-HTML-escape string marshaler so `<`, `>`, `&`
	// remain literal (matches NSJSONSerialization).
	b, err := marshalStringNoHTML(s)
	if err != nil {
		// json.Encoder on a string can't fail in practice; fall back to
		// quoted-string marshaling for safety.
		fallback, _ := json.Marshal(s)
		buf.Write(fallback)
		return
	}
	buf.Write(b)
}

func marshalStringNoHTMLAny(v interface{}) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := b.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}
