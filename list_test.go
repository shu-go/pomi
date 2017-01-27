package main

import (
	"testing"
	"time"
)

func TestList(t *testing.T) {
	config, ic := getTestFixtures()

	if err := ic.Create(config.IMAP.Box); err != nil {
		t.Fatalf("failed to create box %q: %v", config.IMAP.Box, err)
	}

	if err := ic.Select(config.IMAP.Box); err != nil {
		t.Fatalf("failed to select box %q: %v", config.IMAP.Box, err)
	}

	if list, err := listMessages(ic, "", ""); err != nil {
		t.Errorf("failed to list messages: %v", err)
	} else if len(list) != 0 {
		t.Errorf("box %q is not empty", config.IMAP.Box)
		for i, e := range list {
			t.Log(i, e)
		}
	}

	now1 := time.Now()
	msg := makeMailMessage("test", "testbody", now1)
	if err := ic.Append(config.IMAP.Box, nil, *msg); err != nil {
		t.Errorf("failed to append message(%#v): %v", msg, err)
	} else {
		if list, err := listMessages(ic, "", ""); err != nil {
			t.Errorf("failed to list messages: %v", err)
		} else if len(list) != 1 || (list[0].Subject != "test" && list[0].Date != now1.Format(time.RFC1123Z)) {
			t.Errorf("wrong messages are in box %q", config.IMAP.Box)
			for i, e := range list {
				t.Log(i, e)
			}
		}

		now2 := time.Now()
		msg = makeMailMessage("てすと２", "", now2)
		if err := ic.Append(config.IMAP.Box, nil, *msg); err != nil {
			t.Errorf("failed to append message(%#v): %v", msg, err)
		} else {
			if list, err := listMessages(ic, "", ""); err != nil {
				t.Errorf("failed to list messages: %v", err)
			} else if len(list) != 2 ||
				(list[0].Subject != "test" && list[0].Date != now1.Format(time.RFC1123Z)) ||
				(list[1].Subject != "てすと２" && list[1].Date != now2.Format(time.RFC1123Z)) {
				t.Errorf("wrong messages are in box %q", config.IMAP.Box)
				for i, e := range list {
					t.Log(i, e)
				}
			}
		}
	}

	if err := ic.Delete(config.IMAP.Box); err != nil {
		t.Fatalf("failed to delete box %q: %v", config.IMAP.Box, err)
	}
}
