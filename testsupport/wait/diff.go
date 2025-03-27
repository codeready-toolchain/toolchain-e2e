package wait

import (
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
)

func Diff(expected, actual interface{}) string {
	msg := &strings.Builder{}

	fmt.Fprintln(msg, "\nexpected:")
	fmt.Fprintln(msg, "----")
	fmt.Fprintf(msg, "%s", expected)
	fmt.Fprintln(msg, "----")
	fmt.Fprintln(msg, "\nactual:")
	fmt.Fprintln(msg, "----")
	fmt.Fprintf(msg, "%s", actual)
	fmt.Fprintln(msg, "----")
	fmt.Fprintln(msg, "-expected")
	fmt.Fprintln(msg, "+actual")
	msg.WriteString(cmp.Diff(expected, actual))
	return msg.String()
}
