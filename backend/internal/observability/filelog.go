package observability

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Logs live in files, not in the database (поручение пользователя 06.11.2026):
// one file per day named by the Bishkek date, <dir>/2026-11-06.log, one JSON
// event per line. Files older than LogKeepDays days are deleted.
const LogKeepDays = 7

const logFileExt = ".log"
const logDayLayout = "2006-01-02"

// LogZone names the log files: the gateway's working day is Bishkek time.
var LogZone = time.FixedZone("Asia/Bishkek", 6*60*60)

type FileLog struct {
	dir string

	writeMu sync.Mutex
	file    *os.File
	fileDay string

	readMu sync.Mutex
	days   map[string]*logDay
}

// logDay caches one file's parsed events; a file only grows, so a later read
// parses just the appended lines.
type logDay struct {
	offset int64
	events []Event
}

func OpenFileLog(dir string) (*FileLog, error) {
	if dir == "" {
		return nil, errors.New("log directory required")
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, err
	}
	return &FileLog{dir: dir, days: map[string]*logDay{}}, nil
}

func (l *FileLog) Dir() string { return l.dir }

func logDayOf(t time.Time) string { return t.In(LogZone).Format(logDayLayout) }

func (l *FileLog) path(day string) string { return filepath.Join(l.dir, day+logFileExt) }

// Append writes the events to the file of their day and syncs it.
func (l *FileLog) Append(events ...Event) error {
	if len(events) == 0 {
		return nil
	}
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	var buf bytes.Buffer
	day := ""
	flush := func() error {
		if buf.Len() == 0 {
			return nil
		}
		if err := l.open(day); err != nil {
			return err
		}
		if _, err := l.file.Write(buf.Bytes()); err != nil {
			return err
		}
		buf.Reset()
		return l.file.Sync()
	}
	for i := range events {
		e := events[i]
		e.Normalize()
		d := logDayOf(e.Timestamp)
		if d != day {
			if err := flush(); err != nil {
				return err
			}
			day = d
		}
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	return flush()
}

func (l *FileLog) open(day string) error {
	if l.file != nil && l.fileDay == day {
		return nil
	}
	f, err := os.OpenFile(l.path(day), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0640)
	if err != nil {
		return err
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	l.file, l.fileDay = f, day
	return nil
}

func (l *FileLog) Close() error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file, l.fileDay = nil, ""
	return err
}

// Days lists the days that have a log file, newest first.
func (l *FileLog) Days() ([]string, error) {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, err
	}
	var days []string
	for _, e := range entries {
		day, ok := strings.CutSuffix(e.Name(), logFileExt)
		if !ok || e.IsDir() {
			continue
		}
		if _, err := time.ParseInLocation(logDayLayout, day, LogZone); err != nil {
			continue
		}
		days = append(days, day)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	return days, nil
}

// Events returns the events of the days from..to (inclusive, zero = open),
// newest day first; within a day in write order. The slice is shared: callers
// must not change the events.
func (l *FileLog) Events(from, to time.Time) ([][]Event, error) {
	days, err := l.Days()
	if err != nil {
		return nil, err
	}
	first, last := "", ""
	if !from.IsZero() {
		first = logDayOf(from)
	}
	if !to.IsZero() {
		last = logDayOf(to)
	}
	var out [][]Event
	for _, day := range days {
		if (first != "" && day < first) || (last != "" && day > last) {
			continue
		}
		events, err := l.day(day)
		if err != nil {
			return nil, err
		}
		out = append(out, events)
	}
	return out, nil
}

func (l *FileLog) day(day string) ([]Event, error) {
	l.readMu.Lock()
	defer l.readMu.Unlock()
	cached := l.days[day]
	if cached == nil {
		cached = &logDay{}
	}
	f, err := os.Open(l.path(day))
	if err != nil {
		if os.IsNotExist(err) {
			delete(l.days, day)
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < cached.offset {
		cached = &logDay{}
	}
	if st.Size() > cached.offset {
		if _, err := f.Seek(cached.offset, io.SeekStart); err != nil {
			return nil, err
		}
		r := bufio.NewReaderSize(f, 64<<10)
		for {
			line, err := r.ReadBytes('\n')
			if err == io.EOF {
				// An unfinished last line is read again once it is complete.
				break
			}
			if err != nil {
				return nil, err
			}
			cached.offset += int64(len(line))
			var e Event
			if json.Unmarshal(line, &e) == nil {
				cached.events = append(cached.events, e)
			}
		}
	}
	l.days[day] = cached
	return cached.events, nil
}

// Cleanup deletes the files of days older than LogKeepDays, today included.
func (l *FileLog) Cleanup(now time.Time) error {
	days, err := l.Days()
	if err != nil {
		return err
	}
	oldest := logDayOf(now.AddDate(0, 0, -(LogKeepDays - 1)))
	var errs []error
	for _, day := range days {
		if day >= oldest {
			continue
		}
		if err := os.Remove(l.path(day)); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
			continue
		}
		l.readMu.Lock()
		delete(l.days, day)
		l.readMu.Unlock()
	}
	return errors.Join(errs...)
}
