package logging

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type LogEntry struct {
	ID      int64          `json:"id"`
	Time    string         `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Text    string         `json:"text"`
}

type LogFileMeta struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type LogStore struct {
	path   string
	writer *BufferedFileWriter
}

var (
	textLogTimePattern  = regexp.MustCompile(`(?:^| )time=("([^"\\]|\\.)*"|[^ ]+)`)
	textLogLevelPattern = regexp.MustCompile(`(?:^| )level=("([^"\\]|\\.)*"|[^ ]+)`)
	textLogMsgPattern   = regexp.MustCompile(`(?:^| )msg=("([^"\\]|\\.)*"|[^ ]+)`)
)

func NewLogStore(path string, writer *BufferedFileWriter) *LogStore {
	return &LogStore{
		path:   path,
		writer: writer,
	}
}

func (s *LogStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *LogStore) Flush() error {
	if s == nil || s.writer == nil {
		return nil
	}
	return s.writer.Flush()
}

func (s *LogStore) Close() error {
	if s == nil || s.writer == nil {
		return nil
	}
	return s.writer.Close()
}

func (s *LogStore) List(limit int) []LogEntry {
	if s == nil || strings.TrimSpace(s.path) == "" || limit <= 0 {
		return []LogEntry{}
	}
	_ = s.Flush()

	lines, err := tailLogLines(s.path, limit)
	if err != nil {
		return []LogEntry{}
	}

	entries := make([]LogEntry, 0, len(lines))
	for index, line := range lines {
		entry, ok := parseLogLine(line)
		if !ok {
			continue
		}
		entry.ID = int64(index + 1)
		entries = append(entries, entry)
	}
	return entries
}

func (s *LogStore) Meta() LogFileMeta {
	meta := LogFileMeta{
		Path: s.Path(),
	}
	if s == nil || strings.TrimSpace(s.path) == "" {
		return meta
	}

	_ = s.Flush()
	info, err := os.Stat(s.path)
	if err != nil {
		return meta
	}

	meta.Exists = true
	meta.SizeBytes = info.Size()
	meta.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
	return meta
}

func (s *LogStore) Raw(limit int) string {
	if s == nil || strings.TrimSpace(s.path) == "" || limit <= 0 {
		return ""
	}
	_ = s.Flush()

	lines, err := tailLogLines(s.path, limit)
	if err != nil {
		return ""
	}
	return strings.Join(lines, "\n")
}

func (s *LogStore) Subscribe(buffer int) (<-chan LogEntry, func()) {
	if buffer < 1 {
		buffer = 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan LogEntry, buffer)
	go s.follow(ctx, ch)
	return ch, cancel
}

func (s *LogStore) follow(ctx context.Context, ch chan LogEntry) {
	defer close(ch)

	if s == nil || strings.TrimSpace(s.path) == "" {
		<-ctx.Done()
		return
	}

	offset := currentFileSize(s.path)
	var nextID int64 = time.Now().UnixNano()
	var carry string
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Flush()

			size := currentFileSize(s.path)
			if size < offset {
				offset = 0
				carry = ""
			}
			if size == offset {
				continue
			}

			data, err := readLogRange(s.path, offset, size)
			if err != nil {
				continue
			}
			offset = size

			combined := carry + string(data)
			lines := strings.Split(combined, "\n")
			if !strings.HasSuffix(combined, "\n") {
				carry = lines[len(lines)-1]
				lines = lines[:len(lines)-1]
			} else {
				carry = ""
			}

			for _, line := range lines {
				entry, ok := parseLogLine(line)
				if !ok {
					continue
				}
				nextID++
				entry.ID = nextID
				select {
				case ch <- entry:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func tailLogLines(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	const chunkSize int64 = 64 * 1024
	position := size
	buffer := make([]byte, 0, chunkSize)
	newlines := 0

	for position > 0 && newlines <= limit {
		readSize := chunkSize
		if position < readSize {
			readSize = position
		}
		position -= readSize

		chunk := make([]byte, int(readSize))
		if _, err := file.ReadAt(chunk, position); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}

		buffer = append(chunk, buffer...)
		newlines = bytes.Count(buffer, []byte{'\n'})
	}

	rawLines := strings.Split(string(buffer), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}

func readLogRange(path string, start, end int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	length := end - start
	if length <= 0 {
		return nil, nil
	}

	data := make([]byte, int(length))
	n, err := file.ReadAt(data, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return data[:n], nil
}

func currentFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func parseLogLine(line string) (LogEntry, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return LogEntry{}, false
	}

	entry := LogEntry{
		Time:    extractTextLogField(textLogTimePattern, trimmed),
		Level:   strings.ToUpper(extractTextLogField(textLogLevelPattern, trimmed)),
		Message: extractTextLogField(textLogMsgPattern, trimmed),
		Text:    trimmed,
	}
	if entry.Level == "" {
		entry.Level = "INFO"
	}
	if entry.Message == "" {
		entry.Message = "log.line"
	}
	return entry, true
}

func extractTextLogField(pattern *regexp.Regexp, line string) string {
	match := pattern.FindStringSubmatch(line)
	if len(match) < 2 {
		return ""
	}
	value := match[1]
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return value
}
