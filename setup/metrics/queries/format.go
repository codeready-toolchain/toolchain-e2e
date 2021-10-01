package queries

import "fmt"

const MB = 1 << 20

func bytesToMBString(bytes float64) string {
	return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
}

func simple(value float64) string {
	return fmt.Sprintf("%f", value)
}

func utilisation(value float64) string {
	return fmt.Sprintf("%.2f %%", value*100)
}
