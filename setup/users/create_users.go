package users

import (
	"context"
	"fmt"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var memberClusterName string

func Create(cl client.Client, username, hostOperatorNamespace, memberOperatorNamespace string) error {
	memberClusterName, err := getMemberClusterName(cl, hostOperatorNamespace, memberOperatorNamespace)
	if err != nil {
		return fmt.Errorf("unable to lookup member cluster name, ensure the sandbox setup steps are followed")
	}
	usersignup := &toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hostOperatorNamespace,
			Name:      username,
			Labels: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailHashLabelKey: hash.EncodeString(fmt.Sprintf("%s@fake.test", username)),
			},
		},
		Spec: toolchainv1alpha1.UserSignupSpec{
			TargetCluster: memberClusterName,
			IdentityClaims: toolchainv1alpha1.IdentityClaimsEmbedded{
				PropagatedClaims: toolchainv1alpha1.PropagatedClaims{
					Email: fmt.Sprintf("%s@fake.test", username),
					Sub:   username,
				},
				PreferredUsername: username,
			},
		},
	}
	states.SetApprovedManually(usersignup, true)

	return cl.Create(context.TODO(), usersignup)
}

func getMemberClusterName(cl client.Client, hostOperatorNamespace, memberOperatorNamespace string) (string, error) {
	if memberClusterName != "" {
		return memberClusterName, nil
	}
	var memberCluster toolchainv1alpha1.ToolchainCluster
	err := k8swait.Poll(configuration.DefaultRetryInterval, configuration.DefaultTimeout, func() (bool, error) {
		clusters := &toolchainv1alpha1.ToolchainClusterList{}
		if err := cl.List(context.TODO(), clusters, client.InNamespace(hostOperatorNamespace)); err != nil {
			return false, err
		}
		for _, cluster := range clusters.Items {
			if cluster.Status.OperatorNamespace == memberOperatorNamespace && condition.IsTrue(cluster.Status.Conditions, toolchainv1alpha1.ConditionReady) {
				memberCluster = cluster
				return true, nil
			}
		}
		return false, nil
	})
	return memberCluster.Name, err
}
