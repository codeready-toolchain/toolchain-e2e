package sandboxui

import (
	"testing"

	sandboxui "github.com/codeready-toolchain/toolchain-e2e/testsupport/sandbox-ui"
	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// TestHeader opens and verifies that 'Red Hat Developer Hub'
// and 'Contact Red Hat Sales' links are not broken
func TestHeader(t *testing.T) {
	page := sandboxui.Setup(t, "test-header")

	// opens and verifies that 'Red Hat Developer Hub' popup is not broken
	rhdhLink := page.GetByRole("link", playwright.PageGetByRoleOptions{
		Name: "Red Hat Developer Hub , Opens",
	})
	sandboxui.IsVisible(t, rhdhLink)

	rhdhPage, err := sandboxui.ClickAndWaitForPopup(page, rhdhLink)
	require.NoError(t, err)

	h1Text, err := rhdhPage.Locator("h1").TextContent()
	require.NoError(t, err)
	require.Contains(t, h1Text, "Red Hat Developer Hub")

	require.NoError(t, rhdhPage.Close())

	// opens and verifies that 'Contact Red Hat Sales' popup is not broken
	salesBtn := page.GetByRole("button", playwright.PageGetByRoleOptions{
		Name: "Contact Red Hat Sales",
	})
	salesPage, err := sandboxui.ClickAndWaitForPopup(page, salesBtn)
	require.NoError(t, err)

	h1Text, err = salesPage.Locator("h1").TextContent()
	require.NoError(t, err)
	require.Contains(t, h1Text, "Contact Red Hat")

	require.NoError(t, salesPage.Close())
}
