package engine

import (
	"encoding/json"
	"math/rand"
	"os"
	"strings"
)

type ReviewPool struct {
	Path string
}

func (p ReviewPool) Pick(chancePercent float64) (string, error) {
	if chancePercent <= 0 || rand.Float64()*100 >= chancePercent {
		return "", nil
	}

	raw, err := os.ReadFile(p.Path)
	if err != nil {
		return "", err
	}

	var texts []string
	if err := json.Unmarshal(raw, &texts); err != nil {
		return "", err
	}

	cleaned := make([]string, 0, len(texts))
	for _, text := range texts {
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	if len(cleaned) == 0 {
		return "", nil
	}

	return cleaned[rand.Intn(len(cleaned))], nil
}
