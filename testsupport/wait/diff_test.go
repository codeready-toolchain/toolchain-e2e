package wait_test

import (
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDiff(t *testing.T) {

	t.Run("on single condition", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		actual := toolchainv1alpha1.Condition{
			Type:               toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status:             v1.ConditionTrue,
			Reason:             "a reason",
			Message:            "a message",
			LastTransitionTime: now,
			LastUpdatedTime:    &now,
		}
		expected := toolchainv1alpha1.Condition{
			Type:               toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status:             v1.ConditionTrue,
			Reason:             "another reason",
			Message:            "another message",
			LastTransitionTime: now,
			LastUpdatedTime:    &now,
		}
		t.Logf("expected conditions to match:\n%s", wait.Diff(expected, actual))
	})

	t.Run("on multiple conditions", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		actual := []toolchainv1alpha1.Condition{
			{
				Type:               toolchainv1alpha1.ConditionReady,
				Status:             v1.ConditionFalse,
				Reason:             "Provisioning",
				LastTransitionTime: now,
			},
		}
		expected := []toolchainv1alpha1.Condition{
			{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: v1.ConditionTrue,
				Reason: "Provisioned",
			},
			{
				Type:   toolchainv1alpha1.MasterUserRecordUserProvisionedNotificationCreated,
				Status: v1.ConditionTrue,
				Reason: toolchainv1alpha1.MasterUserRecordNotificationCRCreatedReason,
			},
		}
		t.Logf("expected conditions to match:\n%s", wait.Diff(expected, actual))
	})
}
