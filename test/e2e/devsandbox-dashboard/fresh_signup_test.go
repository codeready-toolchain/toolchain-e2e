package sandboxui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	sandboxui "github.com/codeready-toolchain/toolchain-e2e/testsupport/devsandbox-dashboard"
	"github.com/playwright-community/playwright-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreshSignup tests the complete fresh signup flow:
// 1. Homepage layout when first accessing the dashboard
// 2. Clicking "Try it" to signup
// 3. Accessing OpenShift after signup
func TestFreshSignup(t *testing.T) {
	// Step 1: Setup the browser and login (LoadConfig called inside Setup)
	testName := "test-fresh-signup"
	page := sandboxui.Setup(t, testName)

	// Ensure the user signup is not present in the system
	env := viper.GetString("ENVIRONMENT")
	username := viper.GetString("SSO_USERNAME")
	ensureNoUserSignup(t, page, env, username)

	// Step 2: Verify homepage layout on first access
	verifyHomepage(t, page)

	// Step 3: Perform signup by clicking "Try it"
	performSignup(t, page, env, username)

	// Step 4: Verify OpenShift access
	verifyDevSandboxAccess(t, page, env, testName)
}

func ensureNoUserSignup(t *testing.T, page playwright.Page, env, username string) {
	switch env {
	case sandboxui.TestEnv:
		awaitilities := testsupport.WaitForDeployments(t)
		hostAwait := awaitilities.Host()
		userSignup, err := sandboxui.WaitForUserSignup(t, hostAwait, username)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			require.NoError(t, err) // fail on unexpected errors
		}
		if userSignup != nil {
			// delete user signup
			err := sandboxui.DeleteUserSignup(t, hostAwait, userSignup)
			require.NoError(t, err)
			_, err = page.Reload()
			require.NoError(t, err)
		}
	case sandboxui.ProdEnv:
		userSignup := sandboxui.GetUserSignupThroughKsctl(t, username)
		if userSignup != nil {
			// delete user signup
			sandboxui.DeleteUserSignupThroughKsctl(t, username)
			_, err := page.Reload()
			require.NoError(t, err)
		}
	}
}

// verifyHomepage checks the homepage layout and welcome text
// when the user accesses the Developer Sandbox Dashboard for the first time
func verifyHomepage(t *testing.T, page playwright.Page) {
	homeLink := page.Locator("a").Filter(playwright.LocatorFilterOptions{
		HasText: "Home",
	})
	sandboxui.IsVisible(t, homeLink)

	article := page.GetByRole("article")

	// check heading
	heading := article.GetByRole("heading", playwright.LocatorGetByRoleOptions{
		Level: playwright.Int(1),
	})
	headingText, err := heading.TextContent()
	require.NoError(t, err)
	assert.Equal(t, "Try Red Hat products", headingText)

	// check paragraphs
	require.NotNil(t, article.GetByText("Explore, experiment, and see what's possible"))
	require.NotNil(t, article.GetByText(regexp.MustCompile(`Click on "Try it" to initiate your free, no commitment \d+-day trial\.`)))

	// check image
	img := page.GetByRole("img", playwright.PageGetByRoleOptions{
		Name: "Red Hat Trial",
	})
	err = img.WaitFor()
	require.NoError(t, err)
}

