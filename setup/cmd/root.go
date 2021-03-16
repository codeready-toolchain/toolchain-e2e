package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/idlers"
	"github.com/codeready-toolchain/toolchain-e2e/setup/resources"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/codeready-toolchain/toolchain-e2e/setup/users"

	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uitable/util/strutil"
	"github.com/spf13/cobra"
)

var usernamePrefix = "zippy"
var kubeconfig string
var verbose bool
var hostOperatorNamespace string
var memberOperatorNamespace string
var templatePath string
var numberOfUsers int
var userBatches int
var resourceRate int
var resourceProcessorsCount int

// Execute the setup command to fill a cluster with as many users as requested.
// The command uses the default `$KUBECONFIG` or `<home>/.kube/config` unless a path is specified.
func Execute() {
	cmd := &cobra.Command{
		Use:           "setup",
		Short:         "setup a cluster with the number of user accounts",
		SilenceErrors: true,
		SilenceUsage:  false,
		Args:          cobra.NoArgs,
		Run:           setup,
	}

	quayNS, found := os.LookupEnv("QUAY_NAMESPACE")
	if !found || len(quayNS) == 0 {
		fmt.Println("QUAY_NAMESPACE env var is not set, ensure the prerequisite setup steps are followed")
		os.Exit(1)
	}
	defaultHostNS := fmt.Sprintf("%s-host-operator", quayNS)
	defaultMemberNS := fmt.Sprintf("%s-member-operator", quayNS)

	cmd.Flags().StringVar(&usernamePrefix, "username", usernamePrefix, "the prefix used for usersignup names")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "if 'debug' traces should be displayed in the console")
	cmd.Flags().IntVarP(&numberOfUsers, "users", "u", 3000, "the number of user accounts to provision")
	cmd.Flags().IntVarP(&userBatches, "batch", "b", 25, "create user accounts in batches of N, increasing batch size may cause performance problems")
	cmd.Flags().IntVarP(&resourceRate, "resource-rate", "r", 5, "every N users will have resources created to drive load on the cluster")
	cmd.Flags().StringVar(&hostOperatorNamespace, "host-ns", defaultHostNS, "the namespace of Host operator")
	cmd.Flags().StringVar(&memberOperatorNamespace, "member-ns", defaultMemberNS, "the namespace of the Member operator")
	cmd.Flags().IntVar(&resourceProcessorsCount, "resource-processors", 20, "the number of resource processors used for creating user resources, increase value to process templates with more resources faster")
	cmd.Flags().StringVar(&templatePath, "template", "", "the path to the OpenShift template to apply")
	cmd.MarkFlagRequired("template")

	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setup(cmd *cobra.Command, args []string) {
	cmd.SilenceUsage = true
	term := terminal.New(cmd.InOrStdin, cmd.OutOrStdout, verbose)

	term.Debugf("Number of Users:           '%d'", numberOfUsers)
	term.Debugf("User Batch Size:           '%d'", userBatches)
	term.Debugf("Load Rate:                 '%d'", resourceRate)
	term.Debugf("Host Operator Namespace:   '%s'", hostOperatorNamespace)
	term.Debugf("Member Operator Namespace: '%s'\n", memberOperatorNamespace)

	// validate number of users
	if numberOfUsers < 1 {
		term.Fatalf(fmt.Errorf("value must be more than 0"), "invalid users value '%d'", numberOfUsers)
	}
	if numberOfUsers%userBatches != 0 {
		term.Fatalf(fmt.Errorf("users value must be a multiple of the batch size '%d'", userBatches), "invalid users value '%d'", numberOfUsers)
	}

	// validate concurrent creates
	if resourceProcessorsCount < 1 {
		term.Fatalf(fmt.Errorf("value must be more than 0"), "invalid resource processors value '%d'", resourceProcessorsCount)
	}

	term.Infof("🕖 initializing...")
	cl, config, scheme, err := cfg.NewClient(term, kubeconfig)
	if err != nil {
		term.Fatalf(err, "cannot create client")
	}

	if !term.PromptBoolf("👤 provision %d users in batches of %d on %s", numberOfUsers, userBatches, config.Host) {
		return
	}

	// provision the users
	term.Infof("🍿 provisioning users...")

	uip := uiprogress.New()
	uip.Start()

	// start the progress bars in go routines
	var wg sync.WaitGroup
	usersignupBar := uip.AddBar(numberOfUsers).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft(fmt.Sprintf("user signups (%d/%d)", b.Current(), numberOfUsers), 25, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for usersignupBar.Incr() {
			username := fmt.Sprintf("%s-%04d", usernamePrefix, usersignupBar.Current())
			if err := users.Create(cl, username, hostOperatorNamespace, memberOperatorNamespace); err != nil {
				term.Fatalf(err, "failed to provision user '%s'", username)
			}
			time.Sleep(time.Millisecond * 20)

			// when the batch is done, wait for the user's namespaces to exist before proceeding
			if usersignupBar.Current()%userBatches == 0 {
				for i := usersignupBar.Current() - userBatches + 1; i < usersignupBar.Current(); i++ {
					userToCheck := fmt.Sprintf("%s-%04d", usernamePrefix, i)
					userNS := fmt.Sprintf("%s-stage", userToCheck)
					if err := resources.WaitForNamespace(cl, userNS); err != nil {
						term.Fatalf(err, "failed to find namespace '%s'", userNS)
					}
				}
			}
		}
	}()

	setupBar := uip.AddBar(numberOfUsers).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft(fmt.Sprintf("user setup (%d/%d)", b.Current(), numberOfUsers), 25, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for setupBar.Incr() {
			username := fmt.Sprintf("%s-%04d", usernamePrefix, setupBar.Current())

			// update Idlers timeout to kill workloads faster to reduce impact of memory/cpu usage during testing
			if err := idlers.UpdateTimeout(cl, username, 15*time.Second); err != nil {
				term.Fatalf(err, "failed to update idlers for user '%s'", username)
			}

			// create resources for every nth user
			if setupBar.Current()%resourceRate == 0 {
				if err := resources.CreateFromTemplate(cl, config, scheme, templatePath, username); err != nil {
					term.Fatalf(err, "failed to create resources for user '%s'", username)
				}
			}
		}
	}()

	wg.Wait()
	uip.Stop()
	term.Infof("🏁 done provisioning users")
	term.Infof("👋 have fun!")
}
