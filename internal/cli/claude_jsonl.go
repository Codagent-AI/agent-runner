package cli

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// claudeJSONLReader consumes each byte once, retaining incomplete append writes
// until a newline arrives. Positions include the exact bytes of line endings.
type claudeJSONLReader struct {
	file    *os.File
	offset  int64
	pending []byte
}

func openClaudeScopedFile(path string) (*os.File, error) {
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.Open(filepath.Base(path))
}

func (r *claudeJSONLReader) read(ctx context.Context, final bool, consume func([]byte, int64) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stat, err := r.file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() < r.offset {
		return errors.New("claude JSONL file truncated")
	}
	if _, err = r.file.Seek(r.offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(io.LimitReader(r.file, stat.Size()-r.offset))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fragment, readErr := reader.ReadSlice('\n')
		r.offset += int64(len(fragment))
		r.pending = append(r.pending, fragment...)
		if readErr == nil || (readErr == io.EOF && final && len(r.pending) > 0) {
			if err := ctx.Err(); err != nil {
				return err
			}
			err := consume(r.pending, r.offset)
			r.pending = nil
			if err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil && readErr != bufio.ErrBufferFull {
			return readErr
		}
	}
}
