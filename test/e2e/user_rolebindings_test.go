package e2e

import (
	"context"
	"testing"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	config "sigs.k8s.io/controller-runtime/pkg/client/config"

	"k8s.io/apimachinery/pkg/types"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/client-go/rest"

	testsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport"
)

func TestUserCreatingRoleBindings(t *testing.T) {

	awaitilities := testsupport.WaitForDeployments(t)

	config, err := config.GetConfig()
	require.NoError(t, err)

	s := runtime.NewScheme()
	err = userv1.Install(s)
	require.NoError(t, err)

	// Create and approve user signup
	testsupport.NewSignupRequest(t, awaitilities).
		Username("harleyquinn").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(testsupport.ConditionSet(testsupport.Default(), testsupport.ApprovedByAdmin())...).
		Execute()

	//create a non-sandbox user
	nonsandboxUser := &userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: "joker",
		},
	}
	err = awaitilities.Member1().Client.Create(context.TODO(), nonsandboxUser)
	require.NoError(t, err)

	rb := rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind: "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wide-access",
			Namespace: "harleyquinn-dev",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "rbac-edit",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:authenticated",
			},
		},
	}
	t.Run("sandbox user creating different rolebindings", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "harleyquinn",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		require.NoError(t, err)
		t.Run("sandbox user trying to create a rolebinding giving access to all users should be denied", func(t *testing.T) {
			err = clientset.Create(context.TODO(), &rb)
			require.Contains(t, err.Error(), "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:authenticated")
		})

		t.Run("sandbox user trying to create a rolebinding giving access to all service accounts should be denied", func(t *testing.T) {
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:serviceaccounts",
				},
			}
			err = clientset.Create(context.TODO(), &rb)
			require.Contains(t, err.Error(), "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:serviceaccounts")
		})

		t.Run("sandbox user trying to create a rolebinding giving access to a particular user should be allowed", func(t *testing.T) {
			//given
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "johnsmith",
				},
			}

			//when
			err = clientset.Create(context.TODO(), &rb)
			require.NoError(t, err)

			//then
			createdRb := rbacv1.RoleBinding{}
			err = clientset.Get(context.TODO(), types.NamespacedName{
				Name:      "wide-access",
				Namespace: "harleyquinn-dev"}, &createdRb)
			require.NoError(t, err)
			require.NotEmpty(t, createdRb)
			require.Equal(t, "johnsmith", createdRb.Subjects[0].Name)
		})
	})

	t.Run("service account trying to create a rolebinding giving access to all users should be allowed", func(t *testing.T) {
		//using hostawailities which using e2e-service-account
		rb.Name = "wide-access-sa"
		rb.ResourceVersion = ""
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:authenticated",
			},
		}
		//when
		err = awaitilities.Host().Client.Create(context.TODO(), &rb)
		//then
		require.NoError(t, err)
	})

	t.Run("service account trying to create a rolebinding giving access to all service accounts should be allowed", func(t *testing.T) {
		//using hostawailities which using e2e-service-account
		rb.Name = "wide-access-sa2"
		rb.ResourceVersion = ""
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:serviceaccounts",
			},
		}
		//when
		err = awaitilities.Host().Client.Create(context.TODO(), &rb)
		//then
		require.NoError(t, err)
	})

	t.Run("service account trying to create a rolebinding giving access to a particular user should be allowed", func(t *testing.T) {
		//given
		//using hostawailities which using e2e-service-account
		rb.Name = "user-access-sa"
		rb.ResourceVersion = ""
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "johnsmith",
			},
		}
		//when
		err = awaitilities.Host().Client.Create(context.TODO(), &rb)
		//then
		require.NoError(t, err)
	})

	t.Run("non-sandbox user creating various rolebindings", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "joker",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})

		t.Run("non-sandbox user trying to create a rolebinding giving access to all users should be allowed", func(t *testing.T) {
			//given
			rb.Name = "wide-access-non-sandbox"
			rb.ResourceVersion = ""
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:authenticated",
				},
			}
			//when
			err = clientset.Create(context.TODO(), &rb)
			//then
			require.NoError(t, err)
		})

		t.Run("non-sandbox user trying to create a rolebinding giving access to all service accounts should be allowed", func(t *testing.T) {
			//given
			rb.Name = "wide-access-non-sandbox2"
			rb.ResourceVersion = ""
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:serviceaccounts",
				},
			}
			//when
			err = clientset.Create(context.TODO(), &rb)
			//then
			require.NoError(t, err)
		})

		t.Run("non-sandbox user trying to create a rolebinding giving access to a particular should be allowed", func(t *testing.T) {
			//given
			rb.Name = "user-access-non-sandbox"
			rb.ResourceVersion = ""
			rb.Subjects = []rbacv1.Subject{
				{
					Kind:     "User",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "harleyquinn",
				},
			}
			//when
			err = clientset.Create(context.TODO(), &rb)
			//then
			require.NoError(t, err)
		})
	})

}
