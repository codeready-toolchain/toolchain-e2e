package users

import (
	"context"
	"fmt"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var memberClusterName string

func Create(cl client.Client, username, hostOperatorNamespace, memberOperatorNamespace string) error {
	if memberClusterName == "" {
		var err error
		if memberClusterName, err = getMemberClusterName(cl, hostOperatorNamespace, memberOperatorNamespace); err != nil {
			return fmt.Errorf("unable to lookup member cluster name, ensure the sandbox setup steps are followed")
		}
	}
	usersignup := &toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hostOperatorNamespace,
			Name:      username,
			Annotations: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailAnnotationKey: fmt.Sprintf("%s@test.com", username),
			},
			Labels: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailHashLabelKey: md5.CalcMd5(fmt.Sprintf("%s@test.com", username)),
			},
		},
		Spec: toolchainv1alpha1.UserSignupSpec{
			Username:      username,
			UserID:        username,
			Approved:      true,
			TargetCluster: memberClusterName,
		},
	}

	return cl.Create(context.TODO(), usersignup)
}

func getMemberClusterName(cl client.Client, hostOperatorNamespace, memberOperatorNamespace string) (string, error) {
	var memberCluster toolchainv1alpha1.ToolchainCluster
	err := k8swait.Poll(time.Millisecond*100, time.Minute*1, func() (bool, error) {
		clusters := &toolchainv1alpha1.ToolchainClusterList{}
		if err := cl.List(context.TODO(), clusters, client.InNamespace(hostOperatorNamespace), client.MatchingLabels{
			"namespace": memberOperatorNamespace,
			"type":      "member",
		}); err != nil {
			return false, err
		}
		for _, cluster := range clusters.Items {
			if containsClusterCondition(cluster.Status.Conditions, wait.ReadyToolchainCluster) {
				memberCluster = cluster
				return true, nil
			}
		}
		return false, nil
	})
	return memberCluster.Name, err
}

func containsClusterCondition(conditions []toolchainv1alpha1.ToolchainClusterCondition, contains *toolchainv1alpha1.ToolchainClusterCondition) bool {
	if contains == nil {
		return true
	}
	for _, c := range conditions {
		if c.Type == contains.Type {
			return contains.Status == c.Status
		}
	}
	return false
}
