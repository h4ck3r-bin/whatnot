package behaviour

import (
	"testing"
	"time"
)

// TestDictionaryBufferEviction verifies FIFO-by-smallest-key eviction when
// maxSize is exceeded. Matches the SDK's `data.count >= maxSize` branch at
// 0x14bf0 and the `sortedKeys[0]` removal at 0x14c18.
func TestDictionaryBufferEviction(t *testing.T) {
	b := NewDictionaryBuffer(3)
	b.AddEntry("1700000001", 1.0)
	b.AddEntry("1700000002", 2.0)
	b.AddEntry("1700000003", 3.0)
	b.AddEntry("1700000004", 4.0) // should evict "1700000001"

	d := b.ToDictionary()
	if len(d) != 3 {
		t.Fatalf("count after eviction: got %d, want 3", len(d))
	}
	if _, ok := d["1700000001"]; ok {
		t.Error("evicted key still present")
	}
	if d["1700000004"] != 4.0 {
		t.Error("new key missing")
	}
}

// TestDictionaryBufferOverwrite verifies that adding a duplicate key
// overwrites in place without evicting anyone — matches the existing-key
// check at 0x14b48 / 0x14b84.
func TestDictionaryBufferOverwrite(t *testing.T) {
	b := NewDictionaryBuffer(3)
	b.AddEntry("a", 1.0)
	b.AddEntry("b", 2.0)
	b.AddEntry("a", 99.0)
	d := b.ToDictionary()
	if d["a"] != 99.0 {
		t.Errorf("overwrite: got %v, want 99.0", d["a"])
	}
	if len(d) != 2 {
		t.Errorf("no new key created: got %d, want 2", len(d))
	}
}

// TestDictionaryBufferNSDateKey verifies the NSDate-key → "%ld" flattening
// at insert (matches the SDK's toDictionary path at 0x14690 where NSDate
// keys are stringified via timeIntervalSince1970).
func TestDictionaryBufferNSDateKey(t *testing.T) {
	b := NewDictionaryBuffer(5)
	t0 := time.Unix(1700000000, 500_000_000) // 0.5 s after the integer tick
	b.AddEntryAtTime(t0, 42.0)
	d := b.ToDictionary()
	if _, ok := d["1700000000"]; !ok {
		t.Errorf("NSDate key not flattened to unix seconds: got %v", d)
	}
}

// TestDictionaryBufferZeroMax verifies that max-size-0 silently swallows.
func TestDictionaryBufferZeroMax(t *testing.T) {
	b := NewDictionaryBuffer(0)
	b.AddEntry("x", 1.0)
	if b.Count() != 0 {
		t.Fatalf("zero max: got %d, want 0", b.Count())
	}
}
