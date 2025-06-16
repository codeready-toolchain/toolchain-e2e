package sandboxui

import (
	"context"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ClickAndWaitForPopup(currentPage playwright.Page, locator playwright.Locator) (playwright.Page, error) {
	var popup playwright.Page
	var err error

	pollErr := wait.PollUntilContextTimeout(context.TODO(), time.Millisecond, 1*time.Minute, true, func(ctx context.Context) (bool, error) {
		popup, err = currentPage.ExpectPopup(func() error {
			return locator.Click()
		})
		if err != nil || popup == nil {
			return false, nil
		}
		return true, nil
	})

	if pollErr != nil || popup == nil {
		return nil, fmt.Errorf("popup did not appear in time: %w", pollErr)
	}

	waitErr := popup.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	})
	if waitErr != nil {
		return nil, fmt.Errorf("popup did not finish loading: %w", waitErr)
	}
	return popup, nil
}
