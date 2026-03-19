package canonical

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return body
}
