package main

import (
	"bitbucket.org/shu/imapclient"
	"net/mail"
	"os"
	"strings"
	"time"
)

func getTestConfig() *config {
	c, err := loadConfig("./pomi.toml")
	if err != nil {
		panic(err)
	}
	c.IMAP.Box = "pomi_test"
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
