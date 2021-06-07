package wait

import (
	"context"
	"time"

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

func HasSubscriptionWithCondition(cl client.Client, name, namespace string, conditions ...subCheck) (bool, error) {
	sub := &v1alpha1.Subscription{}
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, sub); err != nil {
		return false, err
	}
	for _, cond := range conditions {
		if !cond(sub) {
			return false, nil
		}
	}
	return true, nil
}

func WaitForSubscriptionWithCondition(cl client.Client, name, namespace string, conditions ...subCheck) error {
	if err := k8swait.Poll(configuration.DefaultRetryInterval, configuration.DefaultTimeout, func() (bool, error) {
		return HasSubscriptionWithCondition(cl, name, namespace, conditions...)
	}); err != nil {
		return errors.Wrapf(err, "could not find a Subscription with name '%s' in namespace '%s' that meets the expected conditions", name, namespace)
	}
	return nil
}

func HasCSVWithCondition(cl client.Client, name, namespace string, conditions ...csvCheck) (bool, error) {
	csv := &v1alpha1.ClusterServiceVersion{}
	if err := cl.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, csv); err != nil {
		return false, nil
	}
	for _, cond := range conditions {
		if !cond(csv) {
			return false, nil
		}
	}
	return true, nil
}

func WaitForCSVWithCondition(cl client.Client, name, namespace string, conditions ...csvCheck) error {
	if err := k8swait.Poll(configuration.DefaultRetryInterval, 20*time.Second, func() (bool, error) {
		return HasCSVWithCondition(cl, name, namespace, conditions...)
	}); err != nil {
		return errors.Wrapf(err, "could not find a CSV with name '%s' in namespace '%s' that meets the expected conditions", name, namespace)
	}
	return nil
}

type csvCheck func(csv *v1alpha1.ClusterServiceVersion) bool

type subCheck func(csv *v1alpha1.Subscription) bool
