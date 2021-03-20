package resources

import (
	"context"
	"strings"

	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
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

func HasCSVWithPrefix(cl client.Client, prefix, namespace string) (bool, error) {
	csvList := &v1alpha1.ClusterServiceVersionList{}
	if err := cl.List(context.TODO(), csvList, client.InNamespace(namespace)); err != nil {
		return false, err
	}
	for _, csv := range csvList.Items {
		if strings.HasPrefix(csv.Name, prefix) {
			return true, nil
		}
	}
	return false, nil
}

func WaitForCSVWithPrefix(cl client.Client, prefix, namespace string) error {
	if err := k8swait.Poll(configuration.DefaultRetryInterval, configuration.DefaultTimeout, func() (bool, error) {
		return HasCSVWithPrefix(cl, prefix, namespace)
	}); err != nil {
		return errors.Wrapf(err, "could not find the expected CSV '%s' in namespace '%s'", prefix, namespace)
	}
	return nil
}
