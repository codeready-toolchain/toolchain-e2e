package testsupport

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"

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
	}
}

func VerificationRequired() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupApproved,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.UserSignupApprovedByAdminReason,
		},
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionFalse,
			Reason: toolchainv1alpha1.UserSignupVerificationRequiredReason,
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

func ToolchainStatusReady() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "AllComponentsReady",
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
	}
}
