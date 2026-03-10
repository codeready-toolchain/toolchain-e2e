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

	tracePath := filepath.Join(getTraceDirectory(t), fmt.Sprintf("trace-%s.zip", testName))

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

func getTraceDirectory(t *testing.T) string {
	dir, err := os.Getwd()
	require.NoError(t, err)

	dirName := "trace"
	getTraceDirectoryPath := filepath.Join(dir, "..", "..", dirName)

	if os.Getenv("RUNNING_IN_CONTAINER") == "true" {
		getTraceDirectoryPath = filepath.Join(os.Getenv("E2E_REPO_PATH"), dirName)
	}

	if os.Getenv("CI") == "true" {
		// save trace in the job CI artifact directory
		// artifacts/e2e/test/artifacts/devsandbox-dashboard/trace
		getTraceDirectoryPath = filepath.Join(os.Getenv("ARTIFACT_DIR"), "devsandbox-dashboard", dirName)
	}

	return getTraceDirectoryPath
}

// handleRecordedVideo handles the main recorded video by renaming it to the test name instead of auto-generated IDs, if test fails
// and removing it if the test passes
func handleRecordedVideo(t *testing.T, page playwright.Page, targetVideoPath string) {
	t.Cleanup(func() {
		videoPath, err := page.Video().Path()
		if err == nil && videoPath != "" {
			if t.Failed() {
				if err := os.Rename(videoPath, targetVideoPath); err != nil {
					t.Logf("failed to rename video %s: %v", videoPath, err)
				}
			} else {
				// Test passed, remove the video
				if err := os.Remove(videoPath); err != nil {
					t.Logf("failed to remove video %s: %v", videoPath, err)
				}
			}
		} else {
			t.Logf("skipped video removal - path empty or error: %s, %v", videoPath, err)
		}
	})
}
