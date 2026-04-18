package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/CCoupel/GhostDrive/plugins"
)

// progressMockBackend is a mockBackend that fires the progress callback once per transfer.
type progressMockBackend struct {
	*mockBackend
}

func (p *progressMockBackend) Upload(ctx context.Context, local, remote string, progress plugins.ProgressCallback) error {
	data, err := os.ReadFile(local)
	if err != nil {
		return err
	}
	total := int64(len(data))
	if progress != nil {
		progress(total/2, total)
		progress(total, total)
	}
	info, _ := os.Stat(local)
	p.files[remote] = plugins.FileInfo{
		Name:    filepath.Base(remote),
		Path:    remote,
		Size:    total,
		ModTime: info.ModTime(),
	}
	return nil
}

func (p *progressMockBackend) Download(ctx context.Context, remote, local string, progress plugins.ProgressCallback) error {
	fi, ok := p.files[remote]
	if !ok {
		return os.ErrNotExist
	}
	if err := os.MkdirAll(filepath.Dir(local), 0755); err != nil {
		return err
	}
	if progress != nil {
		progress(fi.Size/2, fi.Size)
		progress(fi.Size, fi.Size)
	}
	return os.WriteFile(local, make([]byte, fi.Size), 0644)
}

// failingMockBackend is a mockBackend whose Upload and Download always return errors.
type failingMockBackend struct {
	*mockBackend
}

func (f *failingMockBackend) Upload(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return errors.New("upload: simulated backend error")
}

func (f *failingMockBackend) Download(_ context.Context, _, _ string, _ plugins.ProgressCallback) error {
	return errors.New("download: simulated backend error")
}
