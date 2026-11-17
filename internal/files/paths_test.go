package files_test

import (
	"path/filepath"
	"testing"

	"github.com/resoltico/pdfconcat/internal/files"
)

func TestNativePaths(t *testing.T) {
	base := t.TempDir()
	for _, s := range []string{"", "a\x00b"} {
		if _, err := files.Path(base, s); err == nil {
			t.Errorf("accepted path %q", s)
		}
	}
	path, err := files.Path(base, "a/link/../b.pdf")
	want := filepath.Join(base, "a/b.pdf")
	if err != nil || path != want {
		t.Fatalf("lexical path %q %v", path, err)
	}
}
