package setup

import (
	"os"
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/test/migration"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
)

func TestSetupMigration(t *testing.T) {
	os.Setenv("HOST_NS", "toolchain-host")
	os.Setenv("MEMBER_NS", "toolchain-member")
	os.Setenv("MEMBER_NS_2", "toolchain-member2")
	os.Setenv("SECOND_MEMBER_MODE", "true")
	os.Setenv("REGISTRATION_SERVICE_NS", "toolchain-host")

	// given
	awaitilities := WaitForOperators(t)

	runner := migration.SetupMigrationRunner{
		Awaitilities: awaitilities,
		WithCleanup:  false,
	}

	runner.Run(t)
}
