package sandboxui

import (
	"regexp"
	"testing"

	sandboxui "github.com/codeready-toolchain/toolchain-e2e/testsupport/sandbox-ui"
	"github.com/playwright-community/playwright-go"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHomepage tests the homepage layout and welcome text
// when the user accesses the Developer Sandbox Dashboard for the first time
func TestHomepage(t *testing.T) {
	page := sandboxui.Setup(t, "test-homepage")

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

// TestSignup tests the signup flow (automatically approved)
// when the user clicks on "Try it" for the first time
func TestSignup(t *testing.T) {
	page := sandboxui.Setup(t, "test-signup")

	article := page.GetByRole("article")
	loadingIcon := page.Locator("svg.v5-MuiCircularProgress-svg").First()

	card := page.GetByText("OpenShift Comprehensive cloud")
	sandboxui.IsVisible(t, card)

	// check cards
	cardsToCheck := []string{
		"OpenShift AI Scalable AI and",
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

	// wait for loading icon to disappear
	err = loadingIcon.WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateHidden})
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
	err = trialText.WaitFor()
	require.NoError(t, err)
}

// TestDevSandbox tests the access to Openshift after the user is signed up
func TestDevSandbox(t *testing.T) {
	page := sandboxui.Setup(t, "test-devsandbox")
	env := viper.GetString("ENVIRONMENT")

	imgName := "Red Hat OpenShift Service on"
	logMessage := "Log in with…"
	if env == sandboxui.UIE2ETestsEnv {
		imgName = "Red Hat OpenShift"
		logMessage = "Log in with"
	}

	err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	})
	require.NoError(t, err)

	card := page.GetByText("OpenShift Comprehensive cloud")
	sandboxui.IsVisible(t, card)

	tryItBtn := card.Locator("button", playwright.LocatorLocatorOptions{
		HasText: "Try it",
	})

	err = tryItBtn.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	sandboxui.IsVisible(t, tryItBtn)

	// open the article in a new popup and wait for it to fully load
	devSandboxPage, err := sandboxui.ClickAndWaitForPopup(page, tryItBtn)
	require.NoError(t, err)

	img := devSandboxPage.GetByRole("img", playwright.PageGetByRoleOptions{
		Name: imgName,
	})
	err = img.WaitFor()
	require.NoError(t, err)

	h := devSandboxPage.GetByRole("heading", playwright.PageGetByRoleOptions{})
	hText, err := h.TextContent()
	require.NoError(t, err)
	require.Contains(t, hText, logMessage)

	list := devSandboxPage.GetByRole("list", playwright.PageGetByRoleOptions{})
	listText, err := list.TextContent()
	require.NoError(t, err)
	require.Contains(t, listText, "DevSandbox")

	require.NoError(t, devSandboxPage.Close())
}
