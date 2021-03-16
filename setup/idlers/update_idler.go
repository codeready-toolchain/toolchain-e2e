package idlers

import (
	"context"
	"fmt"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func UpdateTimeout(cl client.Client, username string, timeout time.Duration) error {
	for _, suffix := range []string{"code", "dev", "stage"} { // TODO: hard coded suffixes, we could probably get them from the tier instead
		idlerName := fmt.Sprintf("%s-%s", username, suffix)
		idler, err := getIdler(cl, idlerName)
		if err != nil {
			return err
		}
		idler.Spec.TimeoutSeconds = int32(timeout.Seconds())
		if err = cl.Update(context.TODO(), idler); err != nil {
			return err
		}
	}
	return nil
}

func getIdler(cl client.Client, name string) (*toolchainv1alpha1.Idler, error) {
	idler := &toolchainv1alpha1.Idler{}
	err := k8swait.Poll(cfg.DefaultRetryInterval, cfg.DefaultTimeout, func() (bool, error) {
		err := cl.Get(context.TODO(), types.NamespacedName{
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