// performSignup executes the signup flow by clicking "Try it"
// and waits for the signup to be approved
func performSignup(t *testing.T, page playwright.Page, env, username string) {
	article := page.GetByRole("article")
	loadingIcon := page.Locator("svg.v5-MuiCircularProgress-svg").First()

	card := page.GetByText("OpenShift Comprehensive cloud")
	sandboxui.IsVisible(t, card)

	// check cards
	cardsToCheck := []string{
		// SANDBOX-1755: OpenShift AI temporarily disabled
		// "OpenShift AI Scalable AI and",
		"Dev Spaces Cloud Development",
		"Ansible Automation Platform Scalable, centralized automation solution Available",
		"OpenShift Virtualization Migrate traditional VM workloads to OpenShift Unified",
	}
	for _, text := range cardsToCheck {
		elem := page.GetByText(text)
		sandboxui.IsVisible(t, elem)
	}

	// check heading
	heading := article.GetByRole("heading", playwright.LocatorGetByRoleOptions{
		Level: playwright.Int(1),
	})
	headingText, err := heading.TextContent()
	require.NoError(t, err)
	assert.Equal(t, "Try Red Hat products", headingText)

	// check paragraph
	require.NotNil(t, article.GetByText("Click on \"Try it\" to initiate your free, no commitment 30-day trial."))

	// click the "Try it" button in the card
	tryItButton := card.Locator("button", playwright.LocatorLocatorOptions{
		HasText: "Try it",
	})
	sandboxui.IsVisible(t, tryItButton)
	err = tryItButton.Click()
	require.NoError(t, err)

	switch env {
	case sandboxui.TestEnv:
		// add signup to cleanup
		awaitilities := testsupport.WaitForDeployments(t)
		hostAwait := awaitilities.Host()
		userSignup, err := sandboxui.WaitForUserSignup(t, hostAwait, username)
		require.NoError(t, err)
		cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	case sandboxui.ProdEnv:
		// delete user signup through ksctl
		t.Cleanup(func() {
			sandboxui.DeleteUserSignupThroughKsctl(t, username)
		})
	}

	// wait for loading icon to disappear
	err = loadingIcon.WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateHidden,
		Timeout: playwright.Float(60000), // 1 minute timeout
	})
	require.NoError(t, err)

	// wait for network to be idle (ensures all updates are complete)
	err = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateNetworkidle,
	})
	require.NoError(t, err)

	welcomeText := article.GetByText("Welcome")
	err = welcomeText.WaitFor()
	require.NoError(t, err)

	trialText := article.GetByText("Your free trial expires in 30 days")
	err = trialText.WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(30000),
	})
	require.NoError(t, err)
}

// verifyDevSandboxAccess tests access to OpenShift after the user is signed up
func verifyDevSandboxAccess(t *testing.T, page playwright.Page, env, testName string) {
	imgName := "Red Hat OpenShift Service on"
	logMessage := "Log in with…"
	if env == sandboxui.TestEnv {
		imgName = "Red Hat OpenShift"
		logMessage = "Log in with"
	}

	err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	})
	require.NoError(t, err)

	card := page.GetByText("OpenShift Comprehensive cloud")
	sandboxui.IsVisible(t, card)

	tryItBtn := card.Locator("a", playwright.LocatorLocatorOptions{
		HasText: "Try it",
	})

	err = tryItBtn.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	sandboxui.IsVisible(t, tryItBtn)

	// open the "Try it" button in a new popup and wait for it to fully load
	devSandboxPage, err := sandboxui.ClickAndWaitForPopup(t, page, tryItBtn, testName)
	require.NoError(t, err)

	if env == sandboxui.ProdEnv {
		// Wait for auth redirect to complete
		if strings.Contains(devSandboxPage.URL(), "/oauth/authorize") {
			err := devSandboxPage.WaitForURL("**/k8s/cluster/projects/**", playwright.PageWaitForURLOptions{
				Timeout: playwright.Float(30000),
			})
			require.NoError(t, err)
		}

		// Find welcome text and wait for it to be visible
		welcomeText := devSandboxPage.GetByText("Welcome to the new OpenShift experience!")
		err = welcomeText.WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(180000), // 3 minutes
		})
		require.NoError(t, err)
	} else {
		img := devSandboxPage.GetByRole("img", playwright.PageGetByRoleOptions{
			Name: imgName,
		})
		err = img.WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(30000),
		})
		require.NoError(t, err)

		h := devSandboxPage.GetByRole("heading", playwright.PageGetByRoleOptions{})
		hText, err := h.TextContent()
		require.NoError(t, err)
		require.Contains(t, hText, logMessage)

		list := devSandboxPage.GetByRole("list", playwright.PageGetByRoleOptions{})
		listText, err := list.TextContent()
		require.NoError(t, err)
		require.Contains(t, listText, "DevSandbox")
	}

	require.NoError(t, devSandboxPage.Close())
}
