package sandboxui

import (
	"testing"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func IsVisible(t *testing.T, elem playwright.Locator) {
	err := elem.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
}
