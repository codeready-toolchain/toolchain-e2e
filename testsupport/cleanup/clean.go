package cleanup

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/davecgh/go-spew/spew"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultRetryInterval = time.Millisecond * 100 // make it short because a "retry interval" is waited before the first test
	defaultTimeout       = time.Second * 60
)

var (
	propagationPolicy     = metav1.DeletePropagationForeground
	propagationPolicyOpts = client.DeleteOption(&client.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	})
)

type cleanManager struct {
	sync.RWMutex
	cleanTasks map[*testing.T][]*cleanTask
}

var cleaning = &cleanManager{
	cleanTasks: map[*testing.T][]*cleanTask{},
}

type AwaitilityInt interface {
	GetClient() client.Client
}

// AddCleanTasks adds cleaning tasks for the given objects that will be automatically performed at the end of the test execution
func AddCleanTasks(t *testing.T, cl client.Client, objects ...client.Object) {
	AddCleanTasksWithTimeout(t, cl, defaultTimeout, objects...)
}

func AddCleanTasksWithTimeout(t *testing.T, cl client.Client, timeout time.Duration, objects ...client.Object) {
	cleaning.addCleanTasks(t, cl, timeout, objects...)
}

func (c *cleanManager) addCleanTasks(t *testing.T, cl client.Client, timeout time.Duration, objects ...client.Object) {
	c.Lock()
	defer c.Unlock()
	for _, obj := range objects {
		if len(c.cleanTasks[t]) == 0 {
			t.Cleanup(c.clean(t))
		}
		c.cleanTasks[t] = append(c.cleanTasks[t], newCleanTask(t, cl, obj, timeout))
	}
}

// ExecuteAllCleanTasks triggers cleanup of all resources that were marked to be cleaned before that
func ExecuteAllCleanTasks(t *testing.T) {
	cleaning.clean(t)()
}

func (c *cleanManager) clean(t *testing.T) func() {
	return func() {
		c.Lock()
		defer c.Unlock()
		if !t.Failed() {
			var wg sync.WaitGroup
			for _, task := range c.cleanTasks[t] {
				wg.Add(1)
				go func(cleanTask *cleanTask) {
					defer wg.Done()
					cleanTask.clean()
				}(task)
			}
			wg.Wait()
		} else {
			t.Logf(
				"skipping object cleanup, test=%s failedTimestamp=%s",
				t.Name(),
				time.Now().Format(time.StampMilli),
			)
		}
		c.cleanTasks[t] = nil
	}
}

type cleanTask struct {
	sync.Once
	objToClean client.Object
	client     client.Client
	t          *testing.T
	timeout    time.Duration
}

func (c *cleanTask) clean() {
	c.Do(c.cleanObject)
}

func newCleanTask(t *testing.T, cl client.Client, obj client.Object, timeout time.Duration) *cleanTask {
	return &cleanTask{
		t:          t,
		client:     cl,
		objToClean: obj,
		timeout:    timeout,
	}
}

