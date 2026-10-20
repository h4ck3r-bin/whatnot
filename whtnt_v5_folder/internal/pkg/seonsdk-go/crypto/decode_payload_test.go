package crypto

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodePayloadFromString lets you paste a SEON wire-format payload into
// the `payload` string below and run:
//
//	go test ./crypto/ -run TestDecodePayloadFromString -v
//
// It prints the session ID and pretty-printed plaintext JSON. Leave `payload`
// empty to skip the test.
//
// Wire format: iOS;<sessionId>;<base64(IV)>;<base64(ciphertext)>
func TestDecodePayloadFromString(t *testing.T) {
	const payload = `iOS;53CF4F76-2DFA-4AE6-AADD-62D45710F940;\/W3xoxiaBLurOvJRyWwXYQ==;PIIjFKwk5Xl3K6EhBmJ7vXeogoQhCg2fKN7veX7KlGSMCEPSUwa5ses3Pygap8tZqAC1L7cRvzapecyoNNrWgLVD00E5kJ76QpG\/YGxysU9iV9lGEh2okT\/u0lVUK8Q0c6Elo2DODhJaXiIiIY7u8fdCGIdXhf3N5iMWO4aOu1lSSNmCUTuBxFjJPiky1dF3IrxwrFL28G0iecaBQebSHNSG9DE9voqu8BBMbpF2hqqOPAxQqevsU2ezuBJrWgIp46\/KZrs0A+1qS2+9z1xMRkuFxqzs0TsOp2JH3WdzsaM\/HLzFr\/RP0s\/Yg3XJkg9zBTkC2FdnB50Yom8eNaoeGQBla0FY477ri94CXMTlWEKvKVBEMJd1i+27LazAw6nRupucg9fP83Zuyyd7HPpcXWdYTxErFe5QGSWV21wlfuWbkDK9n3ccWr0dka4wHLUJgNGNO25lVQsknyOoJU\/fsSgsBth5wUCLb5WtIv1MRBNld6B0rO\/w\/pNX79WHRF3KgE7i+le7OfA9pa9HAXqSkH5lO70JQGJli5UBKY8Tl24+sxlc2+u9FFQUEJxHcq\/zQ3njNLGe9L8tdYxv8fjQ7bdcj9EwWEJ1RM83wZWt\/1OVMYq3Rmn4Z9PJ9fK8l5OEC571GVKUpXDJKmKwCU7ozH8sBr15zXjOqkyhnJQuy+VX0XMGByCqqQE91lShut96fUwZPp8Kveo9jpmZO8k6QOD+Cw1BzetP8i916Y8EsWl7BZ+o9jM3X3n8XsZ9BlifFlOdqwcH7ymBitaUSEXhrGOa5HxZShiKlun1jThL\/M\/mm2JujuzALs8\/dPdy81nEG+YUxU8ZABfq7nwc2nwZASeZy3rJ3N6552C02+Ko8KdH4Rh3N2f2Isv0RDXhOPl7XFPZWl25ynLF\/Ywavw5hGG4DMCOkokQlRVvu3lS3KJf5BHs7WSjH+GD8q+I3z\/7V+B8nI8FCE+160+PdpZAXTsZQh6lN1Ygf7E8fl0uHtkzVdSXNu7iksTKoC5BUyVzE54CylwcJ6vq7o1guYwZ2hUUqbshswGnr+HRek03c7r9Owm8utzIcstLEMoX0qP0EDSVdGR5wLaZ51dHtRYj1n7T5SPYgEq43yGWMvnoP4DYJoYi4iIYmvSAJ4JUkqnZizk0HqKBgYlJrlXIDuc5VObUHxx91OOiDJkeK78gGRGlfETWITeIkunAo5Bc\/TUywcG3DyPQqwHqOAgJMUXeOjCDXWoLmmoL13yjctKurpYHdUWqUg2EmDa0A5PWBuuQvc+Hzj8tmAgWWWW1b5D\/WKt72GhEn3unNJ2SG+mhRssOhVVdqDykhc6wkiEF3wLlU+pBBD5EMt\/2hVCdwKixdyF8lWSpT7OlDqxbWEiHTui2HrkhLYgHlFLBsJ7sTWvCcGCv70tUBsW3Siv2ccDSqTdGVl0f3eHajkPyg8cE4RUzO1RL\/zcIF49wuno9GAYw0VVhuDR5+uylxshH7W1dGys9tJhOrrHDu4F\/EBDKKc5DDVQvPMQq8jaDExC7R9PkXcDVxnUKIDpxmdFVPTX3ko0+06BsqWiZtq+nvKiD3+TVJu\/6Q1tJ3Nx58McJoxhbNJPlhjX8VJKTO1Fs9WE0HHms4X77Y50ZTYzlGqgShmv0b8B1FtlnJlMuYIks3G0DIbO+58M639txGRyESzKsTZhYlHBx667jqkIyys80Xm3rmCVpV5yxPIQPrC2s\/azpK9On2ZKQHgZhm0f94oGwZmiGIEd87PMl5qZ6Z943Adh4605alx09SfIE34TVnjIhBX76BzjKhmBd0uoWdJJequbYy\/mwGqPzqihu0W5Zq4n\/1FMnpyxzOKAUODSd1VFJiMci46iDtwV6CcUKb8CzswB894yKNdrviGndmVb0s6Zd2Ia+maRNVJJ0j54TrgicR8Sa15xtB+inVZHbHvfesC7IMbqgIWAdjbQHu0Pi9JNzQXdMU\/Rmw7Ih9vkILeLOr575+NBPc1LI509W3qOPtYlpZ\/v8AT5smgGUe0UB2\/lbiQ5eNW3U6jDij3RRk0Oz4xQ1mYlFKY92mOOSbLYTkOZbWh6uO\/cdtmBAjravSpDawP5QFU81+83kIAQ+N1wuqaHxDbjqUsF00gjN4zFgK8iecKHpsmFgMj6+9wtV6ITO5x9efZKwuH71g7wr3EQ7v5DrdmAM9dofTxcN4v2qeV8eU7RyH8oLK29ZMaqe8HzhLtEUfKrBSO95lkQDh0AO6PCRQqbWS0b+DOFa3DlksN4QCiZkD8SOC2sZZBUJvKX1FO1h1Y\/HxJ23YcbSr2arMt046pqG5k2LbHWZ\/YVtgrksg2pytFxwItKMhKPoI\/uwX0M214IuDElGlCC6\/blFkz8zprrXeUnXczjnm721AT5rWadnuc9IC+yGLpfd+yeITTu3QKIZmcJh48o1+MxKxLtM6UdLAJcpTe6xxYSE1AuANYxtQNO8Pit\/zB5SytSqiIMHZUki5DjjM0\/J7bBpwia5wBkMQpQFVHoye0\/BFxpo+MvBKYWg4XYGxmBExQEgkC4QgfEAhD+t7vWaBuoj8aWLlkqRxkJHHveZ+FyWJ7obLjChbrCQxsCtw6uzTBVS7L0cCYRh16pdrmJH1LuQyJs+vlZE9qAnT0KhvQFcjH9VGM278Rhr+Yq3jgeQmJLNCN5CZQ4LRCd8PQebvLpXpR6u3+\/UO\/HaT7jynoT1u0KSp+a5aeQRqGzoq2s88zdDULTuYSm+dosGHK\/Dkscx+i4PnhFoOPCDFNP7nTnSZjeRS5ufqKDYTX0qQYrj5rcKN5dQoKEt20So10BdPN\/S4Xj0i+LIl08ViAkS0ph4gS\/Kh2lhWFkuU6ikM4G3c58WJb\/0JnpFL7NdjD+j2EHHUTf547nL\/n7of6e8Zo3LoHT0m65D3zZWI+2+JPRlHjXwmfjIQZwbhZ1KnyKWQcenDXJUqvuXuQ9YXIm6JJvmaZx\/42ulQkrb1kz7t+7vszjbUafKHHa4FDrn7Oe9IIgeEaK6EmH0tIckeZgrwa9umgvdoZxyMPcTW45B0u1olv4MRdgD9yLWzMcp8YO146C9hppJNbeL0Zu1Cm2G95RFZ5+wtyj2ybnbw4PvALLcnjZHKeRQ3oimJUs86sUSbqGTOtKiY9XIpWgc4UTrTbI2CA+E+ZH+NMBhWSvIttwP55Uuk+MPOiFJyVaziaRySNJYRrBh+\/ZQ03FgqpB2lLJmnIxPdQyXFkANzmWgPSLf+nLvUJV5SYhuoU1imeW92yzqJNV0f7vgr8rWLMThYSU8nTZhBJhwP6\/VYb5DppyNwRsnM+c0ZYG80X4ip4vlPsnGCL8jAPWVuBxkINcnJx4yLdqZCLsTb5yo92vhFIAuSZyI0PBZqBUpo\/h9mFsHrhHOA6UOHEKjxUERRdkvKu+IZ+pDrIzXdG4DgwIEvNvpMQgV1n9UcBkzGlVDIPscWpyg3OshtuBNrcmIoXDhZBTnsfFDs0TTr8JcPldLxF4g2bRM4G2Z3ybhLES9ykT6mbscm`

	wire := normalizeWire(payload)
	if wire == "" {
		t.Skip("paste a wire-format payload into the `payload` constant to run this test")
	}

	sessionID, jsonBytes, err := DecodePayload(wire)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	t.Logf("Session ID: %s", sessionID)
	t.Logf("Plaintext length: %d bytes", len(jsonBytes))

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, jsonBytes, "", "  "); err != nil {
		t.Logf("raw plaintext (not valid JSON): %s", string(jsonBytes))
		t.Fatalf("json.Indent: %v", err)
	}
	t.Logf("Plaintext JSON:\n%s", pretty.String())
}

