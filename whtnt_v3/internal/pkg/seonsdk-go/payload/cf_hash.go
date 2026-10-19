package payload

// CFString hashing — ported from Apple's open-source CoreFoundation
// (`__CFStrHashEightBit` / `__CFStrHashCharacters` in CFString.c, Darwin
// opensource).
//
// SEONFingerprint's `implicit_device_hash` is SHA-256 of a compact JSON
// serialization of a 20-field NSMutableDictionary (IDA 0x1a8cc → 0x1a674).
// `NSJSONSerialization options=0` iterates the dict in hash-bucket order
// (ascending bucket index), NOT alphabetical, NOT insertion order. To
// reproduce the exact byte stream an iPhone would emit we need both the
// same hash function and the same bucket layout — implemented here.
//
// Scope: ASCII-only 8-bit path. All 20 stable-dict keys are ASCII, as are
// all string values passed through device/hardware probes. Any key >96
// bytes would need the long-string path (chunked hash of first/middle/last
// 32 chars); we assert non-ASCII + >96-byte keys aren't used.

// CFStringHashASCII reproduces `__CFStrHashEightBit(bytes, len, len)` for
// strings ≤96 bytes. Returns the 64-bit hash (CFHashCode is unsigned long,
// 64-bit on arm64). Wrapping multiplication matches the C behavior since
// CoreFoundation relies on unsigned integer overflow semantics.
//
// Algorithm (from CFString.c):
//
//	result = len
//	for each full 4-byte block [a,b,c,d]:
//	    result = result*67503105 + a*16974593 + b*66049 + c*257 + d
//	for each remaining byte x:
//	    result = result*257 + x
//	return result + (result << (len & 31))
//
// Constants: 67503105 = 257⁴, 16974593 = 257³, 66049 = 257², 257 = 257¹.
func CFStringHashASCII(s string) uint64 {
	n := len(s)
	if n > 96 {
		// Long-string path not implemented — panic so a mismatch is loud.
		// Any iPhone string hashed in CFString.c beyond 96 chars uses
		// first-32 / middle-32 / last-32 chunking. None of the 20 stable
		// keys approach that length.
		panic("CFStringHashASCII: strings >96 bytes require the long-string variant (not implemented)")
	}

	result := uint64(n)

	end4 := n & ^3
	for i := 0; i < end4; i += 4 {
		result = result*67503105 +
			uint64(s[i])*16974593 +
			uint64(s[i+1])*66049 +
			uint64(s[i+2])*257 +
			uint64(s[i+3])
	}
	for i := end4; i < n; i++ {
		result = result*257 + uint64(s[i])
	}
	return result + (result << (uint64(n) & 31))
}
