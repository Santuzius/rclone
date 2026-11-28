package onedrive

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rclone/rclone/backend/onedrive/api"
	"github.com/rclone/rclone/lib/dircache"
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

func TestMovedFoldersLeaveTheDirectoryCache(t *testing.T) {
	f := &Fs{driveID: "d1"}
	f.dirCache = dircache.New("", "d1#root", f)
	f.dirCache.Put("", "d1#root")
	f.dirCache.Put("Docs", "d1#docs")
	f.dirCache.Put("Docs/Sub", "d1#sub")
	folder := func(id, parent, name string) *api.Item {
		return &api.Item{ID: id, Name: name, Folder: &api.FolderFacet{}, ParentReference: &api.ItemReference{DriveID: "d1", ID: parent}}
	}

	// Listed because something below it changed: it stays, and so does the root.
	known, moved := f.forgetMovedFolder(folder("docs", "root", "Docs"))
	assert.Equal(t, []bool{true, false}, []bool{known, moved})
	known, moved = f.forgetMovedFolder(&api.Item{ID: "root", Folder: &api.FolderFacet{}})
	assert.Equal(t, []bool{true, false}, []bool{known, moved})
	_, ok := f.dirCache.Get("Docs/Sub")
	assert.True(t, ok)

	// A folder the cache doesn't know.
	known, _ = f.forgetMovedFolder(folder("new", "docs", "New"))
	assert.False(t, known)

	// Renamed: it leaves the cache with what is below it.
	known, moved = f.forgetMovedFolder(folder("docs", "root", "Papers"))
	assert.Equal(t, []bool{true, true}, []bool{known, moved})
	_, ok = f.dirCache.Get("Docs")
	assert.False(t, ok)
	_, ok = f.dirCache.Get("Docs/Sub")
	assert.False(t, ok)
	_, ok = f.dirCache.Get("")
	assert.True(t, ok)

	// Deleted.
	f.dirCache.Put("Old", "d1#old")
	_, moved = f.forgetMovedFolder(&api.Item{ID: "old", Deleted: &api.DeletedFacet{}})
	assert.True(t, moved)
	_, ok = f.dirCache.Get("Old")
	assert.False(t, ok)
}

// The Personal Vault as a personal drive's root listing gives it, abridged.
const personalVaultJSON = `{"id":"D1D1D1D1D1D1D1D1!s0001","name":"Persönlicher Tresor","size":0,
"parentReference":{"driveType":"personal","driveId":"D1D1D1D1D1D1D1D1","id":"D1D1D1D1D1D1D1D1!s0000","path":"/drives/D1D1D1D1D1D1D1D1/root:","siteId":"00000000-0000-0000-0000-000000000001"},
"remoteItem":{"id":"01VAULT","size":31638,"folder":{"childCount":3},"parentReference":{"driveType":"personal","driveId":"b!vault-drive"},"sharepointIds":{"siteId":"00000000-0000-0000-0000-000000000001"}}}`

func TestPersonalVaultIsLeftOut(t *testing.T) {
	var vault api.Item
	assert.NoError(t, json.Unmarshal([]byte(personalVaultJSON), &vault))
	assert.True(t, isPersonalVault(&vault))
	f := &Fs{driveID: "D1D1D1D1D1D1D1D1"}
	change, ok := f.itemChange(&vault)
	assert.True(t, ok)
	assert.True(t, change.Gone)

	shared := vault
	remote := *vault.RemoteItem
	remote.SharepointIDs = &api.SharepointIDs{SiteID: "another-site"}
	shared.RemoteItem = &remote
	assert.False(t, isPersonalVault(&shared))

	folder := api.Item{Name: "Documents", Folder: &api.FolderFacet{}, ParentReference: vault.ParentReference}
	assert.False(t, isPersonalVault(&folder))
}
