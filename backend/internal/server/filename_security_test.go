package server

import "testing"

func TestFileNamesRejectUnicodeFormatCharacters(t *testing.T) {
	for _, char := range []rune{'\u061c', '\u200b', '\u200d', '\u200e', '\u200f', '\u202d', '\u202e', '\u2066', '\u2067', '\u2068', '\u2069', '\ufeff', '\U000E0001'} {
		name := "report" + string(char) + ".txt"
		if transferName(name) != "" || validTransferPath("dir/"+name, false) || validateUploadName(name) == nil {
			t.Fatalf("format character U+%04X accepted", char)
		}
	}
	if transferName("中文-é.txt") == "" || !validTransferPath("资料/中文-é.txt", false) || validateUploadName("中文-é.txt") != nil {
		t.Fatal("ordinary Unicode filename rejected")
	}
}