func (c *cleanTask) cleanObject() {
	if c.objToClean == nil {
		return
	}
	objToClean, ok := c.objToClean.DeepCopyObject().(client.Object)
	require.True(c.t, ok)
	userSignup, isUserSignup := c.objToClean.(*toolchainv1alpha1.UserSignup)
	nsTemplateTier, isNsTemplateTier := c.objToClean.(*toolchainv1alpha1.NSTemplateTier)
	kind := objToClean.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = reflect.TypeOf(c.objToClean).Elem().Name()
	}
	c.t.Logf("deleting %s: %s ...", kind, objToClean.GetName())
	if err := c.client.Delete(context.TODO(), objToClean, propagationPolicyOpts); err != nil {
		if errors.IsNotFound(err) {
			// if the object was UserSignup, then let's check that the MUR was deleted as well
			murDeleted, err := c.verifyMurDeleted(isUserSignup, userSignup, true)
			require.NoError(c.t, err)
			// if the object was UserSignup, then let's check that the Space was deleted as well
			spaceDeleted, err := c.verifySpaceDeleted(isUserSignup, userSignup, true)
			require.NoError(c.t, err)
			// either if it was deleted or if it wasn't UserSignup, then return here
			if murDeleted && spaceDeleted {
				c.t.Logf("%s: %s was already deleted", kind, objToClean.GetName())
				return
			}
		}
	}
	// if the object was NSTemplateTier, then let's check that the TierTemplateRevisions were deleted as well
	_, err := c.verifyTierTemplateRevisionsDeleted(isNsTemplateTier, nsTemplateTier, true)
	require.NoError(c.t, err)

	// wait until deletion is done
	c.t.Logf("waiting until %s: %s is completely deleted", kind, objToClean.GetName())
	err = wait.PollUntilContextTimeout(context.TODO(), defaultRetryInterval, c.timeout, true, func(ctx context.Context) (done bool, err error) {
		if err := c.client.Get(context.TODO(), test.NamespacedName(objToClean.GetNamespace(), objToClean.GetName()), objToClean); err != nil {
			if errors.IsNotFound(err) {
				// if the object was UserSignup, then let's check that the MUR is deleted as well
				if murDeleted, err := c.verifyMurDeleted(isUserSignup, userSignup, false); !murDeleted || err != nil {
					return false, err
				}
				// if the object was UserSignup, then let's check that the Space is deleted as well
				if spaceDeleted, err := c.verifySpaceDeleted(isUserSignup, userSignup, false); !spaceDeleted || err != nil {
					return false, err
				}
				// if the object was NSTemplateTier, then let's check that the TTRs were deleted as well
				if ttrsDeleted, err := c.verifyTierTemplateRevisionsDeleted(isNsTemplateTier, nsTemplateTier, false); !ttrsDeleted || err != nil {
					return false, err
				}
				return true, nil
			}
			c.t.Logf("problem with getting the related %s '%s': %s", kind, objToClean.GetName(), err)
			return false, err
		}
		// if the object was NSTemplateTier and is still not deleted, prod it to retry the finalization attempt
		c.prodTierDeletion(objToClean)

		return false, nil
	})
	if err != nil {
		if isUserSignup {
			message := spew.Sprintf("The proper cleanup of the UserSignup '%s' and related resources wasn't finished within the given timeout\n", objToClean.GetName())

			message += c.checkIfStillPresent(&toolchainv1alpha1.UserSignup{}, "UserSignup", userSignup.GetNamespace(), userSignup.Name)
			if userSignup.Status.CompliantUsername != "" {
				message += c.checkIfStillPresent(&toolchainv1alpha1.MasterUserRecord{}, "MasterUserRecord", userSignup.GetNamespace(), userSignup.Status.CompliantUsername)
				message += c.checkIfStillPresent(&toolchainv1alpha1.Space{}, "Space", userSignup.GetNamespace(), userSignup.Status.CompliantUsername)
			}
			require.NoError(c.t, err, message)
		} else if isNsTemplateTier {
			message := spew.Sprintf("The proper cleanup of the NSTemplateTier '%s' and related resources wasn't finished within the given timeout\n", objToClean.GetName())
			message += c.checkIfStillPresent(&toolchainv1alpha1.NSTemplateTier{}, "NSTemplateTier", nsTemplateTier.GetNamespace(), nsTemplateTier.Name)
			ttrs := &toolchainv1alpha1.TierTemplateRevisionList{}
			if err := c.client.List(context.TODO(), ttrs,
				client.InNamespace(nsTemplateTier.GetNamespace()),
				client.MatchingLabels{toolchainv1alpha1.TierLabelKey: nsTemplateTier.GetName()}); err != nil {
				message += fmt.Sprintf("unexpected error when getting the TTR CRs: %s \n", err.Error())
			}
			for _, ttr := range ttrs.Items {
				message += fmt.Sprintf("the TTR CR '%s' is still present in the cluster: %+v \n", ttr.Name, ttr)
			}
			require.NoError(c.t, err, message)
		} else {
			require.NoError(c.t, err, "The object still exists after the time out expired: %s", spew.Sdump(objToClean))
		}
	}
}

func (c *cleanTask) checkIfStillPresent(obj client.Object, kind, namespace, name string) string {
	err := c.client.Get(context.TODO(), test.NamespacedName(namespace, name), obj)
	if err == nil {
		return fmt.Sprintf("the %s '%s' is still present in the cluster: %+v \n", kind, name, obj)
	} else if !errors.IsNotFound(err) {
		return fmt.Sprintf("unexpected error when getting the %s '%s': %s \n", kind, name, err.Error())
	}
	return fmt.Sprintf("the %s '%s' has been cleaned properly \n", kind, name)
}

func (c *cleanTask) verifyMurDeleted(isUserSignup bool, userSignup *toolchainv1alpha1.UserSignup, delete bool) (bool, error) {
	// only applicable for UserSignups with compliant username set
	if isUserSignup {
		if userSignup.Status.CompliantUsername != "" {
			mur := &toolchainv1alpha1.MasterUserRecord{}
			if err := c.client.Get(context.TODO(), test.NamespacedName(userSignup.GetNamespace(), userSignup.Status.CompliantUsername), mur); err != nil {
				// if MUR is not found then we are good
				if errors.IsNotFound(err) {
					// c.t.Logf("the related MasterUserRecord: %s is deleted as well", userSignup.Status.CompliantUsername)
					return true, nil
				}
				c.t.Logf("problem with getting the related MasterUserRecord %s: %s", userSignup.Status.CompliantUsername, err)
				return false, err
			}
			if delete {
				c.t.Logf("deleting also the related MasterUserRecord: %s", userSignup.Status.CompliantUsername)
				if err := c.client.Delete(context.TODO(), mur, propagationPolicyOpts); err != nil {
					if errors.IsNotFound(err) {
						// c.t.Logf("the related MasterUserRecord: %s is deleted as well", userSignup.Status.CompliantUsername)
						return true, nil
					}
					c.t.Logf("problem with deleting the related MasterUserRecord %s: %s", userSignup.Status.CompliantUsername, err)
					return false, err
				}
			}
			c.t.Logf("waiting until MasterUserRecord: %s is completely deleted", userSignup.Status.CompliantUsername)
			return false, nil
		}
		c.t.Logf("the UserSignup %s doesn't have CompliantUsername set", userSignup.Name)
		return true, nil
	}
	return true, nil
}

