package main

import (
	"io/ioutil"
	"path/filepath"
	"sort"
	"testing"

	"bitbucket.org/shu/log"
)

func TestPutAndListAndGet(t *testing.T) {
	testdata := []struct {
		Name string
		Data string
	}{
		{
			Name: "test.md",
			Data: "テストファイルです。\r\nテストファイルなんです。",
		},
		{
			Name: "test2.txt",
			Data: "テストファイル 2 です。\r\nテストファイルなんです。",
		},
		{
			Name: "te.txt",
			Data: "テです。\r\nテなんです。",
		},
	}

	setupLocal(t)

	config, ic := getTestFixtures()
	if err := ic.Create(config.IMAP.Box); err != nil {
		t.Fatalf("failed to create box %q: %v", config.IMAP.Box, err)
	}
	setupTestBox(t, config, ic)
	ic.Logout()

	// test

	if err := ioutil.WriteFile("pomera_sync/"+testdata[0].Name, []byte(testdata[0].Data), 0x664); err != nil {
		t.Errorf("failed to write a file %v: %v", testdata[0].Name, err)
	}
	if err := ioutil.WriteFile("pomera_sync/"+testdata[1].Name, []byte(testdata[0].Data), 0x664); err != nil {
		t.Errorf("failed to write a file %v: %v", testdata[1].Name, err)
	}
	if count, err := putMessages(config, "pomera_sync", []string{"*"}, "", nil); err != nil {
		t.Errorf("failed to put messages: %v", err)
	} else if count != 2 {
		t.Errorf("wrong put count %v", count)
	}

	log.Debug("=================")

	// overwrite
	if err := ioutil.WriteFile("pomera_sync/"+testdata[1].Name, []byte(testdata[1].Data), 0x664); err != nil {
		t.Errorf("failed to write a file %v: %v", testdata[1].Name, err)
	}
	if count, err := putMessages(config, "pomera_sync", []string{"test2.txt"}, "", nil); err != nil {
		t.Errorf("failed to put messages: %v", err)
	} else if count != 1 {
		t.Errorf("wrong put count %v", count)
	}

	log.Debug("=================")

	// substring name
	if err := ioutil.WriteFile("pomera_sync/"+testdata[2].Name, []byte(testdata[2].Data), 0x664); err != nil {
		t.Errorf("failed to write a file %v: %v", testdata[2].Name, err)
	}
	if count, err := putMessages(config, "pomera_sync", []string{"te.txt"}, "", nil); err != nil {
		t.Errorf("failed to put messages: %v", err)
	} else if count != 1 {
		t.Errorf("wrong put count %v", count)
	}

	log.Debug("=================")

	// test1, test2, and te do not collide
	if count, err := putMessages(config, "pomera_sync", []string{"*"}, "", nil); err != nil {
		t.Errorf("failed to put messages: %v", err)
	} else if count != 3 {
		t.Errorf("wrong put count %v", count)
	}

	log.Debug("=================")

	ic, _ = initIMAP(config)
	if list, err := listMessages(ic, "", ""); err != nil {
		t.Errorf("failed to list messages: %v", err)

	} else {
		sort.Slice(list, func(i, j int) bool {
			return list[i].Date < list[j].Date
		})
		if len(list) != 3 ||
			list[0].Subject+".md" != testdata[0].Name ||
			list[1].Subject+".txt" != testdata[1].Name ||
			list[2].Subject+".txt" != testdata[2].Name {
			//
			t.Errorf("wrong messages (%v) are in box %q", len(list), config.IMAP.Box)
			for i, e := range list {
				t.Log(i, e)
			}
		}
	}

	log.Debug("=================")

	wipeoutLocalFiles(t, "pomera_sync")
	if err := getMessages(ic, false, true, "", "", "pomera_sync", "txt", filesWriter); err != nil {
		t.Errorf("failed to get messages: %v", err)
	}

	filenames, err := filepath.Glob("pomera_sync/*")
	if err != nil {
		t.Errorf("glob error: %v", err)

	} else if len(filenames) != 3 {
		t.Errorf("wrong files (%v) are in pomera_sync", len(filenames))
		for i, fn := range filenames {
			t.Log(i, fn)
		}

	} else {
		for _, f := range testdata {
			if data, err := ioutil.ReadFile("pomera_sync/" + f.Name); err != nil {
				t.Errorf("failed to read %v: %v", "test.txt", err)
			} else if string(data[3:] /* exclude BOM */) != f.Data {
				t.Errorf("wrong content %q", string(data))
			}
		}

	}

	// teardown

	teardownTestBox(t, config, ic)
	ic.Logout()
	teardownLocal(t)
}
