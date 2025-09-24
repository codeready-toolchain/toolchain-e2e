package sandboxui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// trace starts Playwright tracing for the given browser context and
// saves the trace if the test fails under trace folder
func trace(t *testing.T, context playwright.BrowserContext, testName string) {
	err := context.Tracing().Start(playwright.TracingStartOptions{
		Screenshots: playwright.Bool(true),
		Snapshots:   playwright.Bool(true),
		Sources:     playwright.Bool(true),
	})
	require.NoError(t, err)

	dir, err := os.Getwd()
	require.NoError(t, err)
	tracePath := filepath.Join(dir, fmt.Sprintf("../../trace/trace-%s.zip", testName))

	if os.Getenv("RUNNING_IN_CONTAINER") == "true" {
		tracePath = filepath.Join(os.Getenv("E2E_REPO_PATH"), fmt.Sprintf("/trace/trace-%s.zip", testName))
	}

	if os.Getenv("CI") == "true" {
		// save trace in the job CI artifact directory
		// artifacts/e2e/test/artifacts/sandbox-ui/trace/trace-%s.zip
		tracePath = filepath.Join(os.Getenv("ARTIFACT_DIR"), fmt.Sprintf("/sandbox-ui/trace/trace-%s.zip", testName))
	}

	t.Cleanup(func() {
		if t.Failed() {
			if err := context.Tracing().Stop(tracePath); err != nil {
				t.Logf("failed to save trace: %v", err)
			} else {
				t.Logf("saved trace to %s", tracePath)
			}
		}
	})
}