func (c *cleanTask) verifySpaceDeleted(isUserSignup bool, userSignup *toolchainv1alpha1.UserSignup, delete bool) (bool, error) {
	// only applicable for UserSignups with compliant username set
	if isUserSignup {
		if userSignup.Status.CompliantUsername != "" {
			space := &toolchainv1alpha1.Space{}
			if err := c.client.Get(context.TODO(), test.NamespacedName(userSignup.GetNamespace(), userSignup.Status.CompliantUsername), space); err != nil {
				// if Space is not found then we are good
				if errors.IsNotFound(err) {
					// c.t.Logf("the related Space: %s is deleted as well", userSignup.Status.CompliantUsername)
					return true, nil
				}
				c.t.Logf("problem with getting the related Space %s: %s", userSignup.Status.CompliantUsername, err)
				return false, err
			}
			if delete {
				c.t.Logf("deleting also the related Space: %s", userSignup.Status.CompliantUsername)
				if err := c.client.Delete(context.TODO(), space, propagationPolicyOpts); err != nil {
					if errors.IsNotFound(err) {
						// c.t.Logf("the related Space: %s is deleted as well", userSignup.Status.CompliantUsername)
						return true, nil
					}
					c.t.Logf("problem with deleting the related Space %s: %s", userSignup.Status.CompliantUsername, err)
					return false, err
				}
			}
			// c.t.Logf("waiting until Space: %s is completely deleted", userSignup.Status.CompliantUsername)
			return false, nil
		}
		c.t.Logf("the UserSignup %s doesn't have CompliantUsername set", userSignup.Name)
		return true, nil
	}
	return true, nil
}

func (c *cleanTask) verifyTierTemplateRevisionsDeleted(isNsTemplateTier bool, nsTemplateTier *toolchainv1alpha1.NSTemplateTier, delete bool) (bool, error) {
	if !isNsTemplateTier {
		return true, nil
	}
	ttrs := &toolchainv1alpha1.TierTemplateRevisionList{}
	if err := c.client.List(context.TODO(), ttrs,
		client.InNamespace(nsTemplateTier.GetNamespace()),
		client.MatchingLabels{toolchainv1alpha1.TierLabelKey: nsTemplateTier.GetName()}); err != nil {
		c.t.Logf("problem with getting the ttrs for tier %s: %s", nsTemplateTier.GetName(), err)
		return false, err
	}
	if len(ttrs.Items) == 0 {
		c.t.Logf("the NSTemplateTier %s doesn't have TTRs", nsTemplateTier.GetName())
		return true, nil
	}
	if delete {
		c.t.Log("deleting also the related TTR CRs", "tierName", nsTemplateTier.GetName())
		return false, c.client.DeleteAllOf(context.TODO(), &toolchainv1alpha1.TierTemplateRevision{},
			client.InNamespace(nsTemplateTier.GetNamespace()),
			client.MatchingLabels{toolchainv1alpha1.TierLabelKey: nsTemplateTier.GetName()})
	}
	c.t.Log("waiting until all TTR CRs are completely deleted", "tierName", nsTemplateTier.GetName(), "number of present TTR CRs", len(ttrs.Items))
	// ttrs are still there
	return false, nil
}

// prodTierDeletion updates the obj (assumed freshly loaded from the cluster) with a new "random" annotation value to
// force its reconciliation if it is an NSTemplateTier. It does nothing if the object is not an NSTemplateTier.
func (c *cleanTask) prodTierDeletion(obj client.Object) {
	if _, ok := obj.(*toolchainv1alpha1.NSTemplateTier); !ok {
		return
	}

	if obj.GetAnnotations() == nil {
		obj.SetAnnotations(map[string]string{})
	}

	obj.GetAnnotations()["cleanTaskReconciliationEnforcement"] = uuid.NewString()

	if err := c.client.Update(context.TODO(), obj); err != nil {
		// let's not propagate this kind of error - this is just trying to speed up
		// the finalization of the NSTemplateTiers. A failure to update the object
		// will just cause a longer pause before the next finalization attempt.
		c.t.Logf("failed to force reconciliation of the NSTemplateTier %s: %s", obj.GetName(), err.Error())
	}
}
