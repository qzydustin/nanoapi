package codec

import (
	"os"
	"path/filepath"
	"strings"
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

func fixtureString(t *testing.T, name string) string {
	t.Helper()
	return strings.TrimSpace(string(fixtureBytes(t, name)))
}
