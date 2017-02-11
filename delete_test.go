package main

import (
	"testing"
	"time"
)

func TestDelete(t *testing.T) {
	config, ic := getTestFixtures()
	setupTestBox(t, config, ic)

	ic.Append(config.IMAP.Box, nil, *makeMailMessage("test", "", time.Now()))
	ic.Append(config.IMAP.Box, nil, *makeMailMessage("test1", "", time.Now()))
	ic.Append(config.IMAP.Box, nil, *makeMailMessage("test2", "", time.Now()))
	ic.Append(config.IMAP.Box, nil, *makeMailMessage("hoge", "", time.Now()))
	msgsExistsExactly(t, ic, []string{"test", "test1", "test2", "hoge"})

	err := deleteMessage(ic, false, "aaaa", "")
	if err != nil {
		t.Errorf("failed to delete messages: %v", err)
	}
	msgsExistsExactly(t, ic, []string{"test", "test1", "test2", "hoge"})

	err = deleteMessage(ic, false, "test", "")
	if err != nil {
		t.Errorf("failed to delete messages: %v", err)
	}
	msgsExistsExactly(t, ic, []string{"test1", "test2", "hoge"})

	err = deleteMessage(ic, false, "", "1")
	if err != nil {
		t.Errorf("failed to delete messages: %v", err)
	}
	msgsExistsExactly(t, ic, []string{"test2", "hoge"})

	err = deleteMessage(ic, true, "aaaa", "9999")
	if err != nil {
		t.Errorf("failed to delete messages: %v", err)
	}
	msgsExistsExactly(t, ic, []string{})

	teardownTestBox(t, config, ic)
	ic.Logout()
	teardownLocal(t)
}
