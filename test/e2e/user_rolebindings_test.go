package e2e

import (
	"context"
	"testing"

	"k8s.io/client-go/tools/clientcmd"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"

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
	config, err := clientcmd.BuildConfigFromFlags("", "/Users/kanikarana/openshift-install-mac/my_cluster/auth/kubeconfig")
	require.NoError(t, err)
	s := runtime.NewScheme()
	err = userv1.Install(s)
	require.NoError(t, err)

	config.Impersonate = rest.ImpersonationConfig{
		UserName: "system:serviceaccount:default:test",
	}
	// Create and approve user signup
	usersignup, _ := testsupport.NewSignupRequest(t, awaitilities).
		Username("harleyquinn").
		ManuallyApprove().
		TargetCluster(awaitilities.Member1()).
		EnsureMUR().
		RequireConditions(testsupport.ConditionSet(testsupport.Default(), testsupport.ApprovedByAdmin())...).
		Execute().
		Resources()

	clientset, err := kubernetes.NewForConfig(config)

	role, err := clientset.RbacV1().Roles("harleyquinn-dev").Get(context.TODO(), "rbac-edit", metav1.GetOptions{})
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
	_, err = clientset.RbacV1().RoleBindings(usersignup.Namespace).Create(context.TODO(), &rb, metav1.CreateOptions{})
	require.Errorf(t, err, "asdfasfasfafa")

	//t.Run("try to create rolebindings giving access to all users", func(t *testing.T) {
	//	hostAwait.Client.Create()
	//})
	//
	//t.Run("try to create rolebindings giving access to all service accounts", func(t *testing.T) {
	//
	//})
}
