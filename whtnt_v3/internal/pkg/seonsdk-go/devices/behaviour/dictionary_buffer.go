package behaviour

import (
	"fmt"
	"sort"
	"time"
)

// DictionaryBuffer mirrors SEONBehaviourDictionaryBuffer (IDA 0x1445c–0x150e0).
//
// It's a bounded insertion-order dictionary with two key-storage semantics:
//
//   - String keys (orientation_changes uses "%ld" unix-sec strings) → stored
//     verbatim, serialised out as those same strings.
//   - Time-valued keys (heading_changes uses NSDate) → at serialisation the
//     SDK does `stringWithFormat:@"%ld", date.timeIntervalSince1970` — i.e.
//     truncates the fractional part. Our Go equivalent is `time.Time` inputs
//     converted via `Unix()`.
//
// When `maxSize` would be exceeded, the oldest key (by ascending `compare:`
// order — chronological for both supported key types) is evicted. Matches the
// `maxSize` / `_sortedKeys` eviction path in the `addEntry:forKey:` block at
// 0x14ad0.
type DictionaryBuffer struct {
	maxSize    int
	data       map[string]interface{}
	order      []string // insertion-ordered-sort keys for eviction (same as _sortedKeys)
}

// NewDictionaryBuffer matches -[initWithMaxSize:] at 0x1445c.
func NewDictionaryBuffer(maxSize int) *DictionaryBuffer {
	if maxSize < 0 {
		maxSize = 0
	}
	return &DictionaryBuffer{
		maxSize: maxSize,
		data:    make(map[string]interface{}),
	}
}

// AddEntry stores value under the string form of key and enforces the maxSize
// cap via FIFO eviction of the smallest key already present. Matches the block
// invoke at 0x14ad0 byte-for-byte: overwrite in place if key already exists
// (no eviction), otherwise evict the oldest if full then insert.
func (b *DictionaryBuffer) AddEntry(key string, value interface{}) {
	if b.maxSize == 0 {
		return
	}
	if _, exists := b.data[key]; exists {
		b.data[key] = value
		return
	}
	if len(b.data) >= b.maxSize {
		// Evict the first (smallest per compare:) key.
		oldest := b.order[0]
		b.order = b.order[1:]
		delete(b.data, oldest)
	}
	b.data[key] = value
	// insertKey: does addObject + sortUsingComparator(compare:) — for both
	// NSString-"%ld" and the string keys emitted elsewhere, ascending compare is
	// the natural chronological order on zero-padded fixed-width epoch seconds
	// (and lexicographic otherwise).
	b.order = append(b.order, key)
	sort.Strings(b.order)
}

// AddEntryAtTime is a convenience for time.Time-keyed inserts (heading data).
// Matches the SDK's NSDate-key path: the key goes into `_data` as an NSDate
// initially, then `toDictionary` serialises it as `stringWithFormat:"%ld",
// timeIntervalSince1970`. We flatten to that final string form on insert so
// one storage type holds both cases.
func (b *DictionaryBuffer) AddEntryAtTime(t time.Time, value interface{}) {
	b.AddEntry(fmt.Sprintf("%d", t.Unix()), value)
}

// ToDictionary returns the accumulated entries as a plain map, matching the
// NSMutableDictionary copy returned by -[toDictionary] at 0x145b4.
// Keys are always strings in the payload — any time-valued keys were flattened
// to "%ld" on insert, which matches the 0x14690 NSDate-key path.
func (b *DictionaryBuffer) ToDictionary() map[string]interface{} {
	out := make(map[string]interface{}, len(b.data))
	for k, v := range b.data {
		out[k] = v
	}
	return out
}

// Data exposes the underlying map for read-only inspection; parallels
// -[SEONBehaviourDictionaryBuffer data] at 0x150e0.
func (b *DictionaryBuffer) Data() map[string]interface{} { return b.data }

// Count returns the live entry count.
func (b *DictionaryBuffer) Count() int { return len(b.data) }
