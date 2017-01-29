package main

import (
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/shu/imapclient"
)

func getTestConfig() *config {
	c, err := loadConfig("./pomi.toml")
	if err != nil {
		panic(err)
	}
	c.IMAP.Box = "pomi_test"
	c.IMAP.User = os.Getenv("TEST_USER")
	c.AUTH.ClientID = os.Getenv("API_CLIENT_ID")
	c.AUTH.ClientSecret = os.Getenv("API_CLIENT_SECRET")
	c.AUTH.RefreshToken = os.Getenv("TEST_API_REFRESHTOKEN")

	return c
}

func initTestIMAP(config *config) *imapclient.Client {
	c, err := connIMAP(config)
	if err != nil {
		panic(err)
	}
	err = loginIMAP(c, config)
	if err != nil {
		panic(err)
	}
	return c
}

func getTestFixtures() (*config, *imapclient.Client) {
	config := getTestConfig()
	setAuthVariables(config)

	ic := initTestIMAP(config)

	return config, ic
}

func setupTestBox(t *testing.T, config *config, ic *imapclient.Client) {
	ic.Delete(config.IMAP.Box)
	if err := ic.Create(config.IMAP.Box); err != nil {
		t.Errorf("failed to create box %q: %v", config.IMAP.Box, err)
	}

	if err := ic.Select(config.IMAP.Box); err != nil {
		t.Errorf("failed to select box %q: %v", config.IMAP.Box, err)
	}
}

func teardownTestBox(t *testing.T, config *config, ic *imapclient.Client) {
	if err := ic.Delete(config.IMAP.Box); err != nil {
		t.Errorf("failed to delete box %q: %v", config.IMAP.Box, err)
	}
}

func setupLocal(t *testing.T) {
	if _, err := os.Stat("pomera_sync"); err == nil {
		t.Fatalf("pomera_sync exists. remove first: %v", err)
	}

	if err := os.MkdirAll("pomera_sync", os.ModeDir); err != nil {
		t.Fatalf("failed to create dir pomera_sync: %v", err)
	}
}

func teardownLocal(t *testing.T) {
	if err := os.RemoveAll("pomera_sync"); err != nil {
		t.Fatalf("failed to remove dir pomera_sync: %v", err)
	}
}

func makeMailMessage(subject, body string, date time.Time) *mail.Message {
	msg := new(mail.Message)
	msg.Header = make(mail.Header)
	msg.Header["Subject"] = []string{subject}
	msg.Header["Date"] = []string{date.Format(time.RFC1123Z)}
	msg.Body = strings.NewReader(body)
	msg, err := imapclient.EncodeMailMessage(msg)
	if err != nil {
		panic(err)
	}
	return msg
}

func msgsExistsExactly(t *testing.T, ic *imapclient.Client, subjects []string) {
	list, err := listMessages(ic, "", "")
	if err != nil {
		t.Errorf("failed to list msgs: %v", err)
	} else if len(list) != len(subjects) {
		t.Errorf("wrong messages %v, wanted %v", len(list), len(subjects))
		for _, e := range list {
			t.Log(e)
		}
	} else {
		//missing?
		for _, se := range subjects {
			found := false
			for _, le := range list {
				if le.Subject == se {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("subject %v is missing in the box", se)
			}
		}
		// superfluous?
		for _, le := range list {
			found := false
			for _, se := range subjects {
				if le.Subject == se {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("subject %v is superfluous in the box", le)
			}
		}
	}
}

func wipeoutLocalFiles(t *testing.T, path string) {
	matches, err := filepath.Glob(path + "/*")
	if err != nil {
		t.Fatal(err)
	}

	for _, fn := range matches {
		err = os.Remove(fn)
		if err != nil {
			panic(fmt.Errorf("removing %s: %v", fn, err))
		}
	}

	matches, err = filepath.Glob(path + "/*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("not empty after wipe: %#v", matches)
	}
}
