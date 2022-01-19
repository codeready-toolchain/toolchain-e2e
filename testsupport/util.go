package testsupport

import (
	"fmt"

	"github.com/gofrs/uuid"
)

// GenerateName appends generated UUID to the given string
func GenerateName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.Must(uuid.NewV4()).String())
}
