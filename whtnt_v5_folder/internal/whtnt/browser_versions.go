package whtnt

const (
	defaultBrowserUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36"
	defaultBrowserSecChUa         = `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`
	defaultBrowserSecChUaPlatform = `"Windows"`
	defaultKasadaVersion          = "j-1.2.779"
	defaultMobileKasadaVersion    = "i-1.18.3"

)

func (m *Module) browserUserAgent() string {
	if m != nil && m.config != nil && m.config.AppVersions.BrowserUserAgent != "" {
		return m.config.AppVersions.BrowserUserAgent
	}
	return defaultBrowserUserAgent
}

func (m *Module) browserSecChUa() string {
	if m != nil && m.config != nil && m.config.AppVersions.BrowserSecChUa != "" {
		return m.config.AppVersions.BrowserSecChUa
	}
	return defaultBrowserSecChUa
}

func (m *Module) browserSecChUaPlatform() string {
	if m != nil && m.config != nil && m.config.AppVersions.BrowserSecChUaPlatform != "" {
		return m.config.AppVersions.BrowserSecChUaPlatform
	}
	return defaultBrowserSecChUaPlatform
}

func (m *Module) kasadaVersion() string {
	if m != nil && m.config != nil && m.config.AppVersions.KasadaVersion != "" {
		return m.config.AppVersions.KasadaVersion
	}
	return defaultMobileKasadaVersion
}
