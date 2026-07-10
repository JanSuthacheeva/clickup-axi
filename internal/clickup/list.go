package clickup

import (
	"net/http"
	"net/url"
	"strconv"
)

// FolderlessList is the explicit folder context rendered for a list
// directly in a space. It keeps duplicate list names distinguishable.
const FolderlessList = "(folderless)"

// ListRef is the minimum stable list identity needed for discovery and
// later name resolution. Folder is either a containing folder's name or
// FolderlessList.
type ListRef struct {
	ID     string
	Name   string
	Folder string
}

type folderRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type listRefWire struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetSpaceLists returns a complete list inventory for one space. In
// active mode it traverses active folders only. In archived mode it
// queries both active and archived folders because archive state belongs
// to the list, not its parent folder. A failure returns no partial data.
func (c *Client) GetSpaceLists(spaceID string, archived bool) ([]ListRef, *APIError) {
	folderless, err := c.getFolderlessLists(spaceID, archived)
	if err != nil {
		return nil, err
	}

	folderModes := []bool{false}
	if archived {
		folderModes = append(folderModes, true)
	}
	folders := make([]folderRef, 0)
	seenFolders := make(map[string]bool)
	for _, folderArchived := range folderModes {
		found, err := c.getFolders(spaceID, folderArchived)
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			if seenFolders[f.ID] {
				continue
			}
			seenFolders[f.ID] = true
			folders = append(folders, f)
		}
	}

	refs := make([]ListRef, 0, len(folderless))
	for _, l := range folderless {
		refs = append(refs, ListRef{ID: l.ID, Name: l.Name, Folder: FolderlessList})
	}
	for _, f := range folders {
		lists, err := c.getFolderLists(f.ID, archived)
		if err != nil {
			return nil, err
		}
		for _, l := range lists {
			refs = append(refs, ListRef{ID: l.ID, Name: l.Name, Folder: f.Name})
		}
	}
	return refs, nil
}

func (c *Client) getFolderlessLists(spaceID string, archived bool) ([]listRefWire, *APIError) {
	var out struct {
		Lists []listRefWire `json:"lists"`
	}
	if err := c.do(http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/list?archived="+strconv.FormatBool(archived), nil, &out); err != nil {
		return nil, err
	}
	return out.Lists, nil
}

func (c *Client) getFolders(spaceID string, archived bool) ([]folderRef, *APIError) {
	var out struct {
		Folders []folderRef `json:"folders"`
	}
	if err := c.do(http.MethodGet, "/space/"+url.PathEscape(spaceID)+"/folder?archived="+strconv.FormatBool(archived), nil, &out); err != nil {
		return nil, err
	}
	return out.Folders, nil
}

func (c *Client) getFolderLists(folderID string, archived bool) ([]listRefWire, *APIError) {
	var out struct {
		Lists []listRefWire `json:"lists"`
	}
	if err := c.do(http.MethodGet, "/folder/"+url.PathEscape(folderID)+"/list?archived="+strconv.FormatBool(archived), nil, &out); err != nil {
		return nil, err
	}
	return out.Lists, nil
}