// TestDecodePayloadsFromStrings lets you decode multiple payloads at once.
// Add wire-format strings to the `payloads` slice and run:
//
//	go test ./crypto/ -run TestDecodePayloadsFromStrings -v
func TestDecodePayloadsFromStrings(t *testing.T) {
	payloads := []string{
		// paste payloads here, one per entry
	}

	if len(payloads) == 0 {
		t.Skip("add wire-format payloads to the `payloads` slice to run this test")
	}

	for i, raw := range payloads {
		wire := normalizeWire(raw)
		if wire == "" {
			continue
		}
		t.Run("payload", func(t *testing.T) {
			sessionID, jsonBytes, err := DecodePayload(wire)
			if err != nil {
				t.Fatalf("payload[%d] decode failed: %v", i, err)
			}

			t.Logf("[%d] session ID: %s (%d bytes)", i, sessionID, len(jsonBytes))

			var pretty bytes.Buffer
			if err := json.Indent(&pretty, jsonBytes, "", "  "); err != nil {
				t.Logf("[%d] raw: %s", i, string(jsonBytes))
				return
			}
			t.Logf("[%d] JSON:\n%s", i, pretty.String())
		})
	}
}

// normalizeWire trims whitespace and reverses common copy-paste mangling so a
// wire-format string pulled from JSON logs (with `\/` escapes), shell quoting,
// or pretty-printers still decodes cleanly.
func normalizeWire(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}
