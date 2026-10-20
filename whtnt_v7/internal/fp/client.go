package fp

import "strings"

func GetFp(ip, platform string) (string, error) {
	if strings.EqualFold(platform, "ios") {
		return GetIOSFp(ip)
	}

	session, fingerprint := GenerateLegitFingerprint(ip)
	return EncryptAndGenerateFingerprint(session, fingerprint, true)
}
