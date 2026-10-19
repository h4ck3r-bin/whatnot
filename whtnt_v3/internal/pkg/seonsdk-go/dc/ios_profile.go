package dc

import (
	"github.com/bogdanfinn/fhttp/http2"
	"github.com/bogdanfinn/tls-client/profiles"
)

// IOSNSURLSessionProfile matches the exact TLS + H2 fingerprint a real iPhone
// running iOS 26.x sends for the SEON DC request. Verified against a rvictl
// capture of *.seondnsresolve.com:
//
//	JA3:  ecdf4f49dd59effc439639da29186671
//	JA4:  t13d2013h2_a09f3c656075_7f0f34a4126d
//	H2:   2:0|4:2097152|3:100|9:1|10485760|0|m,s,p,a
//
// This is the same Safari_IOS_26_0 profile the main app uses — iOS's
// NSURLSession shares its BoringSSL configuration across all connections.
var IOSNSURLSessionProfile = newIOSNSURLSessionProfile()

func newIOSNSURLSessionProfile() profiles.ClientProfile {
	return profiles.NewClientProfile(
		profiles.Safari_IOS_26_0.GetClientHelloId(),
		map[http2.SettingID]uint32{
			http2.SettingEnablePush:           0,
			http2.SettingInitialWindowSize:    2097152,
			http2.SettingMaxConcurrentStreams: 100,
			http2.SettingNoRFC7540Priorities:  1,
		},
		[]http2.SettingID{
			http2.SettingEnablePush,
			http2.SettingInitialWindowSize,
			http2.SettingMaxConcurrentStreams,
			http2.SettingNoRFC7540Priorities,
		},
		[]string{":method", ":scheme", ":path", ":authority"},
		10485760,
		nil,
		nil,
		0,
		false,
		nil,
		nil,
		0,
		nil,
		false,
	)
}

// Compile-time assertion: this is a bogdanfinn-compatible profile.
var _ profiles.ClientProfile = IOSNSURLSessionProfile
