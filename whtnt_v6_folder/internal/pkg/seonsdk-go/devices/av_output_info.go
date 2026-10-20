package devices

import (
	seonsdk "status403.com/rsavv/seonsdk-go"
)

// BuildAvOutputInfoFromLocale returns the `av_output_info` dict shape a real
// iPhone emits when audio is routed to the built-in speaker.
//
// The speaker name comes from `loc.SpeakerName` — the localised "Speaker"
// word iOS emits via AVAudioSession. Empty value → "Speaker" (English
// fallback). Presets via `LocaleFromPreset` populate this automatically;
// custom LocaleConfig callers must set it themselves for non-English
// locales or the byte stream will show an English name next to a
// non-English region_country (an immediate tell).
//
// Verified against an iPhone 14 Pro (Polish locale) capture:
//
//	"av_output_info" : {
//	  "channels" : [
//	    {"channelLabel":4294967295,"channelName":"Głośnik 1","channelNumber":1,"owningPortUID":"Speaker"},
//	    {"channelLabel":4294967295,"channelName":"Głośnik 2","channelNumber":2,"owningPortUID":"Speaker"}
//	  ],
//	  "dataSources" : [],
//	  "portName" : "Głośnik",
//	  "portType" : "Speaker",
//	  "portUID" : "Speaker"
//	}
//
// portType + portUID are Core Audio constants ("Speaker") on every locale —
// only `channelName` and `portName` are localised. See IDA
// `-[SEONMediaProbe getCurrentAvOuputInfo]` wrapper at 0x1eb04.
func BuildAvOutputInfoFromLocale(loc seonsdk.LocaleConfig) map[string]interface{} {
	name := loc.SpeakerName
	if name == "" {
		name = "Speaker"
	}
	return map[string]interface{}{
		"channels": []interface{}{
			map[string]interface{}{
				"channelLabel":  uint64(4294967295),
				"channelName":   name + " 1",
				"channelNumber": 1,
				"owningPortUID": "Speaker",
			},
			map[string]interface{}{
				"channelLabel":  uint64(4294967295),
				"channelName":   name + " 2",
				"channelNumber": 2,
				"owningPortUID": "Speaker",
			},
		},
		"dataSources": []interface{}{},
		"portName":    name,
		"portType":    "Speaker",
		"portUID":     "Speaker",
	}
}
