package server

import (
	"os"
	"path/filepath"
	"testing"
)

func fixtureString(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return string(body)
}
