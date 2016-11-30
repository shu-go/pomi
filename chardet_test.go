package main

import (
	"io/ioutil"
	"testing"
)

func testChardetDecode(t *testing.T, filename, name, text string) {
	test, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Error(err)
	}
	encName := Chardet(test)
	if encName != name {
		t.Error("encName=%v, decoded=%q", encName, string(test))
	}

}

func TestChardetDecode(t *testing.T) {
	data := []struct {
		FileName string
		Name     string
		Text     string
	}{
		{
			FileName: "test_sjis.txt",
			Name:     Shift_JIS,
			Text:     "Shift_JIS のテキストです。",
		},
		{
			FileName: "test_utf8.txt",
			Name:     UTF8,
			Text:     string(UTF8BOM) + "UTF-8 のテキストです。",
		},
		{
			FileName: "test_utf8n.txt",
			Name:     UTF8,
			Text:     "UTF-8 BOM 無し のテキストです。",
		},
	}

	for _, d := range data {
		testChardetDecode(t, d.FileName, d.Name, d.Text)
	}
}
