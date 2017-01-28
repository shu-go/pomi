package main

import (
	"io/ioutil"
	"sort"
	"testing"

	"bitbucket.org/shu/log"
)

func TestPutAndList(t *testing.T) {
	setupLocal(t)

	config, ic := getTestFixtures()
	if err := ic.Create(config.IMAP.Box); err != nil {
		t.Fatalf("failed to create box %q: %v", config.IMAP.Box, err)
	}
	setupTestBox(t, config, ic)
	ic.Logout()

	// test

	ioutil.WriteFile("pomera_sync/テスト.txt", []byte("テストファイルです。\r\nテストファイルなんです。"), 0x600)
	ioutil.WriteFile("pomera_sync/テスト2.txt", []byte("テストファイルです。\r\nテストファイルなんです。"), 0x600)
	putMessages(config, "pomera_sync", []string{"*"}, "", nil)

	log.Debug("=================")

	// overwrite
	ioutil.WriteFile("pomera_sync/テスト2.txt", []byte("テストファイル 2 です。\r\nテストファイルなんです。"), 0x600)
	putMessages(config, "pomera_sync", []string{"テスト2.txt"}, "", nil)

	log.Debug("=================")

	// substring name
	ioutil.WriteFile("pomera_sync/テ.txt", []byte("テです。\r\nテなんです。"), 0x600)
	putMessages(config, "pomera_sync", []string{"テ.txt"}, "", nil)

	log.Debug("=================")

	ic, _ = initIMAP(config)
	if list, err := listMessages(ic, "", ""); err != nil {
		t.Errorf("failed to list messages: %v", err)
	} else {
		sort.Slice(list, func(i, j int) bool {
			return list[i].Date < list[j].Date
		})
		if len(list) != 3 || list[0].Subject != "テスト" || list[1].Subject != "テスト2" || list[2].Subject != "テ" {
			t.Errorf("wrong messages (%v) are in box %q", len(list), config.IMAP.Box)
			for i, e := range list {
				t.Log(i, e)
			}
		}
	}

	log.Debug("=================")

	// teardown

	teardownTestBox(t, config, ic)
	ic.Logout()
	teardownLocal(t)
}
