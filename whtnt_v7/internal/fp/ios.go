package fp

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

//go:embed ios_template.json
var iosTemplate []byte

func GetIOSFp(ip string) (string, error) {
	sessionID, payload, err := GenerateIOSFingerprint(ip)
	if err != nil {
		return "", err
	}

	return EncryptAndGenerateFingerprintWithPrefix("iOS", sessionID, payload, true)
}

func GenerateIOSFingerprint(ip string) (SessionID, map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(iosTemplate, &payload); err != nil {
		return "", nil, fmt.Errorf("failed to load ios fingerprint template: %w", err)
	}

	sessionID := SessionID(randUUID())
	nowMs := time.Now().UnixMilli()

	payload["session_id"] = string(sessionID)
	payload["timestamp"] = nowMs

	dynEval, err := EncryptAndGenerateFingerprintWithPrefix("iOS", sessionID, map[string]any{
		"request_id":                    randUUID(),
		"device_ip":                     ip,
		"is_battery_capacity_validated": false,
	}, false)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build ios dynamic evaluator: %w", err)
	}
	payload["dynamic_evaluator_results"] = dynEval

	if ip != "" {
		payload["proxy_address"] = ip

		if experimentalFields, ok := payload["experimental_fields"].(map[string]any); ok {
			if nwInterfaces, ok := experimentalFields["nw_interfaces"].(map[string]any); ok {
				nwInterfaces["utun4"] = ip
			}
		}
	}

	return sessionID, payload, nil
}
