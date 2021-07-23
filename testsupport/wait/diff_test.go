package wait_test

import (
	"fmt"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
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

func TestResetTimeFields(t *testing.T) {

	t.Run("in slice of Condition objects", func(t *testing.T) {
		// given
		now := metav1.NewTime(time.Now())
		c := []toolchainv1alpha1.Condition{
			{
				Type:               toolchainv1alpha1.ChangeTierRequestComplete,
				Status:             v1.ConditionTrue,
				Reason:             "cookie",
				Message:            "chocolate",
				LastTransitionTime: now,
				LastUpdatedTime:    &now,
			},
			{
				Type:               toolchainv1alpha1.MasterUserRecordReady,
				Status:             v1.ConditionTrue,
				Reason:             "pasta",
				Message:            "rice",
				LastTransitionTime: now,
				LastUpdatedTime:    &now,
			},
		}
		// when
		result := wait.ResetTimeFields(c)
		// then
		assert.Equal(t, []toolchainv1alpha1.Condition{
			{
				Type:               toolchainv1alpha1.ChangeTierRequestComplete,
				Status:             v1.ConditionTrue,
				Reason:             "cookie",
				Message:            "chocolate",
				LastTransitionTime: metav1.NewTime(time.Time{}), // reset
				LastUpdatedTime:    nil,                         // reset
			},
			{
				Type:               toolchainv1alpha1.MasterUserRecordReady,
				Status:             v1.ConditionTrue,
				Reason:             "pasta",
				Message:            "rice",
				LastTransitionTime: metav1.NewTime(time.Time{}), // reset
				LastUpdatedTime:    nil,                         // reset
			},
		}, result)
	})

	t.Run("in slice of UserAccountStatusEmbedded objects", func(t *testing.T) {
		// given
		now := metav1.NewTime(time.Now())
		c := []toolchainv1alpha1.UserAccountStatusEmbedded{
			{
				Cluster: toolchainv1alpha1.Cluster{
					Name: "cookie",
				},
				SyncIndex: "1",
				UserAccountStatus: toolchainv1alpha1.UserAccountStatus{
					Conditions: []toolchainv1alpha1.Condition{
						{
							Type:               toolchainv1alpha1.ChangeTierRequestComplete,
							Status:             v1.ConditionTrue,
							Reason:             "cookie",
							Message:            "chocolate",
							LastTransitionTime: now,
							LastUpdatedTime:    &now,
						},
						{
							Type:               toolchainv1alpha1.MasterUserRecordReady,
							Status:             v1.ConditionTrue,
							Reason:             "pasta",
							Message:            "rice",
							LastTransitionTime: now,
							LastUpdatedTime:    &now,
						},
					},
				},
			},
		}

		// when
		result := wait.ResetTimeFields(c)
		// then
		assert.Equal(t, []toolchainv1alpha1.UserAccountStatusEmbedded{
			{
				Cluster: toolchainv1alpha1.Cluster{
					Name: "cookie",
				},
				SyncIndex: "1",
				UserAccountStatus: toolchainv1alpha1.UserAccountStatus{
					Conditions: []toolchainv1alpha1.Condition{
						{
							Type:               toolchainv1alpha1.ChangeTierRequestComplete,
							Status:             v1.ConditionTrue,
							Reason:             "cookie",
							Message:            "chocolate",
							LastTransitionTime: metav1.NewTime(time.Time{}), // reset
							LastUpdatedTime:    nil,                         // reset
						},
						{
							Type:               toolchainv1alpha1.MasterUserRecordReady,
							Status:             v1.ConditionTrue,
							Reason:             "pasta",
							Message:            "rice",
							LastTransitionTime: metav1.NewTime(time.Time{}), // reset
							LastUpdatedTime:    nil,                         // reset
						},
					},
				},
			},
		}, result)
	})

}
