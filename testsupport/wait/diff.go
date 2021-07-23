package wait

import (
	"bytes"
	"strings"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"

	"github.com/davecgh/go-spew/spew"
	"github.com/sergi/go-diff/diffmatchpatch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	spew.Config.DisablePointerAddresses = true
	spew.Config.DisableCapacities = true
}

func Diff(actual, expected interface{}) string {
	actual = ResetTimeFields(actual)
	expected = ResetTimeFields(expected)
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

func ResetTimeFields(obj interface{}) interface{} {
	switch obj := obj.(type) {
	case []toolchainv1alpha1.Condition:
		result := make([]toolchainv1alpha1.Condition, len(obj))
		for i, c := range obj {
			result[i] = ResetTimeFields(c).(toolchainv1alpha1.Condition)
		}
		return result
	case toolchainv1alpha1.Condition:
		return toolchainv1alpha1.Condition{
			Type:               obj.Type,
			Status:             obj.Status,
			Reason:             obj.Reason,
			Message:            obj.Message,
			LastTransitionTime: metav1.NewTime(time.Time{}),
		}
	case []toolchainv1alpha1.UserAccountStatusEmbedded:
		result := make([]toolchainv1alpha1.UserAccountStatusEmbedded, len(obj))
		for i, c := range obj {
			result[i] = ResetTimeFields(c).(toolchainv1alpha1.UserAccountStatusEmbedded)
		}
		return result
	case toolchainv1alpha1.UserAccountStatusEmbedded:
		return toolchainv1alpha1.UserAccountStatusEmbedded{
			Cluster:   obj.Cluster,
			SyncIndex: obj.SyncIndex,
			UserAccountStatus: toolchainv1alpha1.UserAccountStatus{
				Conditions: ResetTimeFields(obj.UserAccountStatus.Conditions).([]toolchainv1alpha1.Condition),
			},
		}
	default:
		return obj
	}
}
