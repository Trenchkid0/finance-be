package utils

import "math"

// RoundToTwoDecimals rounds a float64 value to 2 decimal places
// to prevent floating-point precision issues in financial calculations.
func RoundToTwoDecimals(val float64) float64 {
	return math.Round(val*100) / 100
}
