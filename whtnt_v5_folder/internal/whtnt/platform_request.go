package whtnt

import (
	"fmt"
)

var (
	iosAppContext        = "ios/native"
	iosGraphQLClientName = "apollo-ios"
)

func (m *Module) requestAppHeader() string {
	if m.platform == PlatformIOS {
		return "whatnot-ios"
	}
	return "whatnot-web"
}

func (m *Module) requestAppContext() string {
	if m.platform == PlatformIOS {
		return iosAppContext
	}
	return "next-js/browser"
}

func (m *Module) requestAppVersion() string {
	if m != nil && m.platform == PlatformIOS {
		return m.whatnotIOSAppVersion()
	}
	if m != nil && m.config != nil && m.config.AppVersions.WebAppVersion != "" {
		return m.config.AppVersions.WebAppVersion
	}
	return defaultWhatnotWebAppVersion
}

func (m *Module) graphQLEndpoint(operationName string) string {
	return fmt.Sprintf("https://api.whatnot.com/graphql/?operationName=%s", operationName)
}

func (m *Module) referralClientLibrary() map[string]string {
	if m.platform == PlatformIOS {
		return map[string]string{
			"name":    iosGraphQLClientName,
			"version": m.referralClientLibraryVersion(),
		}
	}

	return map[string]string{
		"name":    "apollo-kotlin",
		"version": m.referralClientLibraryVersion(),
	}
}

const defaultWhatnotWebAppVersion = "20260714-0111"

func (m *Module) referralClientLibraryVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.ReferralClientLibraryVer != "" {
		return m.config.AppVersions.ReferralClientLibraryVer
	}
	return "2.0.6"
}
