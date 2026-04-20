// Package storage abstracts export file storage so the application can use
// local disk during development and Amazon S3 (or any compatible object store)
// in production without changing the reporting or download logic.
package storage

import (
	"context"
	"io"
	"net/http"
)

// ExportStorage is the interface that wraps file creation and download serving.
type ExportStorage interface {
	// Create opens a new file for streaming writes.
	// The caller must call either Commit (success) or Abort (failure) on the
	// returned writer — never both, and never leave it open.
	Create(name string) (ExportWriter, error)

	// ServeDownload delivers a previously stored file to an HTTP response.
	// For local storage this streams the file directly.
	// For S3 this issues a 307 redirect to a short-lived presigned URL.
	ServeDownload(ctx context.Context, ref, filename string, w http.ResponseWriter, r *http.Request) error
}

// ExportWriter is returned by ExportStorage.Create.
type ExportWriter interface {
	io.Writer
	// Commit finalises the write and returns the stable storage reference
	// (file path for local, S3 object key for S3) to be stored in the DB.
	Commit() (ref string, err error)
	// Abort discards the in-progress write without persisting anything.
	Abort()
}
