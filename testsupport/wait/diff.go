package wait

import (
	"bytes"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func init() {
	spew.Config.DisablePointerAddresses = true
	spew.Config.DisableCapacities = true
}

func Diff(actual, expected interface{}) string {
	dmp := diffmatchpatch.New()
	actualdmp, expecteddmp, dmpStrings := dmp.DiffLinesToChars(spew.Sdump(actual), spew.Sdump(expected))
	diffs := dmp.DiffMain(actualdmp, expecteddmp, false)
	diffs = dmp.DiffCharsToLines(diffs, dmpStrings)
	return diffPrettyText(diffs)
}

// DiffPrettyText converts a []Diff into a colored text report.
func diffPrettyText(diffs []diffmatchpatch.Diff) string {
	var buff bytes.Buffer

	_, _ = buff.WriteString("\x1b[31m")
	_, _ = buff.WriteString("-actual\n")
	_, _ = buff.WriteString("\x1b[0m")
	_, _ = buff.WriteString("\x1b[32m")
	_, _ = buff.WriteString("+expected\n")
	_, _ = buff.WriteString("\x1b[0m")
	for _, diff := range diffs {
		text := diff.Text

		switch diff.Type {
		case diffmatchpatch.DiffInsert:
			_, _ = buff.WriteString("\x1b[32m")
			_, _ = buff.WriteString(strings.Replace(text, " ", "+", 1))
			_, _ = buff.WriteString("\x1b[0m")
		case diffmatchpatch.DiffDelete:
			_, _ = buff.WriteString("\x1b[31m")
			_, _ = buff.WriteString(strings.Replace(text, " ", "-", 1))
			_, _ = buff.WriteString("\x1b[0m")
		case diffmatchpatch.DiffEqual:
			_, _ = buff.WriteString(text)
		}
	}

	return buff.String()
}
