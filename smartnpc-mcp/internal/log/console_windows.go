//go:build windows

package log

import "golang.org/x/sys/windows"

// EnableUTF8Console switches the current Windows console's output code page to
// UTF-8 (65001) so non-ASCII bytes written to stderr (e.g. em-dash, CJK from
// LLM-produced reasons) render correctly instead of mojibake under the default
// GBK / cp936 codepage. Failure is non-fatal; the process keeps running with
// whatever codepage was in effect.
//
// Only the *output* codepage is touched — input stays untouched so that any
// Windows-side child-process plumbing keeps working.
func EnableUTF8Console() {
	const cpUTF8 = 65001
	_ = windows.SetConsoleOutputCP(cpUTF8)
}
