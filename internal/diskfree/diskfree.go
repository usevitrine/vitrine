// Package diskfree tells how big a disk is and how much of it is free.
package diskfree

// Usage is a disk's size and free space in bytes (free: what this program may use).
type Usage struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}
