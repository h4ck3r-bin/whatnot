// seondecrypt decodes a SEON wire-format string and prints the PLAINTEXT
// bytes exactly as Apple's NSJSONSerialization produced them — no Go
// re-formatting. Use this to diff real-device captures against our own SDK
// output byte-for-byte.
//
// Flags:
//
//	--hex     also print a hex dump of the plaintext
//	--pretty  parse+re-emit via Go json.MarshalIndent (loses Apple's exact bytes)
//	--batch <input> --out <output>
//	          read newline-delimited wire strings from <input>, write a JSON
//	          array of decoded fingerprints to <output>. Each entry preserves
//	          the original key order via json.RawMessage.
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"status403.com/rsavv/seonsdk-go/crypto"
)

func main() {
	hexFlag := flag.Bool("hex", false, "also print a hex dump of the plaintext bytes")
	prettyFlag := flag.Bool("pretty", false, "re-emit via Go json.MarshalIndent (loses Apple's exact byte order)")
	batchInput := flag.String("batch", "", "path to a file containing one wire-format string per line")
	batchOutput := flag.String("out", "", "output path for batch mode (JSON array)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: seondecrypt [--hex] [--pretty] <wire-format-string>")
		fmt.Fprintln(os.Stderr, "       seondecrypt --batch <input> --out <output>")
		fmt.Fprintln(os.Stderr, "  Decrypts a SEON payload and prints the raw plaintext bytes.")
		fmt.Fprintln(os.Stderr, "  Wire format: iOS;<sessionId>;<base64(IV)>;<base64(ciphertext)>")
	}
	flag.Parse()

	if *batchInput != "" {
		if err := runBatch(*batchInput, *batchOutput); err != nil {
			fmt.Fprintf(os.Stderr, "batch decode failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	wireString := strings.TrimSpace(flag.Arg(0))

	// DecodeAny rather than DecodePayload: DC RESPONSES are AES-only while
	// fingerprints and DC requests are gzip+AES, and this tool gets pointed
	// at both. See crypto.DecodeAny.
	sessionID, jsonBytes, wasGzipped, err := crypto.DecodeAny(wireString)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Decryption failed: %v\n", err)
		os.Exit(1)
	}

	encoding := "gzip+AES (fingerprint or DC request)"
	if !wasGzipped {
		encoding = "AES only (DC response)"
	}
	fmt.Fprintf(os.Stderr, "Session ID: %s\n", sessionID)
	fmt.Fprintf(os.Stderr, "Encoding: %s\n", encoding)
	fmt.Fprintf(os.Stderr, "Plaintext length: %d bytes\n\n", len(jsonBytes))

	if *prettyFlag {
		var m interface{}
		if err := json.Unmarshal(jsonBytes, &m); err != nil {
			fmt.Fprintf(os.Stderr, "JSON parse failed: %v\n", err)
			os.Stdout.Write(jsonBytes)
			os.Exit(0)
		}
		out, _ := json.MarshalIndent(m, "", "  ")
		os.Stdout.Write(out)
		fmt.Println()
		return
	}

	// Raw path — preserve Apple's exact bytes.
	os.Stdout.Write(jsonBytes)
	fmt.Println()

	if *hexFlag {
		fmt.Fprintln(os.Stderr, "\n--- hex dump ---")
		fmt.Fprint(os.Stderr, hex.Dump(jsonBytes))
	}
}

func runBatch(inputPath, outputPath string) error {
	if outputPath == "" {
		return fmt.Errorf("--out is required when --batch is set")
	}

	f, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer f.Close()

	out := &bytes.Buffer{}
	out.WriteString("[\n")

	sc := bufio.NewScanner(f)
	// Default scanner buffer is too small for full SEON payloads.
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	written := 0
	lineNum := 0
	failed := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		// Captured wire strings sometimes carry JSON-style backslash-escaped
		// forward slashes from upstream serialization. Unescape so the base64
		// alphabet is intact.
		line = strings.ReplaceAll(line, `\/`, `/`)

		sessionID, jsonBytes, _, err := crypto.DecodeAny(line)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "line %d: decode failed: %v\n", lineNum, err)
			continue
		}

		// Validate JSON without reformatting — preserve Apple's exact bytes.
		if !json.Valid(jsonBytes) {
			failed++
			fmt.Fprintf(os.Stderr, "line %d: decoded payload is not valid JSON\n", lineNum)
			continue
		}

		if written > 0 {
			out.WriteString(",\n")
		}
		// Re-emit with two-space indentation for readability while preserving
		// Apple's key order via json.RawMessage round-trip.
		var indented bytes.Buffer
		if err := json.Indent(&indented, jsonBytes, "  ", "  "); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "line %d: indent failed: %v\n", lineNum, err)
			continue
		}
		out.WriteString("  ")
		out.Write(indented.Bytes())
		fmt.Fprintf(os.Stderr, "line %d: decoded session %s (%d bytes)\n", lineNum, sessionID, len(jsonBytes))
		written++
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan input: %w", err)
	}

	out.WriteString("\n]\n")

	if err := os.WriteFile(outputPath, out.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nbatch complete: %d decoded, %d failed -> %s\n", written, failed, outputPath)
	if written == 0 {
		return fmt.Errorf("no fingerprints decoded")
	}
	return nil
}
