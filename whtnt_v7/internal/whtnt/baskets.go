package whtnt

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rnldo.dev/wnshell/internal/appconfig"
)

const (
	prize10  = "\u00a310"
	prize20  = "\u00a320"
	prize200 = "\u00a3200"
)

type Product struct {
	ProductName      string `json:"ProductName"`
	ProductListingID string `json:"ProductListingId"`
	ProductID        string `json:"ProductId"`
	ProductPrice     int    `json:"ProductPrice"`
	MaxPrice         int    `json:"maxPrice"`
	InStock          bool   `json:"inStock"`
}

type PrizeBaskets map[string][][]Product

func basketsJSONPath() (string, error) {
	cfg, err := appconfig.Load()
	if err == nil && strings.TrimSpace(cfg.Engine.BasketsJSONPath) != "" {
		return cfg.Engine.BasketsJSONPath, nil
	}

	workingDirectory, wdErr := os.Getwd()
	if wdErr != nil {
		return "", fmt.Errorf("get working directory: %w", wdErr)
	}

	return filepath.Join(workingDirectory, "setup", "baskets.json"), nil
}

func loadOrCreatePrizeBaskets() (PrizeBaskets, error) {
	path, err := basketsJSONPath()
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read baskets.json: %w", err)
		}

		baskets := defaultPrizeBaskets()
		if err := writePrizeBasketsJSON(path, baskets); err != nil {
			return nil, err
		}
		return baskets, nil
	}

	var baskets PrizeBaskets
	if err := json.Unmarshal(raw, &baskets); err != nil {
		return nil, fmt.Errorf("parse baskets.json: %w", err)
	}

	normalized := normalizeInStockDefaultsWithRaw(baskets, raw)
	if err := validatePrizeBaskets(baskets); err != nil {
		return nil, err
	}

	if normalized {
		if err := writePrizeBasketsJSON(path, baskets); err != nil {
			return nil, err
		}
	}

	return baskets, nil
}

func writePrizeBasketsJSON(path string, baskets PrizeBaskets) error {
	data, err := json.MarshalIndent(baskets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baskets: %w", err)
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(path), "baskets_*.json")
	if err != nil {
		return fmt.Errorf("create temp baskets file: %w", err)
	}
	tempName := tempFile.Name()

	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempName)
	}()

	if _, err := tempFile.Write(data); err != nil {
		return fmt.Errorf("write temp baskets file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp baskets file: %w", err)
	}
	if err := replaceFileWithRetry(tempName, path); err != nil {
		return fmt.Errorf("replace baskets file: %w", err)
	}

	return nil
}

func validatePrizeBaskets(baskets PrizeBaskets) error {
	if len(baskets) == 0 {
		return errors.New("baskets.json has no baskets")
	}

	for prizeName, prizeBaskets := range baskets {
		if len(prizeBaskets) == 0 {
			return fmt.Errorf("prize %q has no baskets", prizeName)
		}

		for basketIndex, basket := range prizeBaskets {
			if len(basket) == 0 {
				return fmt.Errorf("prize %q basket[%d] is empty", prizeName, basketIndex)
			}

			for productIndex, product := range basket {
				if product.ProductName == "" {
					return fmt.Errorf("prize %q basket[%d] product[%d] missing ProductName", prizeName, basketIndex, productIndex)
				}
				if product.ProductListingID == "" {
					return fmt.Errorf("prize %q basket[%d] product[%d] missing ProductListingId", prizeName, basketIndex, productIndex)
				}
				if product.ProductID == "" {
					return fmt.Errorf("prize %q basket[%d] product[%d] missing ProductId", prizeName, basketIndex, productIndex)
				}
				if product.ProductPrice <= 0 {
					return fmt.Errorf("prize %q basket[%d] product[%d] invalid ProductPrice=%d", prizeName, basketIndex, productIndex, product.ProductPrice)
				}
			}
		}
	}

	return nil
}

func normalizeInStockDefaultsWithRaw(baskets PrizeBaskets, raw []byte) bool {
	if strings.Contains(string(raw), `"inStock"`) {
		return false
	}

	for prizeName := range baskets {
		for basketIndex := range baskets[prizeName] {
			for productIndex := range baskets[prizeName][basketIndex] {
				baskets[prizeName][basketIndex][productIndex].InStock = true
			}
		}
	}

	return true
}

