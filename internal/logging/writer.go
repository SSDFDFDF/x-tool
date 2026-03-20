package logging

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type BufferedFileWriterOptions struct {
	MaxSizeBytes int64
	MaxBackups   int
}

type BufferedFileWriter struct {
	mu          sync.Mutex
	path        string
	file        *os.File
	buffer      *bufio.Writer
	stdout      io.Writer
	closed      bool
	closeMu     sync.Once
	stopCh      chan struct{}
	doneCh      chan struct{}
	flushAt     time.Duration
	currentSize int64
	maxSize     int64
	maxBackups  int
}

func NewBufferedFileWriter(path string, stdout io.Writer) (*BufferedFileWriter, error) {
	return NewBufferedFileWriterWithOptions(path, stdout, nil)
}

func NewBufferedFileWriterWithOptions(path string, stdout io.Writer, opts *BufferedFileWriterOptions) (*BufferedFileWriter, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("log path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	maxSize := int64(0)
	maxBackups := 0
	if opts != nil {
		if opts.MaxSizeBytes > 0 {
			maxSize = opts.MaxSizeBytes
		}
		if opts.MaxBackups > 0 {
			maxBackups = opts.MaxBackups
		}
	}

	writer := &BufferedFileWriter{
		path:        path,
		file:        file,
		buffer:      bufio.NewWriterSize(file, 64*1024),
		stdout:      stdout,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		flushAt:     500 * time.Millisecond,
		currentSize: info.Size(),
		maxSize:     maxSize,
		maxBackups:  maxBackups,
	}
	go writer.flushLoop()
	return writer, nil
}

func (w *BufferedFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, os.ErrClosed
	}

	if w.stdout != nil {
		if _, err := w.stdout.Write(p); err != nil {
			return 0, err
		}
	}
	if err := w.rotateIfNeededLocked(len(p)); err != nil {
		return 0, err
	}
	if _, err := w.buffer.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *BufferedFileWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	buffered := w.buffer.Buffered()
	if err := w.buffer.Flush(); err != nil {
		return err
	}
	w.currentSize += int64(buffered)
	return nil
}

func (w *BufferedFileWriter) Close() error {
	var closeErr error
	w.closeMu.Do(func() {
		close(w.stopCh)
		<-w.doneCh

		w.mu.Lock()
		defer w.mu.Unlock()

		if w.closed {
			return
		}
		w.closed = true

		if err := w.buffer.Flush(); err != nil {
			_ = w.file.Close()
			closeErr = err
			return
		}
		closeErr = w.file.Close()
	})
	return closeErr
}

func (w *BufferedFileWriter) flushLoop() {
	ticker := time.NewTicker(w.flushAt)
	defer ticker.Stop()
	defer close(w.doneCh)

	for {
		select {
		case <-ticker.C:
			_ = w.Flush()
		case <-w.stopCh:
			return
		}
	}
}

func (w *BufferedFileWriter) rotateIfNeededLocked(incomingBytes int) error {
	if w.maxSize <= 0 || w.maxBackups <= 0 {
		return nil
	}

	pendingSize := w.currentSize + int64(w.buffer.Buffered()) + int64(incomingBytes)
	if pendingSize <= w.maxSize {
		return nil
	}

	if w.currentSize == 0 && w.buffer.Buffered() == 0 {
		return nil
	}

	return w.rotateLocked()
}

func (w *BufferedFileWriter) rotateLocked() error {
	buffered := w.buffer.Buffered()
	if err := w.buffer.Flush(); err != nil {
		return err
	}
	w.currentSize += int64(buffered)
	if err := w.file.Close(); err != nil {
		return err
	}

	oldestBackup := rotatedLogPath(w.path, w.maxBackups)
	if err := os.Remove(oldestBackup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i := w.maxBackups - 1; i >= 1; i-- {
		src := rotatedLogPath(w.path, i)
		dst := rotatedLogPath(w.path, i+1)
		if err := os.Rename(src, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(w.path, rotatedLogPath(w.path, 1)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	w.file = file
	w.buffer = bufio.NewWriterSize(file, 64*1024)
	w.currentSize = 0
	return nil
}

func rotatedLogPath(path string, index int) string {
	return fmt.Sprintf("%s.%d", path, index)
}
