package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"maybe-finance-backend/utils"
)

type FXRatesResponse struct {
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	UpdatedAt string             `json:"updatedAt"`
}

// Fallback static exchange rates relative to USD
var defaultUSDRates = map[string]float64{
	"USD": 1.0,
	"IDR": 16250.0,
	"SGD": 1.34,
	"EUR": 0.92,
	"JPY": 155.0,
	"MYR": 4.68,
	"AUD": 1.51,
	"GBP": 0.78,
}

// GetExchangeRates fetches real-time exchange rates for the given base currency (e.g. IDR, USD, SGD)
func GetExchangeRates(baseCurrency string) (*FXRatesResponse, error) {
	baseCurrency = strings.ToUpper(strings.TrimSpace(baseCurrency))
	if baseCurrency == "" {
		baseCurrency = "IDR"
	}

	cacheKey := utils.BuildCacheKey("fx_rates", baseCurrency)

	fetchFunc := func() (FXRatesResponse, error) {
		// Attempt fetching from free public Exchange Rate API
		url := fmt.Sprintf("https://open.er-api.com/v6/latest/%s", baseCurrency)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			var apiResult struct {
				Base  string             `json:"base_code"`
				Rates map[string]float64 `json:"rates"`
			}
			if decodeErr := json.NewDecoder(resp.Body).Decode(&apiResult); decodeErr == nil && len(apiResult.Rates) > 0 {
				return FXRatesResponse{
					Base:      apiResult.Base,
					Rates:     apiResult.Rates,
					UpdatedAt: time.Now().Format(time.RFC3339),
				}, nil
			}
		}

		// Fallback calculation using static rates table relative to USD
		usdRate, exists := defaultUSDRates[baseCurrency]
		if !exists || usdRate == 0 {
			usdRate = defaultUSDRates["IDR"]
		}

		convertedRates := make(map[string]float64)
		for code, usdVal := range defaultUSDRates {
			convertedRates[code] = usdVal / usdRate
		}

		return FXRatesResponse{
			Base:      baseCurrency,
			Rates:     convertedRates,
			UpdatedAt: time.Now().Format(time.RFC3339),
		}, nil
	}

	// Cache exchange rates for 12 hours
	rates, err := utils.CacheOrFetch(cacheKey, 12*time.Hour, fetchFunc)
	if err != nil {
		return nil, err
	}

	return &rates, nil
}
