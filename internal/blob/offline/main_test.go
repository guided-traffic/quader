package offline_test

import (
	"os"
	"testing"

	"github.com/guided-traffic/quader/internal/blob/blobtest"
)

// TestMain runs the package's tests with the AWS environment emptied: a connection element is read with the binary's rules, and
// a region the element does not name would otherwise come from the machine's ~/.aws/config (storage.md R-STOR-089).
func TestMain(m *testing.M) {
	blobtest.EmptyAWSEnvironment()
	os.Exit(m.Run())
}
