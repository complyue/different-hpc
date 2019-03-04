package bknd

import (
	"fmt"
	"math"
)

// human readable data size
func HRDSZ(sz int64) string {
	// compare with 999* to avoid scientific notations from appearing
	if float64(sz) > 999*math.Pow(1024, 3) {
		return fmt.Sprintf("%0.1fTB", float64(sz)/math.Pow(1024, 4))
	}
	if float64(sz) > 999*math.Pow(1024, 2) {
		return fmt.Sprintf("%0.1fGB", float64(sz)/math.Pow(1024, 3))
	}
	if float64(sz) > 999*1024 {
		return fmt.Sprintf("%0.1fMB", float64(sz)/math.Pow(1024, 2))
	}
	if float64(sz) > 999 {
		return fmt.Sprintf("%0.1fKB", float64(sz)/1024)
	}
	return fmt.Sprintf("%dByte", sz)
}
