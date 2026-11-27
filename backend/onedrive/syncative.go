package onedrive

// Added for Syncative: one-off queries of the drive's change log (its delta listing), which ChangeNotify otherwise runs on a timer. The delta listing names items by ID and their parent's ID; it gives no paths (https://learn.microsoft.com/en-us/graph/api/driveitem-delta, Remarks). ItemChangesSince reports the items by ID, as listings name them; ChangesSince places them by the folders the directory cache knows.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/rclone/rclone/backend/onedrive/api"
	"github.com/rclone/rclone/fs"
)

// ItemChange is one item of the change log as it is now, named by ID as listings name it (Item.GetID).
type ItemChange struct {
	ID string
	// ParentID is the ID of the folder holding the item.
	ParentID string
	// Name in rclone's standard encoding, as listings give it.
	Name    string
	IsDir   bool
	ModTime time.Time
	// Gone: deleted, or hidden from listings (a OneNote notebook), with everything below it.
	Gone bool
}

// ChangesRoot returns the ID of folder dir below the root, as listings name it; fs.ErrorDirNotFound when it is missing.
func (f *Fs) ChangesRoot(ctx context.Context, dir string) (string, error) {
	return f.dirCache.FindDir(ctx, dir, false)
}

// LatestItemEvent returns where the change log stands now, for ItemChangesSince.
func (f *Fs) LatestItemEvent(ctx context.Context) (string, error) {
	return f.changeNotifyStartPageToken(ctx)
}

// ItemChangesSince returns the items that changed after token, and the token after them. relist: the drive can't tell the changes since token (it expired); next is then where the log stands now.
func (f *Fs) ItemChangesSince(ctx context.Context, token string) (next string, relist bool, items []ItemChange, err error) {
	next, err = f.deltaSince(ctx, token, func(item *api.Item) {
		if change, ok := f.itemChange(item); ok {
			items = append(items, change)
		}
	})
	if errors.Is(err, errResyncRequired) {
		next, err = f.changeNotifyStartPageToken(ctx)
		return next, true, nil, err
	}
	if err != nil {
		return "", false, nil, err
	}
	return next, false, items, nil
}

// ChangesStartToken returns where the change log stands now, for ChangesSince.
func (f *Fs) ChangesStartToken(ctx context.Context) (string, error) {
	return f.changeNotifyStartPageToken(ctx)
}

// ChangesSince calls notifyFunc for the folders below the root the directory cache knows that changed after token: a changed folder, where it was and where it is, and the folder holding a changed file. Returns the token after them; "" when the drive can't tell the changes since token. A file moved away is reported by its new folder only: the cache knows no files.
func (f *Fs) ChangesSince(ctx context.Context, notifyFunc func(string, fs.EntryType), token string) (string, error) {
	next, err := f.deltaSince(ctx, token, func(item *api.Item) {
		id := f.itemID(item)
		if old, ok := f.dirCache.GetInv(id); ok {
			notifyFunc(old, fs.EntryDirectory)
		}
		if parent := item.GetParentReference(); parent != nil && parent.ID != "" {
			if dir, ok := f.dirCache.GetInv(parent.GetID()); ok {
				notifyFunc(dir, fs.EntryDirectory)
				if item.GetFolder() != nil && item.Deleted == nil {
					notifyFunc(path.Join(dir, f.opt.Enc.ToStandardName(item.GetName())), fs.EntryDirectory)
				}
			}
		}
	})
	if errors.Is(err, errResyncRequired) {
		return "", nil
	}
	return next, err
}

// errResyncRequired: the drive answered a delta token with 410 Gone, it can't list the changes since then.
var errResyncRequired = errors.New("onedrive: the change log can't be followed from this token")

// deltaSince calls fn for every item of the delta listing after token and returns the token after them.
func (f *Fs) deltaSince(ctx context.Context, token string, fn func(*api.Item)) (string, error) {
	nextLink := ""
	for {
		opts := f.buildDriveDeltaOpts(token, nextLink)
		var delta api.DeltaResponse
		var resp *http.Response
		err := f.pacer.Call(func() (bool, error) {
			var err error
			resp, err = f.srv.CallJSON(ctx, &opts, nil, &delta)
			return shouldRetry(ctx, resp, err)
		})
		if resp != nil && resp.StatusCode == http.StatusGone {
			return "", errResyncRequired
		}
		if err != nil {
			return "", err
		}
		for i := range delta.Value {
			fn(&delta.Value[i])
		}
		if delta.NextLink == "" {
			parsed, err := url.Parse(delta.DeltaLink)
			if err != nil {
				return "", err
			}
			next := parsed.Query().Get("token")
			if next == "" {
				return "", fmt.Errorf("onedrive: no token in the delta link %q", delta.DeltaLink)
			}
			return next, nil
		}
		nextLink = delta.NextLink
	}
}

// itemChange is item as ItemChangesSince reports it; false for the drive's root.
func (f *Fs) itemChange(item *api.Item) (ItemChange, bool) {
	id := f.itemID(item)
	if item.Deleted != nil || (!f.opt.ExposeOneNoteFiles && item.GetPackageType() == api.PackageTypeOneNote) {
		return ItemChange{ID: id, Gone: true}, true
	}
	parent := item.GetParentReference()
	if parent == nil || parent.ID == "" {
		return ItemChange{}, false
	}
	// The time listings give (Object.setMetaData).
	modTime := time.Time(item.GetLastModifiedDateTime())
	if info := item.GetFileSystemInfo(); info != nil {
		modTime = time.Time(info.LastModifiedDateTime)
	}
	return ItemChange{
		ID:       id,
		ParentID: parent.GetID(),
		Name:     f.opt.Enc.ToStandardName(item.GetName()),
		IsDir:    item.GetFolder() != nil,
		ModTime:  modTime,
	}, true
}

// itemID is the ID listings give item; a deleted item may come without the drive it was on, which is then this one.
func (f *Fs) itemID(item *api.Item) string {
	id := item.GetID()
	if !strings.Contains(id, "#") {
		id = f.driveID + "#" + id
	}
	return id
}
