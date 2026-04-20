package storage

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
)

// LocalStorage stores export files on the local filesystem.
// Suitable for single-node deployments and development.
// For multi-pod Kubernetes deployments use S3Storage instead.
type LocalStorage struct {
	dir string
}

// NewLocalStorage creates a LocalStorage that writes files under dir.
// The directory is created if it does not exist.
func NewLocalStorage(dir string) (*LocalStorage, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &LocalStorage{dir: dir}, nil
}

// Create opens a new file at dir/name for streaming writes.
func (s *LocalStorage) Create(name string) (ExportWriter, error) {
	path := filepath.Join(s.dir, name)
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &localWriter{f: f, path: path}, nil
}

// ServeDownload streams the file at ref to the HTTP response using http.ServeContent
// so Range requests and conditional GETs are handled correctly.
func (s *LocalStorage) ServeDownload(_ context.Context, ref, filename string, w http.ResponseWriter, r *http.Request) error {
	cleanPath := filepath.Clean(ref)
	f, err := os.Open(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "export file not found", http.StatusNotFound)
			return nil
		}
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	http.ServeContent(w, r, filename, stat.ModTime(), f)
	return nil
}

// localWriter writes to a temp file and returns the final path on Commit.
type localWriter struct {
	f    *os.File
	path string
}

func (lw *localWriter) Write(p []byte) (int, error) { return lw.f.Write(p) }

func (lw *localWriter) Commit() (string, error) {
	if err := lw.f.Close(); err != nil {
		return "", err
	}
	return lw.path, nil
}

func (lw *localWriter) Abort() {
	lw.f.Close()
	if err := os.Remove(lw.path); err != nil {
		slog.Warn("failed to remove aborted export file", "path", lw.path)
	}
}
