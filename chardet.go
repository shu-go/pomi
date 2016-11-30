package main

import (
	"bytes"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"
)

const (
	UTF8      = "utf-8-sig"
	UTF8N     = "utf-8"
	Shift_JIS = "cp932"
)

var UTF8BOM = []byte{0xef, 0xbb, 0xbf}

func Chardet(test []byte) string {
	if len(test) == 0 {
		return UTF8
	}

	encs := make(map[string]encoding.Encoding)
	encs[Shift_JIS] = japanese.ShiftJIS
	encs[UTF8] = encoding.Nop

	if bytes.Index(test, UTF8BOM) == 0 {
		return UTF8
	}

	for name, enc := range encs {
		dec := enc.NewDecoder()
		buf := bytes.Buffer{}
		t := transform.NewWriter(&buf, dec)
		_, err := t.Write(test)
		if err == nil {
			t.Close()

			return name
		}
	}

	return UTF8N
}
