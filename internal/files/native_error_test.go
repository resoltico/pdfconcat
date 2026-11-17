package files_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/resoltico/pdfconcat/internal/files"
)

func TestSourceDisappearanceRetainsNativeEvidence(t *testing.T) {
	_, err := files.Read(t.Context(), t.TempDir()+"/removed.pdf", 0)
	native, cause := files.NativeCause(err)
	if !native || !errors.Is(cause, fs.ErrNotExist) {
		t.Fatalf("native read failure lost: %v", err)
	}
}
