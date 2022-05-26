package wait

import (
	"strings"

	"github.com/google/go-cmp/cmp"
)

func Diff(expected, actual interface{}) string {
	msg := &strings.Builder{}
	msg.WriteString("-expected\n")
	msg.WriteString("+actual\n")
	msg.WriteString(cmp.Diff(expected, actual))
	return msg.String()
}
