package whtnt

import (
	http2 "github.com/bogdanfinn/fhttp/http2"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

func whatnotIOSProfile() profiles.ClientProfile {
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

func mustNewHTTPSession(platform DevicePlatform) tls_client.HttpClient {
	c, err := createHTTPClient(platform, "")
	if err != nil {
		panic("failed to create HTTP session: " + err.Error())
	}
	return c
}

func createHTTPClient(platform DevicePlatform, proxy string) (tls_client.HttpClient, error) {
	jar := tls_client.NewCookieJar()

	opts := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithCookieJar(jar),
		tls_client.WithNotFollowRedirects(),
		tls_client.WithInsecureSkipVerify(),
	}

	if platform == PlatformIOS {
		opts = append(opts, tls_client.WithClientProfile(whatnotIOSProfile()))
	} else {
		opts = append(opts, tls_client.WithClientProfile(profiles.Chrome_133))
	}

	if proxy != "" {
		opts = append(opts, tls_client.WithProxyUrl(proxy))
	}

	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), opts...)
}
