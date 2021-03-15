package user

import (
	"context"
	"fmt"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Setup struct {
	Config *rest.Config
	Client client.Client
}

func (s Setup) UpdateUserIdlersTimeout(username string, timeout time.Duration) error {
	for _, suffix := range []string{"code", "dev", "stage"} { // TODO: hard coded suffixes, we could probably get them from the tier instead
		idlerName := fmt.Sprintf("%s-%s", username, suffix)
		idler, err := s.getIdler(idlerName)
		if err != nil {
			return err
		}
		idler.Spec.TimeoutSeconds = int32(timeout.Seconds())
		if err = s.Client.Update(context.TODO(), idler); err != nil {
			return err
		}
	}
	return nil
}

func (s Setup) getIdler(name string) (*toolchainv1alpha1.Idler, error) {
	idler := &toolchainv1alpha1.Idler{}
	err := k8swait.Poll(cfg.DefaultRetryInterval, cfg.DefaultTimeout, func() (bool, error) {
		err := s.Client.Get(context.TODO(), types.NamespacedName{
			Name: name,
		}, idler)
		if errors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		// check the status conditions, wait until the idler is "Ready/True"
		return test.ContainsCondition(idler.Status.Conditions, testsupport.Running()), nil

	})
	return idler, err
}

func (s Setup) GetMemberClusterName(hostOperatorNamespace, memberOperatorNamespace string) (string, error) {
	var memberCluster v1alpha1.ToolchainCluster
	err := k8swait.Poll(time.Millisecond*100, time.Minute*1, func() (bool, error) {
		clusters := &v1alpha1.ToolchainClusterList{}
		if err := s.Client.List(context.TODO(), clusters, client.InNamespace(hostOperatorNamespace), client.MatchingLabels{
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

func (s Setup) WaitForNamespace(namespace string) error {
	ns := &corev1.Namespace{}
	err := k8swait.Poll(cfg.DefaultRetryInterval, cfg.DefaultTimeout, func() (bool, error) {
		err := s.Client.Get(context.TODO(), types.NamespacedName{
			Name: namespace,
		}, ns)
		if errors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	})
	return err
}

func containsClusterCondition(conditions []v1alpha1.ToolchainClusterCondition, contains *v1alpha1.ToolchainClusterCondition) bool {
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
