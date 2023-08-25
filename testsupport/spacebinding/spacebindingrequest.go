package spacebinding

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type RequestOption func(request *toolchainv1alpha1.SpaceBindingRequest)

func WithSpecSpaceRole(spaceRole string) RequestOption {
	return func(s *toolchainv1alpha1.SpaceBindingRequest) {
		s.Spec.SpaceRole = spaceRole
	}
}

func WithSpecMasterUserRecord(mur string) RequestOption {
	return func(s *toolchainv1alpha1.SpaceBindingRequest) {
		s.Spec.MasterUserRecord = mur
	}
}

func WithNamespace(ns string) RequestOption {
	return func(s *toolchainv1alpha1.SpaceBindingRequest) {
		s.ObjectMeta.Namespace = ns
	}
}

func CreateSpaceBindingRequest(t *testing.T, awaitilities wait.Awaitilities, memberName string, opts ...RequestOption) *toolchainv1alpha1.SpaceBindingRequest {
	memberAwait, err := awaitilities.Member(memberName)
	require.NoError(t, err)
	namePrefix := util.NewObjectNamePrefix(t)

	spaceBindingRequest := &toolchainv1alpha1.SpaceBindingRequest{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namePrefix + "-",
		},
	}
	for _, apply := range opts {
		apply(spaceBindingRequest)
	}
	err = memberAwait.CreateWithCleanup(t, spaceBindingRequest)
	require.NoError(t, err)

	return spaceBindingRequest
}
