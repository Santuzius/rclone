package onedrive

import (
	"testing"
	"time"

	"github.com/rclone/rclone/backend/onedrive/api"
	"github.com/stretchr/testify/assert"
)

func TestItemChangeNamesItemsAsListingsDo(t *testing.T) {
	f := &Fs{driveID: "d1"}
	modified := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

	file := &api.Item{
		ID:              "f1",
		Name:            "a.txt",
		ParentReference: &api.ItemReference{DriveID: "d1", ID: "p1"},
		FileSystemInfo:  &api.FileSystemInfoFacet{LastModifiedDateTime: api.Timestamp(modified)},
	}
	change, ok := f.itemChange(file)
	assert.True(t, ok)
	assert.Equal(t, ItemChange{ID: "d1#f1", ParentID: "d1#p1", Name: "a.txt", ModTime: modified}, change)

	deleted := &api.Item{ID: "f2", Deleted: &api.DeletedFacet{}}
	change, ok = f.itemChange(deleted)
	assert.True(t, ok)
	assert.Equal(t, ItemChange{ID: "d1#f2", Gone: true}, change)

	notebook := &api.Item{ID: "n1", Package: &api.PackageFacet{Type: api.PackageTypeOneNote}, ParentReference: &api.ItemReference{DriveID: "d1", ID: "p1"}}
	change, _ = f.itemChange(notebook)
	assert.True(t, change.Gone)

	root := &api.Item{ID: "r", Folder: &api.FolderFacet{}, ParentReference: &api.ItemReference{DriveID: "d1"}}
	_, ok = f.itemChange(root)
	assert.False(t, ok)
}
