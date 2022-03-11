package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
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
	//os.Setenv("MEMBER_NS", "toolchain-member-24093142")
	//os.Setenv("MEMBER_NS_2", "toolchain-member2-24093142")
	//os.Setenv("HOST_NS", "toolchain-host-24093142")
	//os.Setenv("REGISTRATION_SERVICE_NS", "toolchain-host-24093142")
	// given
	awaitilities := testsupport.WaitForDeployments(t)

	//config, err := clientcmd.BuildConfigFromFlags("", "/Users/kanikarana/openshift-install-mac/my_cluster/auth/kubeconfig")
	//config, err := rest.InClusterConfig()
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
	testsupport.NewSignupRequest(t, awaitilities).
		Username("joker").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(testsupport.ConditionSet(testsupport.Default(), testsupport.ApprovedByAdmin())...).
		Execute()

	nonsandboxUser := &userv1.User{}
	err = awaitilities.Member1().Client.Get(context.TODO(), types.NamespacedName{Name: "joker"}, nonsandboxUser)
	require.NoError(t, err)

	nonsandboxUser.Labels = map[string]string{
		toolchainv1alpha1.OwnerLabelKey: "joker",
	}
	err = awaitilities.Member1().Client.Update(context.TODO(), nonsandboxUser)
	require.NoError(t, err)
	err = awaitilities.Member1().Client.Get(context.TODO(), types.NamespacedName{Name: "joker"}, nonsandboxUser)
	require.NoError(t, err)
	fmt.Printf("The labels of joker: %+v \n", nonsandboxUser.Labels)

	//role, err := clientset.RbacV1().Roles("harleyquinn-dev").Get(context.TODO(), "rbac-edit", metav1.GetOptions{})
	role := rbacv1.Role{}
	err = awaitilities.Member1().Client.Get(context.TODO(), types.NamespacedName{
		Namespace: "harleyquinn-dev",
		Name:      "rbac-edit",
	}, &role)
	require.NoError(t, err)
	require.NotEmpty(t, role)
	require.Equal(t, role.Name, "rbac-edit")

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
			Name:     role.Name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:authenticated",
			},
		},
	}

	t.Run("sandbox user trying to create a rolebinding giving access to all users should be denied", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "harleyquinn",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		err = clientset.Create(context.TODO(), &rb)
		require.Errorf(t, err, "asdfasfasfafa")
		require.Contains(t, err.Error(), "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:authenticated")
		fmt.Printf(">>>>> err is : %+v \n ", err)
	})

	t.Run("sandbox user trying to create a rolebinding giving access to all service accounts should be denied", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "harleyquinn",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:serviceaccounts",
			},
		}
		err = clientset.Create(context.TODO(), &rb)
		require.Errorf(t, err, "asdfasfasfafa")
		require.Contains(t, err.Error(), "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:serviceaccounts")
		fmt.Printf(">>>>> err is : %+v \n ", err)
	})

	t.Run("sandbox user trying to create a rolebinding giving access to a particular user should be allowed", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "harleyquinn",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "johnsmith",
			},
		}
		err = clientset.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

	t.Run("service account trying to create a rolebinding giving access to all users should be allowed", func(t *testing.T) {
		//using hostawailities which using e2e-service-account
		//config.Impersonate = rest.ImpersonationConfig{}
		//clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Name = "wide-access-sa"
		rb.ResourceVersion = ""
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:authenticated",
			},
		}
		err = awaitilities.Host().Client.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

	t.Run("service account trying to create a rolebinding giving access to all service accounts should be allowed", func(t *testing.T) {
		//config.Impersonate = rest.ImpersonationConfig{
		//	UserName: "system:serviceaccount:e2e-service-account",
		//}
		//clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Name = "wide-access-sa2"
		rb.ResourceVersion = ""
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:serviceaccounts",
			},
		}
		err = awaitilities.Host().Client.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

	t.Run("service account trying to create a rolebinding giving access to a particular user should be allowed", func(t *testing.T) {
		//config.Impersonate = rest.ImpersonationConfig{
		//	UserName: "system:serviceaccount:e2e-service-account",
		//}
		//clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Name = "user-access-sa"
		rb.ResourceVersion = ""
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "johnsmith",
			},
		}
		err = awaitilities.Host().Client.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

	t.Run("non-sandbox user trying to create a rolebinding giving access to all users should be allowed", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "joker",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Name = "wide-access-non-sandbox"
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:authenticated",
			},
		}
		err = clientset.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

	t.Run("non-sandbox user trying to create a rolebinding giving access to all service accounts should be allowed", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "joker",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Name = "wide-access-non-sandbox2"
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "Group",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "system:serviceaccounts",
			},
		}
		err = clientset.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

	t.Run("non-sandbox user trying to create a rolebinding giving access to a particular should be allowed", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "joker",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		rb.Name = "user-access-non-sandbox"
		rb.Subjects = []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "harleyquinn",
			},
		}
		err = clientset.Create(context.TODO(), &rb)
		require.NoError(t, err)
	})

}
