package drive

import (
	"testing"

	"google.golang.org/api/drive/v3"
)

func TestDirLeftPath(t *testing.T) {
	parents := map[string]string{"root": "", "a": "A", "b": "B"}
	parentPath := func(id string) (string, bool) {
		p, ok := parents[id]
		return p, ok
	}
	same := func(s string) string { return s }
	folder := func(name string, parent string) *drive.Change {
		return &drive.Change{File: &drive.File{Name: name, Parents: []string{parent}, MimeType: driveFolderType}}
	}
	trashed := folder("x", "a")
	trashed.File.Trashed = true
	for _, c := range []struct {
		name   string
		change *drive.Change
		cached string
		left   bool
	}{
		{"unchanged", folder("x", "a"), "A/x", false},
		{"unchanged at the root", folder("A", "root"), "A", false},
		{"renamed", folder("y", "a"), "A/x", true},
		{"moved", folder("x", "b"), "A/x", true},
		{"moved out of the known folders", folder("x", "elsewhere"), "A/x", true},
		{"trashed", trashed, "A/x", true},
		{"deleted", &drive.Change{Removed: true}, "A/x", true},
	} {
		if got := dirLeftPath(c.change, c.cached, parentPath, same); got != c.left {
			t.Errorf("%s: got %v, want %v", c.name, got, c.left)
		}
	}
}
