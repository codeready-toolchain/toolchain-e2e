package metrics

import "fmt"

const MB = 1 << 20

func bytesToMBString(bytes float64) string {
	return fmt.Sprintf("%.2f MB\n", float64(bytes)/MB)
}
