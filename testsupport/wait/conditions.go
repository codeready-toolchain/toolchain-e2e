package wait

import (
	"fmt"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

// ConditionSet may be used to combine separate arrays of conditions into a single array, allowing the condition
// functions in this package to be "mixed and matched" to achieve the desired set of conditions.  Any conditions
// contained within the earlier parameters passed to this function will be overridden by those in later parameters if
// there exists a condition of the same type.
//
// Usage example:
//
// ConditionSet(Default(), wait.Provisioned()) - returns an array of conditions that contains the condition types:
// ConditionReady										true		"Provisioned"
// UserSignupUserDeactivatedNotificationCreated		false		"UserIsActive"
// UserSignupUserDeactivatingNotificationCreated		false		"UserNotInPreDeactivation"
func ConditionSet(conditions ...[]toolchainv1alpha1.Condition) []toolchainv1alpha1.Condition {
	conditionSet := make(map[toolchainv1alpha1.ConditionType]toolchainv1alpha1.Condition)

	for _, conds := range conditions {
		for _, cond := range conds {
			conditionSet[cond.Type] = cond
		}
	}

	var result []toolchainv1alpha1.Condition
	for _, v := range conditionSet {
		result = append(result, v)
	}
	return result
}

// Default defines default values for the two deactivation notification conditions that most tests expect to be
// present within a UserSignup Status
func Default() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserNotInPreDeactivation",
		},
	}
}

func Provisioned() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "Provisioned",
	}
}

func PendingApproval() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
	}
}

func ApprovedAutomatically() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupApprovedAutomaticallyReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		},
	}
}

func ApprovedByAdmin() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupApprovedByAdminReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		},
	}
}

func PendingApprovalNoCluster() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "NoClusterAvailable",
		},
	}
}

func ApprovedByAdminNoCluster() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:    toolchainv1alpha1.UserSignupComplete,
			Status:  corev1.ConditionFalse,
			Reason:  "NoClusterAvailable",
			Message: "no suitable member cluster found - capacity was reached",
		},
	}
}

func Banned() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: "Banned",
		},
	}
}

func VerificationRequired() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "VerificationRequired",
		},
	}
}

func Disabled() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionFalse,
		Reason: "Disabled",
	}
}

func ProvisionedNotificationCRCreated() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.MasterUserRecordUserProvisionedNotificationCreated,
		Status: corev1.ConditionTrue,
		Reason: "NotificationCRCreated",
	}
}

func Sent() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.NotificationSent,
		Status: corev1.ConditionTrue,
		Reason: "Sent",
	}
}

func ToolchainStatusReadyAndUnreadyNotificationNotCreated() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		ToolchainStatusReady(),
		ToolchainStatusUnreadyNotificationNotCreated(),
	}
}

func ToolchainStatusReady() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "AllComponentsReady",
	}
}

func ToolchainStatusComponentsNotReady(componentNames string) toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.ToolchainStatusComponentsNotReadyReason,
		Message: fmt.Sprintf("components not ready: %s", componentNames),
	}
}

func ToolchainStatusUnreadyNotificationNotCreated() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ToolchainStatusUnreadyNotificationCreated,
		Status: corev1.ConditionFalse,
		Reason: "AllComponentsReady",
	}
}

func RoutesAvailable() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "RoutesAvailable",
	}
}

func Deactivating() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
	}
}

func DeactivatedWithoutPreDeactivation() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
		{
			Type:    toolchainv1alpha1.UserSignupApproved,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
	}
}

func DeactivatedWithoutNotification() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
		},
		{
			Type:    toolchainv1alpha1.UserSignupApproved,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
	}
}

func ManuallyDeactivated() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
		{
			Type:    toolchainv1alpha1.UserSignupApproved,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
	}
}

func Deactivated() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
		{
			Type:    toolchainv1alpha1.UserSignupApproved,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
	}
}

func UserSignupMissingEmailAnnotation() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:    toolchainv1alpha1.UserSignupComplete,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.UserSignupMissingUserEmailAnnotationReason,
			Message: "missing annotation at usersignup",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: toolchainv1alpha1.UserSignupDeactivatedNotificationUserIsActiveReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: toolchainv1alpha1.UserSignupDeactivatingNotificationUserNotInPreDeactivationReason,
		},
	}
}

func Running() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "Running",
	}
}

func IdledNotificationCreated() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.IdlerTriggeredNotificationCreated,
		Status: corev1.ConditionTrue,
		Reason: toolchainv1alpha1.IdlerTriggeredReason,
	}
}

func Complete() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.UserSignupComplete,
		Status: corev1.ConditionTrue,
	}
}

func ToolchainConfigSyncComplete() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ToolchainConfigSyncComplete,
		Status: corev1.ConditionTrue,
		Reason: toolchainv1alpha1.ToolchainConfigSyncedReason,
	}
}

func UnableToDeleteUserAccount() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.MasterUserRecordUnableToDeleteUserAccountsReason,
		Message: "UserAccount deletion has not completed in over 1 minute",
	}
}

func TerminatingUserAccount() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:    toolchainv1alpha1.ConditionReady,
		Status:  corev1.ConditionFalse,
		Reason:  toolchainv1alpha1.UserAccountTerminatingReason,
		Message: "deleting NSTemplateSet",
	}
}

func TerminatingNSTemplateSet() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionFalse,
		Reason: toolchainv1alpha1.NSTemplateSetTerminatingReason,
	}
}

func TerminatingSpace() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionFalse,
		Reason: toolchainv1alpha1.SpaceTerminatingReason,
	}
}
