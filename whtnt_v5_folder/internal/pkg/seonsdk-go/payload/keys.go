package payload

var KeysLocale = []string{
	"region_timezone",
	"region_language",
	"region_country",
	"timezone_identifier",
	"calendar_identifier",
}

var KeysDevice = []string{
	"accessories_count",
	"device_adid",
	"device_orientation",
	"device_udid",
	"ios_version",
	"pasteboard_hash",
	"screen_brightness",
	"screen_height",
	"screen_width",
	"icloud_ubiquity_token",
	"is_screen_captured",
}

var KeysHardware = []string{
	"battery_level",
	"battery_charging",
	"cpu_type",
	"cpu_count",
	"is_emulator",
	"physical_memory",
	"system_uptime",
}

var KeysMedia = []string{
	"audio_mute_status",
	"audio_volume_current",
	"can_send_mail",
	"can_send_text",
}

var KeysNetwork = []string{
	"vpn_state",
	"proxy_address",
	"proxy_state",
	"wifi_ssid",
	"wifi_mac_address",
}

var KeysOS = []string{
	"kernel_version",
	"kernel_name",
	"kernel_arch",
	"is_jailbroken",
	"is_ios_app_on_mac",
	"is_debugger_attached",
}

var KeysTelephony = []string{
	"network_config",
	"is_on_call",
}

var KeysAccessibility = []string{
	"is_bold_text_enabled",
	"is_reduce_motion_enabled",
	"is_video_autoplay_enabled",
	"is_assisstive_touch_running",
	"is_reduce_transparency_enabled",
	"is_darker_system_colors_enabled",
	"preferred_content_size",
	"mfi_hearing_aids_status",
}

var KeysAuth = []string{
	"is_passcode_enabled",
	"is_biometrics_enabled",
}
