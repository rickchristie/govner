package view

import "unicode/utf8"

func trimLastRune(s string) string {
	if s == "" {
		return s
	}
	_, size := utf8.DecodeLastRuneInString(s)
	if size == 0 {
		return ""
	}
	return s[:len(s)-size]
}
