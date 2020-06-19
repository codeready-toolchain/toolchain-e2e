package e2e

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

func provisioned() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionTrue,
		Reason: "Provisioned",
	}
}

func pendingApproval() []toolchainv1alpha1.Condition {
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

func approvedByAdmin() []toolchainv1alpha1.Condition {
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

func approvedAutomatically() []toolchainv1alpha1.Condition {
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

func approvedAutomaticallyAndBanned() []toolchainv1alpha1.Condition {
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

func banned() []toolchainv1alpha1.Condition {
	return []toolchainv1alpha1.Condition{
		{
			Type:   toolchainv1alpha1.UserSignupComplete,
			Status: corev1.ConditionTrue,
			Reason: "Banned",
		},
	}
}

func disabled() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.ConditionReady,
		Status: corev1.ConditionFalse,
		Reason: "Disabled",
	}
}

func provisionedNotificationCRCreated() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.MasterUserRecordUserProvisionedNotificationCreated,
		Status: corev1.ConditionTrue,
		Reason: "NotificationCRCreated",
	}
}

func sent() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:   toolchainv1alpha1.NotificationSent,
		Status: corev1.ConditionTrue,
		Reason: "Sent",
	}
}
