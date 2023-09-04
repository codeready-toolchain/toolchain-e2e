package spacebinding

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SpaceBindingRequestOption func(request *toolchainv1alpha1.SpaceBindingRequest) //nolint:revive

func WithSpecSpaceRole(spaceRole string) SpaceBindingRequestOption {
	return func(s *toolchainv1alpha1.SpaceBindingRequest) {
		s.Spec.SpaceRole = spaceRole
	}
}

func WithSpecMasterUserRecord(mur string) SpaceBindingRequestOption {
	return func(s *toolchainv1alpha1.SpaceBindingRequest) {
		s.Spec.MasterUserRecord = mur
	}
}

func WithNamespace(ns string) SpaceBindingRequestOption {
	return func(s *toolchainv1alpha1.SpaceBindingRequest) {
		s.ObjectMeta.Namespace = ns
	}
}

func CreateSpaceBindingRequest(t *testing.T, awaitilities wait.Awaitilities, memberName string, opts ...SpaceBindingRequestOption) *toolchainv1alpha1.SpaceBindingRequest {
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
