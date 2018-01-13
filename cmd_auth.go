package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pkg/browser"
)

type authCmd struct {
	Port    int `default:"7676"  help:"a temporal port for OAuth authentication. 0 is for copy&paste to CLI."`
	Timeout int `default:"60"  help:"set timeout (in seconds) on authentication transaction. < 0 is infinite."`
}

func (c authCmd) Run(g globalCmd) error {
	config, err := loadConfig(g.Config)
	if err != nil {
		return err
	}
	setAuthVariables(config)

	if c.Timeout > 0 {
		go func() {
			select {
			case <-time.After(time.Duration(c.Timeout) * time.Second):
				fmt.Fprintf(os.Stderr, "timed out\n")
				os.Exit(1)
			}
		}()
	}

	// setup parameters

	redirectURI := "urn:ietf:wg:oauth:2.0:oob"
	var codeChan chan string
	if c.Port != 0 {
		redirectURI = fmt.Sprintf("http://localhost:%d/", c.Port)
		codeChan = make(chan string)
		go launchRedirectionServer(c.Port, codeChan)
	}

	// request authorization (and authentication)

	authURL := oauth2AuhBaseURL
	form := url.Values{}
	form.Add("client_id", apiClientID)
	form.Add("redirect_uri", redirectURI)
	form.Add("scope", oauth2Scope)
	form.Add("response_type", "code")
	browser.OpenURL(fmt.Sprintf("%s?%s", authURL, form.Encode()))

	var authorizationCode string
	if c.Port == 0 {
		fmt.Scanln(&authorizationCode)
	} else {
		authorizationCode = <-codeChan
	}
	//log.Printf("authorization_code=%s\n", authorization_code)

	// request access token & request token

	tokenURL := oauth2TokenBaseURL
	form = url.Values{}
	form.Add("client_id", apiClientID)
	form.Add("client_secret", apiClientSecret)
	form.Add("code", authorizationCode)
	form.Add("redirect_uri", redirectURI)
	form.Add("grant_type", "authorization_code")
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	/*
		{
			log.Printf("%s&%s", tokenURL, form.Encode())
			b, _ := ioutil.ReadAll(resp.Body)
			log.Printf("resp.Body=%s", b)
		}
	*/
	dec := json.NewDecoder(resp.Body)
	t := oAuth2AuthedTokens{}
	err = dec.Decode(&t)
	if err == io.EOF {
		return fmt.Errorf("auth response from the server is empty")
	} else if err != nil {
		return err
	}
	config.AUTH.RefreshToken = t.RefreshToken

	// get email address

	infoURL := "https://www.googleapis.com/userinfo/email"
	form = url.Values{}
	form.Add("access_token", t.AccessToken)
	form.Add("alt", "json")
	//inforesp, err := http.PostForm(infoURL, form)
	inforesp, err := http.Get(fmt.Sprintf("%s?%s", infoURL, form.Encode()))
	if err != nil {
		// save with User unchanged.
		saveConfig(config, g.Config)
		return fmt.Errorf("failed to get email address: %v", err)
	}
	defer inforesp.Body.Close()
	/*
		 {
			log.Printf("%s&%s", infoURL, form.Encode())
			b, _ := ioutil.ReadAll(inforesp.Body)
			log.Printf("inforesp.Body=%s", b)
		}
	*/

	dec = json.NewDecoder(inforesp.Body)
	e := oAuth2Email{}
	err = dec.Decode(&e)
	if err == io.EOF {
		return fmt.Errorf("auth response from the server is empty")
	} else if err != nil {
		return err
	}
	config.IMAP.User = e.Data.Email

	saveConfig(config, g.Config)

	return nil

}
