package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBufferedFileWriterRotatesBySize(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "app.log")
	writer, err := NewBufferedFileWriterWithOptions(logPath, nil, &BufferedFileWriterOptions{
		MaxSizeBytes: 20,
		MaxBackups:   2,
	})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})

	if _, err := writer.Write([]byte("first-line\n")); err != nil {
		t.Fatalf("write first line: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush first line: %v", err)
	}
	if _, err := writer.Write([]byte("second-line\n")); err != nil {
		t.Fatalf("write second line: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush second line: %v", err)
	}

	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	rotated, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("read rotated log: %v", err)
	}

	if got := string(current); got != "second-line\n" {
		t.Fatalf("expected current log to contain latest line, got %q", got)
	}
	if got := string(rotated); got != "first-line\n" {
		t.Fatalf("expected rotated log to contain previous line, got %q", got)
	}
}

func TestBufferedFileWriterHonorsBackupLimit(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "app.log")
	writer, err := NewBufferedFileWriterWithOptions(logPath, nil, &BufferedFileWriterOptions{
		MaxSizeBytes: 10,
		MaxBackups:   2,
	})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	t.Cleanup(func() {
		_ = writer.Close()
	})

	lines := []string{"aaaaa\n", "bbbbb\n", "ccccc\n", "ddddd\n"}
	for _, line := range lines {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatalf("write line %q: %v", line, err)
		}
		if err := writer.Flush(); err != nil {
			t.Fatalf("flush line %q: %v", line, err)
		}
	}

	if _, err := os.Stat(logPath + ".3"); !os.IsNotExist(err) {
		t.Fatalf("expected third backup to be removed, stat err=%v", err)
	}

	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read current log: %v", err)
	}
	backup1, err := os.ReadFile(logPath + ".1")
	if err != nil {
		t.Fatalf("read first backup: %v", err)
	}
	backup2, err := os.ReadFile(logPath + ".2")
	if err != nil {
		t.Fatalf("read second backup: %v", err)
	}

	if !strings.Contains(string(current), "ddddd") {
		t.Fatalf("expected current log to contain latest content, got %q", string(current))
	}
	if !strings.Contains(string(backup1), "ccccc") {
		t.Fatalf("expected first backup to contain previous content, got %q", string(backup1))
	}
	if !strings.Contains(string(backup2), "bbbbb") {
		t.Fatalf("expected second backup to contain older content, got %q", string(backup2))
	}
}
