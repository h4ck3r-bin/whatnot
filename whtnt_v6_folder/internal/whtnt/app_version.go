package whtnt

import "fmt"

const (
	defaultWhatnotIOSAppVersion          = "26.37.0"
	defaultWhatnotIOSAppBuild            = "37"
	defaultWhatnotIOSApolloClientVersion = "2.0.6"
	defaultWhatnotIOSStripeUserAgent     = "Whatnot/" + defaultWhatnotIOSAppBuild + " CFNetwork/3860.200.71 Darwin/25.1.0"
)

func (m *Module) whatnotIOSAppVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.IOSAppVersion != "" {
		return m.config.AppVersions.IOSAppVersion
	}
	return defaultWhatnotIOSAppVersion
}

func (m *Module) whatnotIOSAppBuild() string {
	if m != nil && m.config != nil && m.config.AppVersions.IOSAppBuild != "" {
		return m.config.AppVersions.IOSAppBuild
	}
	return defaultWhatnotIOSAppBuild
}

func (m *Module) whatnotIOSApolloClientVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.IOSApolloClientVersion != "" {
		return m.config.AppVersions.IOSApolloClientVersion
	}
	return defaultWhatnotIOSApolloClientVersion
}

func (m *Module) whatnotIOSStripeUserAgent() string {
	if m != nil && m.config != nil && m.config.AppVersions.WhatnotIOSStripeUserAgent != "" {
		return m.config.AppVersions.WhatnotIOSStripeUserAgent
	}
	return defaultWhatnotIOSStripeUserAgent
}

func (m *Module) whatnotIOSUserAgent(iosVersion string, machine string) string {
	return fmt.Sprintf("Whatnot v%s (%s), iOS %s, %s", m.whatnotIOSAppVersion(), m.whatnotIOSAppBuild(), iosVersion, machine)
}
