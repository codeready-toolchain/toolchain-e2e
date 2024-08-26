package util

import (
	"testing"
	"time"
)

func LogWithTimestamp(t *testing.T, message string) {
	time := time.Now().Format("2006-01-02 15:04:05")
	t.Logf("[%s] %s", time, message)
}
