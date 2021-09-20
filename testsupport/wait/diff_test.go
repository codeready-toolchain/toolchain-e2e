package wait_test

import (
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDiff(t *testing.T) {
	now := metav1.NewTime(time.Now())
	actual := toolchainv1alpha1.Condition{
		Type:               toolchainv1alpha1.ChangeTierRequestComplete,
		Status:             v1.ConditionTrue,
		Reason:             "cookie",
		Message:            "cookie",
		LastTransitionTime: now,
		LastUpdatedTime:    &now,
	}
	expected := toolchainv1alpha1.Condition{
		Type:               toolchainv1alpha1.ChangeTierRequestComplete,
		Status:             v1.ConditionTrue,
		Reason:             "chocolate",
		Message:            "chocolate",
		LastTransitionTime: now,
		LastUpdatedTime:    &now,
	}
	t.Log(fmt.Sprintf("expected conditions to match:\n%s", wait.Diff(actual, expected)))
}
