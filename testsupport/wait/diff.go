package wait

import (
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
	dmp := diffmatchpatch.New()
	actual = ResetTimeFields(actual)
	expected = ResetTimeFields(expected)
	diffs := dmp.DiffMain(spew.Sdump(actual), spew.Sdump(expected), true)
	return dmp.DiffPrettyText(diffs)
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
