// Package export holds helpers shared by the export commands: safe local names, unique step file
// names, script encoding, and file writing. New exporters should build on these so every export
// produces files the same way.
package export

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafeName replaces characters that are not allowed in Windows or POSIX file names and trims
// surrounding whitespace.
func SafeName(name string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, name))
}

// UniqueName returns stem, or stem_2, stem_3, ... so that the result is not already in used
// (compared case-insensitively). The returned name is recorded in used.
func UniqueName(stem string, used map[string]bool) string {
	name, n := stem, 2
	for used[strings.ToLower(name)] {
		name = fmt.Sprintf("%s_%d", stem, n)
		n++
	}
	used[strings.ToLower(name)] = true
	return name
}

// EncodeScript converts script text to the bytes written to disk for the given extension.
// ".cmd" files use CRLF line endings and the OEM code page 857 (Turkish DOS), because cmd.exe
// reads batch files that way. Everything else is written as UTF-8 with a byte order mark, since
// Windows PowerShell 5.1 misreads BOM-less UTF-8 with non-ASCII characters.
func EncodeScript(script, ext string) ([]byte, error) {
	if ext == ".cmd" {
		return encodeCP857(strings.ReplaceAll(strings.ReplaceAll(script, "\r\n", "\n"), "\n", "\r\n"))
	}
	return append([]byte{0xEF, 0xBB, 0xBF}, script...), nil
}

// WriteFile writes data to path, creating parent directories as needed.
func WriteFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// cp857Upper maps bytes 0x80-0xFF of code page 857 to Unicode code points. Zero marks an
// undefined byte. The table is generated from Python's cp857 codec.
var cp857Upper = [128]rune{
	0x00C7, 0x00FC, 0x00E9, 0x00E2, 0x00E4, 0x00E0, 0x00E5, 0x00E7,
	0x00EA, 0x00EB, 0x00E8, 0x00EF, 0x00EE, 0x0131, 0x00C4, 0x00C5,
	0x00C9, 0x00E6, 0x00C6, 0x00F4, 0x00F6, 0x00F2, 0x00FB, 0x00F9,
	0x0130, 0x00D6, 0x00DC, 0x00F8, 0x00A3, 0x00D8, 0x015E, 0x015F,
	0x00E1, 0x00ED, 0x00F3, 0x00FA, 0x00F1, 0x00D1, 0x011E, 0x011F,
	0x00BF, 0x00AE, 0x00AC, 0x00BD, 0x00BC, 0x00A1, 0x00AB, 0x00BB,
	0x2591, 0x2592, 0x2593, 0x2502, 0x2524, 0x00C1, 0x00C2, 0x00C0,
	0x00A9, 0x2563, 0x2551, 0x2557, 0x255D, 0x00A2, 0x00A5, 0x2510,
	0x2514, 0x2534, 0x252C, 0x251C, 0x2500, 0x253C, 0x00E3, 0x00C3,
	0x255A, 0x2554, 0x2569, 0x2566, 0x2560, 0x2550, 0x256C, 0x00A4,
	0x00BA, 0x00AA, 0x00CA, 0x00CB, 0x00C8, 0x0000, 0x00CD, 0x00CE,
	0x00CF, 0x2518, 0x250C, 0x2588, 0x2584, 0x00A6, 0x00CC, 0x2580,
	0x00D3, 0x00DF, 0x00D4, 0x00D2, 0x00F5, 0x00D5, 0x00B5, 0x0000,
	0x00D7, 0x00DA, 0x00DB, 0x00D9, 0x00EC, 0x00FF, 0x00AF, 0x00B4,
	0x00AD, 0x00B1, 0x0000, 0x00BE, 0x00B6, 0x00A7, 0x00F7, 0x00B8,
	0x00B0, 0x00A8, 0x00B7, 0x00B9, 0x00B3, 0x00B2, 0x25A0, 0x00A0,
}

var cp857Encoding = func() map[rune]byte {
	m := make(map[rune]byte, len(cp857Upper))
	for i, r := range cp857Upper {
		if r != 0 {
			m[r] = byte(0x80 + i)
		}
	}
	return m
}()

func encodeCP857(s string) ([]byte, error) {
	var out bytes.Buffer
	for _, r := range s {
		if r < 0x80 {
			out.WriteByte(byte(r))
			continue
		}
		b, ok := cp857Encoding[r]
		if !ok {
			return nil, fmt.Errorf("character %q (U+%04X) cannot be written as code page 857", r, r)
		}
		out.WriteByte(b)
	}
	return out.Bytes(), nil
}
