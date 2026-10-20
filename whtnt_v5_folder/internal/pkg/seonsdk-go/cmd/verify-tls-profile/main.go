// verify-tls-profile dials a local TLS listener using the SDK's
// IOSNSURLSessionProfile, captures the raw ClientHello bytes from the wire,
// and prints the JA3 fingerprint. Use to confirm the bogdanfinn profile
// matches the iPhone capture without needing rvictl.
//
// Expected JA3 (from iphone-20260419-183108.pcap, SEON DC ClientHello):
//
//	773906b0efdefa24a7f2b8eb6985bf37
package main

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/bogdanfinn/tls-client/profiles"
	tls "github.com/bogdanfinn/utls"

	dc "status403.com/rsavv/seonsdk-go/dc"
)

const expectedJA3 = "773906b0efdefa24a7f2b8eb6985bf37"

func main() {
	profile := dc.IOSNSURLSessionProfile

	// Listen and accept exactly one connection — we only need the ClientHello.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	defer ln.Close()

	chBytes := make(chan []byte, 1)
	errCh := make(chan error, 2)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- fmt.Errorf("accept: %w", err)
			return
		}
		defer conn.Close()
		// Read TLS record header (5 bytes), then the rest of the record.
		hdr := make([]byte, 5)
		if _, err := io.ReadFull(conn, hdr); err != nil {
			errCh <- fmt.Errorf("read hdr: %w", err)
			return
		}
		if hdr[0] != 0x16 {
			errCh <- fmt.Errorf("not handshake record: 0x%02x", hdr[0])
			return
		}
		rlen := binary.BigEndian.Uint16(hdr[3:5])
		body := make([]byte, rlen)
		if _, err := io.ReadFull(conn, body); err != nil {
			errCh <- fmt.Errorf("read body: %w", err)
			return
		}
		chBytes <- body
	}()

	// Dial through utls with the same ClientHelloID the bogdanfinn profile uses.
	addr := ln.Addr().String()
	rawConn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer rawConn.Close()

	tlsCfg := &tls.Config{ServerName: "verify.example.com", InsecureSkipVerify: true}
	// withForceHttp1=false so utls leaves the ALPN list intact (h2 + http/1.1).
	uconn := tls.UClient(rawConn, tlsCfg, profile.GetClientHelloId(), false, false, true)
	go func() {
		// Handshake will fail because we never reply, that's fine.
		_ = uconn.Handshake()
	}()

	body := <-chBytes
	close(chBytes)

	// body[0] = handshake type (0x01 ClientHello)
	if len(body) < 4 || body[0] != 0x01 {
		fmt.Fprintln(os.Stderr, "not ClientHello")
		os.Exit(1)
	}
	hsLen := int(body[1])<<16 | int(body[2])<<8 | int(body[3])
	if 4+hsLen > len(body) {
		hsLen = len(body) - 4
	}
	ch := body[4 : 4+hsLen]

	ja3, parts := computeJA3(ch)
	fmt.Printf("ClientHello bytes (%d): %s\n\n", len(body), hex.EncodeToString(body))
	fmt.Printf("JA3 string : %s\n", ja3)
	hash := md5.Sum([]byte(ja3))
	got := hex.EncodeToString(hash[:])
	fmt.Printf("JA3 MD5    : %s\n", got)
	fmt.Printf("Expected   : %s\n", expectedJA3)
	fmt.Printf("Profile    : %s (%s)\n", profile.GetClientHelloStr(), profileKind(profile))
	if got == expectedJA3 {
		fmt.Println("MATCH      : ✅")
	} else {
		fmt.Println("MATCH      : ❌")
		fmt.Println("\nDecoded parts:")
		for k, v := range parts {
			fmt.Printf("  %-22s %s\n", k+":", v)
		}
		os.Exit(1)
	}
}

func profileKind(p profiles.ClientProfile) string {
	if p.GetClientHelloId().Client == "NSURLSessionIOS" {
		return "iPhone NSURLSession (custom)"
	}
	return p.GetClientHelloStr()
}

// computeJA3 strips GREASE and concatenates fields per JA3 spec:
//
//	SSLVersion,Cipher,Extension,Curve,PointFormat
func computeJA3(ch []byte) (string, map[string]string) {
	parts := map[string]string{}
	if len(ch) < 38 {
		return "", parts
	}
	o := 0
	legacyVersion := binary.BigEndian.Uint16(ch[o:])
	o += 2 + 32 // version + random
	o += 1 + int(ch[o])
	csLen := int(binary.BigEndian.Uint16(ch[o:]))
	o += 2
	ciphers := []uint16{}
	for i := 0; i < csLen; i += 2 {
		c := binary.BigEndian.Uint16(ch[o+i:])
		if !isGREASE(c) {
			ciphers = append(ciphers, c)
		}
	}
	o += csLen
	o += 1 + int(ch[o]) // compression methods

	extTotal := int(binary.BigEndian.Uint16(ch[o:]))
	o += 2
	end := o + extTotal
	exts := []uint16{}
	var curves []uint16
	var formats []byte
	for o < end {
		et := binary.BigEndian.Uint16(ch[o:])
		o += 2
		el := int(binary.BigEndian.Uint16(ch[o:]))
		o += 2
		body := ch[o : o+el]
		o += el
		if isGREASE(et) {
			continue
		}
		exts = append(exts, et)
		switch et {
		case 10:
			gl := int(binary.BigEndian.Uint16(body[:2]))
			for i := 0; i < gl; i += 2 {
				g := binary.BigEndian.Uint16(body[2+i:])
				if !isGREASE(g) {
					curves = append(curves, g)
				}
			}
		case 11:
			fl := int(body[0])
			formats = append(formats, body[1:1+fl]...)
		}
	}
	parts["legacy_version"] = strconv.Itoa(int(legacyVersion))
	parts["ciphers"] = joinUints(ciphers)
	parts["extensions"] = joinUints(exts)
	parts["curves"] = joinUints(curves)
	parts["formats"] = joinBytes(formats)

	ja3 := fmt.Sprintf("%d,%s,%s,%s,%s",
		legacyVersion, joinUints(ciphers), joinUints(exts),
		joinUints(curves), joinBytes(formats))
	return ja3, parts
}

func isGREASE(v uint16) bool { return (v&0x0f0f) == 0x0a0a && (v>>8) == (v&0xff) }

func joinUints(xs []uint16) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = strconv.Itoa(int(x))
	}
	return strings.Join(s, "-")
}
func joinBytes(xs []byte) string {
	s := make([]string, len(xs))
	for i, x := range xs {
		s[i] = strconv.Itoa(int(x))
	}
	return strings.Join(s, "-")
}
