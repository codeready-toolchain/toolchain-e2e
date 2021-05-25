package testsupport

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

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

func PendingApprovalNoCluster() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionFalse,
			Reason: "PendingApproval",
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: "NoClusterAvailable",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserNotInPreDeactivation",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
		},
	}
}

func ApprovedByAdmin() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		},
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

func ApprovedByAdminNoCluster() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedByAdmin",
		},
		{
			Type:    toolchainv1alpha1.UserSignupComplete,
			Status:  corev1.ConditionFalse,
			Reason:  "NoClusterAvailable",
			Message: "no suitable member cluster found - capacity was reached",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserNotInPreDeactivation",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
		},
	}
}

func ApprovedAutomatically() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserNotInPreDeactivation",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
		},
	}
}

func ApprovedAutomaticallyAndBanned() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: "ApprovedAutomatically",
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: "Banned",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserNotInPreDeactivation",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
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

func Banned() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: "Banned",
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
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupApprovedByAdminReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserIsActive",
		},
	}
}

func DeactivatedWithoutPreDeactivation() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupApprovedByAdminReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupUserDeactivatedReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatingNotificationCreated,
			Status: corev1.ConditionFalse,
			Reason: "UserNotInPreDeactivation",
		},
		{
			Type:   toolchainv1alpha1.UserSignupUserDeactivatedNotificationCreated,
			Status: corev1.ConditionTrue,
			Reason: "NotificationCRCreated",
		},
	}
}

func Deactivated() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupApprovedByAdminReason,
		},
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
	}
}

func Running() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "Running",
	}
}

func Complete() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.UserSignupComplete,
		Status: corev1.ConditionTrue,
	}
}
