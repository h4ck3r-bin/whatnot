package whtnt

import (
	"fmt"
	"strings"
	"sync"
)

type DevicePlatform string

const (
	PlatformAndroid DevicePlatform = "android"
	PlatformIOS     DevicePlatform = "ios"
)

type RuntimeOptions struct {
	ProxyFile string
	Platform  DevicePlatform
}

var runtimeState = struct {
	mu        sync.RWMutex
	proxyFile string
	platform  DevicePlatform
	proxies   []string
}{
	proxyFile: "proxies.txt",
	platform:  PlatformAndroid,
}

func ConfigureRuntime(opts RuntimeOptions) error {
	proxyFile := strings.TrimSpace(opts.ProxyFile)
	if proxyFile == "" {
		proxyFile = "proxies.txt"
	}

	proxyList, err := loadTextLines(proxyFile)
	if err != nil {
		return fmt.Errorf("failed to load proxies from %s: %w", proxyFile, err)
	}
	if len(proxyList) == 0 {
		return fmt.Errorf("no proxies found in %s", proxyFile)
	}

	platform := opts.Platform
	if platform != PlatformIOS {
		platform = PlatformAndroid
	}

	runtimeState.mu.Lock()
	defer runtimeState.mu.Unlock()
	runtimeState.proxyFile = proxyFile
	runtimeState.platform = platform
	runtimeState.proxies = proxyList
	return nil
}

func currentPlatform() DevicePlatform {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()
	return runtimeState.platform
}

func currentProxies() []string {
	runtimeState.mu.RLock()
	defer runtimeState.mu.RUnlock()

	out := make([]string, len(runtimeState.proxies))
	copy(out, runtimeState.proxies)
	return out
}
