// sample-fps generates N SDK fingerprints using the same device build the
// whatnot test uses, decodes each, and writes the array of decoded payloads
// to a file. Used to diff SDK output distributions against the legit corpus.
//
// Usage:
//
//	go run ./cmd/sample-fps --count 20 --out legitfp/sdk_decoded.json
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/brianvoe/gofakeit/v7"

	seonsdk "status403.com/rsavv/seonsdk-go"
	"status403.com/rsavv/seonsdk-go/crypto"
	"status403.com/rsavv/seonsdk-go/devices"
	"status403.com/rsavv/seonsdk-go/devices/behaviour"
)

func main() {
	count := flag.Int("count", 20, "number of fingerprints to generate")
	outPath := flag.String("out", "legitfp/sdk_decoded.json", "output JSON array path")
	flag.Parse()

	out := &bytes.Buffer{}
	out.WriteString("[\n")

	for i := 0; i < *count; i++ {
		decoded, err := generateOne()
		if err != nil {
			return
		}
		var indented bytes.Buffer
		if err := json.Indent(&indented, decoded, "  ", "  "); err != nil {
			fmt.Fprintf(os.Stderr, "indent: %v\n", err)
			os.Exit(1)
		}
		if i > 0 {
			out.WriteString(",\n")
		}
		out.WriteString("  ")
		out.Write(indented.Bytes())
		fmt.Fprintf(os.Stderr, "generated %d/%d\n", i+1, *count)
	}

	out.WriteString("\n]\n")
	if err := os.WriteFile(*outPath, out.Bytes(), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote %d fingerprints to %s\n", *count, *outPath)
}

func generateOne() ([]byte, error) {
	firstName := gofakeit.FirstName()
	deviceName := fmt.Sprintf("%s's iPhone", firstName)

	machine, iosVersion := devices.RandomMachineAndRealisticIOS("18.5")

	dev, err := devices.NewRealisticDevice(devices.DeviceOptions{
		Machine:        machine,
		IOSVersion:     iosVersion,
		UserDeviceName: deviceName,
		Locale: &seonsdk.LocaleConfig{
			RegionTimezone:     "+01:00",
			RegionLanguage:     "en",
			RegionCountry:      "GB",
			TimezoneIdentifier: "Europe/London",
			CalendarIdentifier: "gregorian",
			SpeakerName:        "Speaker",
		},
		SkipDC:             true,
		DCSyntheticSuccess: true,
		// device_ip in the synthetic response is what the SEON server "sees"
		// for the client. Without an actual proxy + probe, we leave it blank.
		// The legit corpus shows IPv4s here; an empty string is itself a tell,
		// so callers should fill DCDeviceIP when running a real proxy.
		DCDeviceIP:       "",
		DCAcceptLanguage: "en-GB,en;q=0.9",
	})
	if err != nil {
		return nil, fmt.Errorf("new device: %w", err)
	}

	// Behaviour omission rate: the iPhone 15 / iOS 26.5 corpus shows 3/20
	// (15%) captures with no behaviour_results at all (the probe was launched
	// but didn't accumulate samples before stopBehaviourMonitoring fired).
	enableBehaviour := rand.Float64() >= 0.15

	var behaviourOpts *devices.BehaviourOptions
	if enableBehaviour {
		// Window duration matched to the corpus: heading_changes spans
		// 0–10 s; SEON's host apps typically pulse the probe for a short
		// reading rather than the full 15–20 s window we used to emit.
		// Sample 2–10 s.
		durationMs := 2000 + rand.Intn(8000)
		behaviourOpts = &devices.BehaviourOptions{
			Enabled:             true,
			Duration:            time.Duration(durationMs) * time.Millisecond,
			Activity:            behaviour.RandomizeActivity(),
			HeadingActivity:     behaviour.RandomizeHeadingActivity(),
			InitialHeading:      rand.Float64() * 360,
			MagFieldMagnitude:   devices.BehaviourMagField(dev.LocalePreset(), dev.Machine()),
			EmitAtEar:           false,
			MagneticDeclination: devices.BehaviourMagDeclination(dev.LocalePreset()),
		}
	}
	cfg := dev.NewFingerprint(devices.FingerprintOverrides{
		Behaviour: behaviourOpts,
	})

	wire, err := seonsdk.Fingerprint(*cfg)
	if err != nil {
		return nil, fmt.Errorf("fingerprint: %w", err)
	}
	_, jsonBytes, err := crypto.DecodePayload(wire)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !json.Valid(jsonBytes) {
		return nil, fmt.Errorf("decoded payload is not valid JSON")
	}
	return jsonBytes, nil
}

