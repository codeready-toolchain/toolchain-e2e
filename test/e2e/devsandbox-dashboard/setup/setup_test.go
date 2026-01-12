package setup

import (
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
)

func TestSetup(t *testing.T) {
	// check Developer Sandbox Dashboard is up and running
	testsupport.WaitForDevSandboxDashboard(t)
}
