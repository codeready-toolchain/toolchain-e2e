package doubles

import (
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"time"
)

const (
	CleanupRetryInterval = time.Second * 1
	CleanupTimeout       = time.Second * 5
)

// CleanupOptions returns a CleanupOptions for the given test context and set with CleanupTimeout and CleanupRetryInterval
func CleanupOptions(ctx *framework.TestCtx) *framework.CleanupOptions {
	return &framework.CleanupOptions{TestContext: ctx, Timeout: CleanupTimeout, RetryInterval: CleanupRetryInterval}
}
