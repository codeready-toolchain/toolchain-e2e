package wait

import (
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
)

func Diff(expected, actual interface{}) string {
	msg := &strings.Builder{}

	msg.WriteString("\nexpected:\n")
	msg.WriteString("----\n")
	msg.WriteString(fmt.Sprintf("%s", expected))
	msg.WriteString("----\n")
	msg.WriteString("actual:\n")
	msg.WriteString("----\n")
	msg.WriteString(fmt.Sprintf("%s", actual))
	msg.WriteString("----\n")

	msg.WriteString("-expected\n")
	msg.WriteString("+actual\n")
	msg.WriteString(cmp.Diff(expected, actual))
	return msg.String()
}
