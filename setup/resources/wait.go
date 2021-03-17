package resources

import (
	"context"

	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func WaitForNamespace(cl client.Client, namespace string) error {
	ns := &corev1.Namespace{}
	if err := k8swait.Poll(configuration.DefaultRetryInterval, configuration.DefaultTimeout, func() (bool, error) {
		err := cl.Get(context.TODO(), types.NamespacedName{
			Name: namespace,
		}, ns)
		if k8serrors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		return errors.Wrapf(err, "namespace '%s' does not exist", namespace)
	}
	return nil
}