func defaultPrizeBaskets() PrizeBaskets {
	return PrizeBaskets{
		prize10: {
			{
				{
					ProductName:      "Destined Rivals Booster Pack",
					ProductListingID: "TGlzdGluZ05vZGU6MTMwNzg2OTI4MA==",
					ProductID:        "UHJvZHVjdE5vZGU6MzIyODAxOTU5",
					ProductPrice:     800,
					MaxPrice:         800,
					InStock:          true,
				},
			},
		},
		prize20: {
			{
				{
					ProductName:      "Fusion Strike Booster Pack",
					ProductListingID: "TGlzdGluZ05vZGU6MTQ0MDEyNTg1OA==",
					ProductID:        "UHJvZHVjdE5vZGU6MzYzNTAxNzUy",
					ProductPrice:     1600,
					MaxPrice:         1600,
					InStock:          true,
				},
			},
			{
				{
					ProductName:      "151 Booster Pack",
					ProductListingID: "TGlzdGluZ05vZGU6MTM1Njc2Njc3Mw==",
					ProductID:        "UHJvZHVjdE5vZGU6MzM4MDc2Mzg1",
					ProductPrice:     2000,
					MaxPrice:         2000,
					InStock:          true,
				},
			},
		},
		prize200: {
			{
				{
					ProductName:      "1g 999.9 fine gold",
					ProductListingID: "TGlzdGluZ05vZGU6MTQ1MTg4NTQ5NA==",
					ProductID:        "UHJvZHVjdE5vZGU6MzY3NjIwNTM5",
					ProductPrice:     14200,
					MaxPrice:         14200,
					InStock:          true,
				},
				{
					ProductName:      "151 Booster Pack",
					ProductListingID: "TGlzdGluZ05vZGU6MTM1Njc2Njc3Mw==",
					ProductID:        "UHJvZHVjdE5vZGU6MzM4MDc2Mzg1",
					ProductPrice:     2000,
					MaxPrice:         2000,
					InStock:          true,
				},
				{
					ProductName:      "151 Booster Pack",
					ProductListingID: "TGlzdGluZ05vZGU6MTM1Njc2Njc3Mw==",
					ProductID:        "UHJvZHVjdE5vZHVjdE5vZGU6MzM4MDc2Mzg1",
					ProductPrice:     2000,
					MaxPrice:         2000,
					InStock:          true,
				},
				{
					ProductName:      "151 Booster Pack",
					ProductListingID: "TGlzdGluZ05vZGU6MTM1Njc2Njc3Mw==",
					ProductID:        "UHJvZHVjdE5vZGU6MzM4MDc2Mzg1",
					ProductPrice:     2000,
					MaxPrice:         2000,
					InStock:          true,
				},
			},
			{
				{
					ProductName:      "Scarlet & Violet Surging Sparks Booster Box",
					ProductListingID: "TGlzdGluZ05vZGU6MTMyNzQzMzgyOQ==",
					ProductID:        "UHJvZHVjdE5vZGU6MzI5MTQ5NjYz",
					ProductPrice:     21500,
					MaxPrice:         21500,
					InStock:          true,
				},
			},
		},
	}
}

func BasketSignature(basket []Product) string {
	var builder strings.Builder
	for index, product := range basket {
		if index > 0 {
			builder.WriteByte('|')
		}
		builder.WriteString(product.ProductListingID)
	}
	return builder.String()
}

func GetShuffledEligibleBasketsForPrizeDebug(prizeName string) ([][]Product, string, error) {
	baskets, err := loadOrCreatePrizeBaskets()
	if err != nil {
		return nil, "", err
	}

	prizeBaskets, exists := baskets[prizeName]
	if !exists || len(prizeBaskets) == 0 {
		return nil, "", fmt.Errorf("unknown prize %q", prizeName)
	}

	eligible := make([][]Product, 0, len(prizeBaskets))
	rejectedForStock := 0
	for _, basket := range prizeBaskets {
		if !basketEligible(basket) {
			rejectedForStock++
			continue
		}
		cloned := make([]Product, len(basket))
		copy(cloned, basket)
		eligible = append(eligible, cloned)
	}

	if len(eligible) == 0 {
		return nil, fmt.Sprintf("Prize %q: total=%d eligible=0 rejectedForStock=%d", prizeName, len(prizeBaskets), rejectedForStock), fmt.Errorf("no eligible baskets for %q", prizeName)
	}

	if err := shuffleBaskets(eligible); err != nil {
		return nil, "", err
	}

	debugMessage := fmt.Sprintf("Prize %q: total=%d eligible=%d rejectedForStock=%d", prizeName, len(prizeBaskets), len(eligible), rejectedForStock)
	return eligible, debugMessage, nil
}

func shuffleBaskets(baskets [][]Product) error {
	for i := len(baskets) - 1; i > 0; i-- {
		j, err := cryptoIntn(i + 1)
		if err != nil {
			return err
		}
		baskets[i], baskets[j] = baskets[j], baskets[i]
	}
	return nil
}

func basketEligible(basket []Product) bool {
	for _, product := range basket {
		if !product.InStock {
			return false
		}
	}

	return true
}

func cryptoIntn(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("invalid max value %d", max)
	}

	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return 0, fmt.Errorf("read random bytes: %w", err)
	}

	return int(binary.LittleEndian.Uint64(buffer[:]) % uint64(max)), nil
}

func SetPriceAndInStockByListingID(listingID string, newPrice int, inStock bool) (int, error) {
	return SetPriceAndInStockByListingIDWithMax(listingID, newPrice, 0, inStock)
}

func SetPriceAndInStockByListingIDWithMax(listingID string, newPrice int, maxPrice int, inStock bool) (int, error) {
	if listingID == "" {
		return 0, errors.New("listingID is empty")
	}

	baskets, err := loadOrCreatePrizeBaskets()
	if err != nil {
		return 0, err
	}

	updated := 0
	for prizeName := range baskets {
		for basketIndex := range baskets[prizeName] {
			for productIndex := range baskets[prizeName][basketIndex] {
				if baskets[prizeName][basketIndex][productIndex].ProductListingID != listingID {
					continue
				}

				baskets[prizeName][basketIndex][productIndex].ProductPrice = newPrice
				if maxPrice > 0 {
					baskets[prizeName][basketIndex][productIndex].MaxPrice = maxPrice
				}
				baskets[prizeName][basketIndex][productIndex].InStock = inStock
				updated++
			}
		}
	}

	if updated == 0 {
		return 0, fmt.Errorf("no basket entries found for listingID %q", listingID)
	}

	path, err := basketsJSONPath()
	if err != nil {
		return 0, err
	}

	if err := writePrizeBasketsJSON(path, baskets); err != nil {
		return 0, err
	}

	return updated, nil
}
