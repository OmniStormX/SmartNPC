//go:build !windows

package log

// EnableUTF8Console is a no-op on non-Windows platforms; their consoles are
// UTF-8 by default.
func EnableUTF8Console() {}
