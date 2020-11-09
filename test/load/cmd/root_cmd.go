package cmd

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"

	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
)

var kubeconfig string
var hostOperatorNS string
var signupUserAfter time.Duration
var signupUserInterval time.Duration
var deactivateUserAfter time.Duration
var deactivateUserInterval time.Duration
var banUserAfter time.Duration
var banUserInterval time.Duration
var startAt = 0

func init() {
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", defaultKubeConfig(), "location for the Kube config file")
	rootCmd.Flags().StringVar(&hostOperatorNS, "host-ns", "", "namespace of the host operator")
	rootCmd.Flags().DurationVar(&signupUserAfter, "user-signup-after", 0*time.Second, "delay before the first user signup")
	rootCmd.Flags().DurationVar(&signupUserInterval, "user-signup-every", 5*time.Second, "interval between each user signup")
	rootCmd.Flags().DurationVar(&deactivateUserAfter, "user-deactivate-after", 30*time.Second, "delay before the first user deactivation")
	rootCmd.Flags().DurationVar(&deactivateUserInterval, "user-deactivate-every", 10*time.Second, "interval between each user deactivation")
	rootCmd.Flags().DurationVar(&banUserAfter, "user-ban-after", 60*time.Second, "delay before the first user ban")
	rootCmd.Flags().DurationVar(&banUserInterval, "user-ban-every", 60*time.Second, "interval between each user ban")
	rootCmd.Flags().IntVar(&startAt, "start-at", 1, "first value of the sequence uses to generate the usernames")
	rootCmd.MarkFlagRequired("host-ns")
	rootCmd.MarkFlagRequired("kubeconfig")
}

// Execute runs the root cmd
func Execute() error {
	return rootCmd.Execute()
}

var rootCmd = &cobra.Command{
	Use:   "toolchain-loader",
	Short: "Generates load on the cluster",
	RunE:  run,
}

const (
	userSignupResourcePlural = "usersignups"
	bannedUserResourcePlural = "bannedusers"
)

var (
	lock      = sync.RWMutex{}
	usernames = []string{}
)

// run creates independent go routines which take care of
func run(cmd *cobra.Command, args []string) error {
	client, config, err := newClient()
	if err != nil {
		return errors.Wrap(err, "unable to initialize the client")
	}
	// confirm connection to cluster defined in config
	if confirm, err := confirmCluster(config); err != nil {
		return err
	} else if !confirm {
		return nil
	}

	counter = startAt
	// user signup routine
	go createUsers(cmd, client)
	// user deactivation routine
	go deactivateUsers(cmd, client)
	// user banning routine
	go banUsers(cmd, client)

	// wait until user kills with `<Ctrl+C>` this program
	// See https://gobyexample.com/signals
	fmt.Fprintf(cmd.OutOrStdout(), "🚛 load started. Press <ctrl>+C to stop...\n")
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		done <- true
	}()
	<-done
	return nil
}

func confirmCluster(config *restclient.Config) (bool, error) {
	prompt := promptui.Select{
		Label: fmt.Sprintf("☁️ connecting to %s ?", config.Host),
		Items: []string{
			"No, not that one!",
			"Yes, go ahead",
		},
	}
	i, _, err := prompt.Run()
	if err != nil {
		return false, err
	}
	return !(i == 0), nil
}

var counter = 0

func createUsers(cmd *cobra.Command, client *rest.RESTClient) {
	time.Sleep(signupUserAfter)
	for {
		if username, err := createUser(client); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "failed to create user '%s': %v\n", username, err.Error())
		}
		counter++
		time.Sleep(signupUserInterval)
	}
}

