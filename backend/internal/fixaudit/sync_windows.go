//go:build windows

package fixaudit

// The deployed Linux container fsyncs directories; Windows supports file Sync only.
func syncDirectory(string) error { return nil }
