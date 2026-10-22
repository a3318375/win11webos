// Package storage defines provider-independent contracts. Implementations must not
// expose credentials through these public models.
package storage

import (
	"context"
	"errors"
	"time"
)

var ErrNotSupported = errors.New("operation not supported by storage provider")

type Capabilities struct {
	Upload         bool `json:"upload"`
	Search         bool `json:"search"`
	Trash          bool `json:"trash"`
	DirectDownload bool `json:"directDownload"`
}

type Entry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	ParentID   string    `json:"parentId"`
	Directory  bool      `json:"directory"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
}

type Page struct {
	Items      []Entry `json:"items"`
	NextCursor string  `json:"nextCursor,omitempty"`
}

// Adapter instances belong to one account. Authentication and HTTP transport are
// internal to the implementation. All I/O must honor cancellation via ctx.
type Adapter interface {
	Capabilities() Capabilities
	List(ctx context.Context, parentID, cursor string) (Page, error)
	Stat(ctx context.Context, id string) (Entry, error)
}

// Download resolution and streaming will be a separate contract in transfer,
// rather than leaking upstream URLs or authorization headers to API consumers.