func createUser(client *restclient.RESTClient) (string, error) {
	username := fmt.Sprintf("test-user-%06d", counter)
	userEmail := username + "@example.com"
	md5hash := md5.New()
	// Ignore the error, as this implementation cannot return one
	_, _ = md5hash.Write([]byte(userEmail))
	emailHash := hex.EncodeToString(md5hash.Sum(nil))
	usersignup := toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hostOperatorNS,
			Name:      username,
			Annotations: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailAnnotationKey:           userEmail,
				toolchainv1alpha1.UserSignupVerificationCounterAnnotationKey: "0",
			},
			Labels: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailHashLabelKey: emailHash,
			},
		},
		Spec: toolchainv1alpha1.UserSignupSpec{
			Username:             username,
			Approved:             true,
			VerificationRequired: false,
		},
	}
	if err := client.Post().
		Namespace(hostOperatorNS).
		Resource(userSignupResourcePlural).
		Name(username).
		Body(&usersignup).
		Do(context.TODO()).Error(); err != nil {
		return username, err
	}
	// use a lock to avoid concurrent access to the `usernames`
	lock.Lock()
	defer lock.Unlock()
	usernames = append(usernames, username)
	return username, nil
}

func deactivateUsers(cmd *cobra.Command, client *rest.RESTClient) {
	time.Sleep(deactivateUserAfter)
	for {
		if username, err := deactivateUser(client); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "failed to deactivate user '%s': %v\n", username, err.Error())
		} else if username == "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "no user to deactivate? 😳\n")
		}
		time.Sleep(deactivateUserInterval)
	}
}

func deactivateUser(client *restclient.RESTClient) (string, error) {
	// use a lock to avoid concurrent access to the `usernames`
	lock.Lock()
	defer lock.Unlock()
	if len(usernames) == 0 {
		return "", nil
	}
	username := usernames[0]
	usersignup := &toolchainv1alpha1.UserSignup{}
	if err := client.Get().
		Namespace(hostOperatorNS).
		Resource(userSignupResourcePlural).
		Name(username).
		Do(context.TODO()).
		Into(usersignup); err != nil {
		return username, err
	}
	usersignup.Spec.Deactivated = true
	if err := client.Put().
		Namespace(hostOperatorNS).
		Resource(userSignupResourcePlural).
		Name(username).
		Body(usersignup).
		Do(context.TODO()).
		Error(); err != nil {
		return username, err
	}
	usernames = usernames[1:]
	return username, nil
}

func banUsers(cmd *cobra.Command, client *rest.RESTClient) {
	time.Sleep(banUserAfter)
	for {
		if username, err := banUser(client); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "failed to ban user '%s': %v\n", username, err.Error())
		} else if username == "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "no user to ban? 😳\n")
		}
		time.Sleep(banUserInterval)
	}
}

func banUser(client *restclient.RESTClient) (string, error) {
	// use a lock to avoid concurrent access to the `usernames`
	lock.Lock()
	defer lock.Unlock()
	if len(usernames) == 0 {
		return "", nil
	}
	username := usernames[0]
	usersignup := &toolchainv1alpha1.UserSignup{}
	if err := client.Get().
		Namespace(hostOperatorNS).
		Resource(userSignupResourcePlural).
		Name(username).
		Do(context.TODO()).
		Into(usersignup); err != nil {
		return username, err
	}
	banUser := &toolchainv1alpha1.BannedUser{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hostOperatorNS,
			Name:      username,
			Labels: map[string]string{
				toolchainv1alpha1.BannedUserEmailHashLabelKey: usersignup.ObjectMeta.Labels[toolchainv1alpha1.UserSignupUserEmailHashLabelKey],
			},
		},
		Spec: toolchainv1alpha1.BannedUserSpec{
			Email: username + "@example.com",
		},
	}
	if err := client.Post().
		Namespace(hostOperatorNS).
		Resource(bannedUserResourcePlural).
		Name(username).
		Body(banUser).
		Do(context.TODO()).
		Error(); err != nil {
		return username, err
	}
	// use a lock to avoid concurrent access to the `usernames`
	usernames = usernames[1:]
	return username, nil
}
