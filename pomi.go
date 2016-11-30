package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bitbucket.org/shu/imapclient"
	"github.com/BurntSushi/toml"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
)

var _ = log.Print

type config struct {
	IMAP struct {
		User   string
		Pass   string
		Server string
		Box    string
	}
	AUTH struct {
		ClientID     string `toml:"ClientID,omitempty"`
		ClientSecret string `toml:"ClientSecret,omitempty"`

		RefreshToken string `toml:"RefreshToken,omitempty"`
	}
}

type OAuth2AuthedTokens struct {
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
}
type OAuth2Email struct {
	Data struct {
		Email      string `json:"email"`
		IsVerified bool   `json:"isVerified"`
	} `json:"data"`
}

var (
	apiClientID     string
	apiClientSecret string
)

const (
	DEFAULT_IMAP_SERVER = "imap.gmail.com:993"
	DEFAULT_IMAP_BOX    = "Notes/pomera_sync"

	OAUTH2_AUTH_BASE_URL  = "https://accounts.google.com/o/oauth2/auth"
	OAUTH2_TOKEN_BASE_URL = "https://accounts.google.com/o/oauth2/token"
	OAUTH2_SCOPE          = "https://mail.google.com/ email"
)

func main() {
	app := cli.NewApp()
	app.Commands = []cli.Command{
		{
			Name:  "auth",
			Usage: "authenticate with gmail",
			Flags: []cli.Flag{
				cli.IntFlag{Name: "port", Value: 7878, Usage: "a temporal `PORT` for OAuth authentication. 0 is for copy&paste to CLI."},
				cli.IntFlag{Name: "timeout", Value: 60, Usage: "set `TIMEOUT` (in seconds) on authentication transaction. < 0 is infinite."},
			},
			Action: func(c *cli.Context) error {
				config, err := loadConfig(c)
				if err != nil {
					return err
				}

				timeout := time.Duration(c.Int("timeout"))
				if timeout > 0 {
					go func() {
						select {
						case <-time.After(timeout * time.Second):
							fmt.Fprintf(os.Stderr, "timed out\n")
							os.Exit(1)
						}
					}()
				}

				// setup parameters

				redirectURI := "urn:ietf:wg:oauth:2.0:oob"
				port := c.Int("port")
				var codeChan chan string
				if port != 0 {
					redirectURI = fmt.Sprintf("http://localhost:%d/", port)
					codeChan = make(chan string)
					go launchRedirectionServer(port, codeChan)
				}

				// request authorization (and authentication)

				authURL := OAUTH2_AUTH_BASE_URL
				form := url.Values{}
				form.Add("client_id", apiClientID)
				form.Add("redirect_uri", redirectURI)
				form.Add("scope", OAUTH2_SCOPE)
				form.Add("response_type", "code")
				browser.OpenURL(fmt.Sprintf("%s?%s", authURL, form.Encode()))

				var authorization_code string
				if port == 0 {
					fmt.Scanln(&authorization_code)
				} else {
					authorization_code = <-codeChan
				}
				//log.Printf("authorization_code=%s\n", authorization_code)

				// request access token & request token

				tokenURL := OAUTH2_TOKEN_BASE_URL
				form = url.Values{}
				form.Add("client_id", apiClientID)
				form.Add("client_secret", apiClientSecret)
				form.Add("code", authorization_code)
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
				t := OAuth2AuthedTokens{}
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
					saveConfig(config, c)
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
				e := OAuth2Email{}
				err = dec.Decode(&e)
				if err == io.EOF {
					return fmt.Errorf("auth response from the server is empty")
				} else if err != nil {
					return err
				}
				config.IMAP.User = e.Data.Email

				saveConfig(config, c)

				return nil
			},
		},
		{
			Name:    "list",
			Aliases: []string{"l", "ls"},
			Usage:   "list messages",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "criteria, c", Value: "SUBJECT", Usage: "criteria"},
			},
			Action: func(c *cli.Context) error {
				config, err := loadConfig(c)
				if err != nil {
					return err
				}

				ic, err := initIMAP(config)
				if err != nil {
					return err
				}

				return listMessages(ic, c.String("criteria"), strings.Join(c.Args(), " "))
			},
		},
		{
			Name:    "show",
			Aliases: []string{"g"},
			Usage:   "show messages",
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "all", Usage: "show all messages"},
				cli.StringFlag{Name: "seq", Usage: "show by seq. (comma separated or s1:s2"},
				cli.StringFlag{Name: "subject, subj, s", Usage: "show by subject"},
				cli.BoolFlag{Name: "header, H", Usage: "output mail headers"},
			},
			Action: func(c *cli.Context) error {
				config, err := loadConfig(c)
				if err != nil {
					return err
				}

				ic, err := initIMAP(config)
				if err != nil {
					return err
				}

				var seq, subject string
				if c.Bool("all") {
					seq = "1:9999999"
				} else {
					if subject = c.String("subject"); subject != "" {
						seq = resolveSeqBySubject(ic, subject)
					} else {
						seq = c.String("seq")
					}
				}

				if seq == "" {
					fmt.Println("no match")
					return nil
				}

				return getMessagesBySeq(ic, seq, c.Bool("header"), c.GlobalString("dir"), "stdout", "")
			},
		},
		{
			Name:    "get",
			Aliases: []string{"g"},
			Usage:   "get messages",
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "all", Usage: "fetch all messages"},
				cli.StringFlag{Name: "seq", Usage: "fetch by seq. (comma separated or s1:s2"},
				cli.StringFlag{Name: "subject, subj, s", Usage: "fetch by subject"},
				cli.StringFlag{Name: "ext, e", Value: "txt", Usage: "file extention"},
				cli.BoolFlag{Name: "header, H", Usage: "output mail headers"},
			},
			Action: func(c *cli.Context) error {
				config, err := loadConfig(c)
				if err != nil {
					return err
				}

				ic, err := initIMAP(config)
				if err != nil {
					return err
				}

				var seq, subject string
				if c.Bool("all") {
					seq = "1:9999999"
				} else {
					if subject = c.String("subject"); subject != "" {
						seq = resolveSeqBySubject(ic, subject)
					} else {
						seq = c.String("seq")
					}
				}

				if seq == "" {
					fmt.Println("no match")
					return nil
				}

				return getMessagesBySeq(ic, seq, c.Bool("header"), c.GlobalString("dir"), "subject", c.String("ext"))
			},
		},
		{
			Name:    "put",
			Aliases: []string{"p"},
			Usage:   "put messages",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "name", Usage: "if from stdin, specify the name of it"},
			},
			Action: func(c *cli.Context) error {
				config, err := loadConfig(c)
				if err != nil {
					return err
				}

				ic, err := initIMAP(config)
				if err != nil {
					return err
				}

				filenames := c.Args()
				fmt.Fprintf(os.Stderr, "searching files in %v\n", c.GlobalString("dir"))
				for _, fp := range filenames {
					matches, err := filepath.Glob(filepath.Join(c.GlobalString("dir"), fp))
					if err != nil {
						continue
					}
					if len(matches) == 0 {
						fmt.Fprintf(os.Stderr, "no matches\n")
					}
					for _, fn := range matches {
						f, err := os.Open(fn)
						if err != nil {
							fmt.Fprintf(os.Stderr, "failed to open file %v: %v\n", fn, err)
							continue
						}
						fmt.Fprintf(os.Stderr, "putting %v\n", fn)

						_, subject := filepath.Split(filepath.Base(fn))
						extpos := strings.LastIndex(subject, ".")
						if extpos != -1 {
							subject = subject[:extpos]
						}

						var tm time.Time
						info, err := f.Stat()
						if err == nil {
							tm = info.ModTime()
						}

						err = putMessage(config, ic, subject, f, tm)
						if err != nil {
							return err
						}

						f.Close()
					}
				}
				return nil
			},
		},
		{
			Name:    "delete",
			Aliases: []string{"del", "d"},
			Usage:   "delete messages",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "seq", Usage: "fetch by seq. (comma separated or s1:s2"},
				cli.StringFlag{Name: "subject, subj, s", Usage: "fetch by subject"},
			},
			Action: func(c *cli.Context) error {
				config, err := loadConfig(c)
				if err != nil {
					return err
				}

				ic, err := initIMAP(config)
				if err != nil {
					return err
				}

				var seq, subject string
				if c.Bool("all") {
					seq = "1:9999999"
				} else {
					if subject = c.String("subject"); subject != "" {
						seq = resolveSeqBySubject(ic, subject)
					} else {
						seq = c.String("seq")
					}
				}

				if seq == "" {
					fmt.Println("no match")
					return nil
				}

				err = ic.Store(seq, "+FLAGS", []string{imapclient.FlagDeleted})
				if err != nil {
					return err
				}

				err = ic.Expunge()
				if err != nil {
					return err
				}

				return nil
			},
		},
	}
	app.Name = "pomi"
	app.Usage = "Pomera Sync IMAP tool"
	app.Version = "0.1.0"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "config, conf", Value: "./pomi.toml", Usage: "load the configuration from `CONFIG`"},
		cli.StringFlag{Name: "dir, d", Value: "./pomera_sync", Usage: "set local directory to `DIR`"},
	}
	app.Run(os.Args)
	return
}

func loadConfig(c *cli.Context) (*config, error) {
	config := new(config)
	_, err := toml.DecodeFile(c.GlobalString("config"), config)
	if err != nil {
		//return nil, fmt.Errorf("failed to open %v: %v\n", c.GlobalString("config"), err)
		fmt.Fprintf(os.Stderr, "missing %v. -> creating with minimal contents...", c.GlobalString("config"))
		config.IMAP.Server = DEFAULT_IMAP_SERVER
		config.IMAP.Box = DEFAULT_IMAP_BOX
		if err = saveConfig(config, c); err != nil {
			return nil, fmt.Errorf("failed to access to config: %v", err)
		}
		fmt.Fprintf(os.Stderr, "created.\n")
	}

	// use own client id?
	if config.AUTH.ClientID != "" {
		apiClientID = config.AUTH.ClientID
		apiClientSecret = config.AUTH.ClientSecret
	}

	return config, nil
}

func saveConfig(config *config, c *cli.Context) error {
	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(config); err != nil {
		return err
	}
	return ioutil.WriteFile(c.GlobalString("config"), buf.Bytes(), 0700)
}

func initIMAP(config *config) (*imapclient.Client, error) {
	c, err := connIMAP(config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %v: %v\n", config.IMAP.Server, err)
	}

	err = loginIMAP(c, config)
	if err != nil {
		return nil, fmt.Errorf("failed to log in to %v: %v\n", config.IMAP.Server, err)
	}

	err = c.Select(config.IMAP.Box)
	if err != nil {
		return nil, fmt.Errorf("can't select box %v: %v\n", config.IMAP.Box, err)
	}

	return c, nil
}

func loginIMAP(c *imapclient.Client, config *config) error {
	loggedin := false

	if config.AUTH.RefreshToken != "" {
		access_token, err := refreshAccessToken(config)
		if err == nil {
			data := fmt.Sprintf("user=%s\001auth=Bearer %s\001\001", config.IMAP.User, access_token)
			am := base64.StdEncoding.EncodeToString([]byte(data))

			err = c.Authenticate(fmt.Sprintf("XOAUTH2 %s", am))

			loggedin = true
		}

		if err != nil {
			loggedin = false
			fmt.Fprintf(os.Stderr, "failed to refresh access token: %v", err)
			fmt.Fprintf(os.Stderr, "try to login with [IMAP] User and Pass")
		}
	}

	if !loggedin {
		fmt.Fprintf(os.Stderr, "login with user&pass\n")
		if config.IMAP.User == "" {
			config.IMAP.User = os.Getenv("IMAP_USER")
		}
		if config.IMAP.Pass == "" {
			config.IMAP.Pass = os.Getenv("IMAP_PASS")
		}

		err := c.Login(config.IMAP.User, config.IMAP.Pass)
		if err != nil {
			return fmt.Errorf("can't login as %v: %v\n", config.IMAP.User, err)
		}
	}

	return nil
}

func connIMAP(config *config) (*imapclient.Client, error) {
	c, err := imapclient.NewClient("tcp", config.IMAP.Server)
	if err != nil {
		return nil, fmt.Errorf("can't connect to %v: %v\n", config.IMAP.Server, err)
	}

	return c, nil
}

func putMessage(config *config, c *imapclient.Client, subject string, file *os.File, tm time.Time) error {
	var m *mail.Message

	ids, err := c.Search("SUBJECT", subject)
	if err == nil && len(ids) > 0 {
		if len(ids) > 1 {
			fmt.Fprintf(os.Stderr, "more than one messages are found for %q. skipped.\n", subject)
			return nil
		}

		msgs, err := c.Fetch(fmt.Sprintf("%v", ids[0]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to fetch message %q. skipped.", subject)
			return nil
		}

		m = msgs[ids[0]]
		if err != nil {
			return fmt.Errorf("message(%q) read: %v", subject, err)
		}

		m, err = imapclient.DecodeMailMessage(m)
		if err != nil {
			return fmt.Errorf("message(%q) decode error: %v", subject, err)
		}
	} else {
		m = new(mail.Message)
		m.Header = make(mail.Header)
		m.Header["Subject"] = []string{subject}
		m.Header["Content-Type"] = []string{"text/plain; charset=\"utf-8-sig\""}

		// for pomera
		from := config.IMAP.User
		if strings.Index(from, "@") == -1 {
			from += "@gmail.com"
		}
		m.Header["From"] = []string{from}
	}

	if m == nil {
		return fmt.Errorf("miss initializing m")
	}

	m.Header["Date"] = []string{tm.Format(time.RFC1123Z)}

	//add BOM for pomera
	{
		buff := new(bytes.Buffer)
		if all, err := ioutil.ReadAll(file); err == nil {
			buff.Write(all)
		}

		rawBytes := buff.Bytes()
		encName := Chardet(rawBytes)
		if encName == UTF8N {
			bombuff := bytes.NewBuffer(UTF8BOM)
			bombuff.Write(rawBytes)
			buff = bombuff
		}
		log.Printf("charset=%v\n", encName)
		m.Header["Content-Type"] = []string{fmt.Sprintf("text/plain; charset=\"%s\"", encName)}

		m.Body = buff
	}

	m, err = imapclient.EncodeMailMessage(m)
	if err != nil {
		return fmt.Errorf("message encode error of %q: %v", subject, err)
	}

	if len(ids) > 0 {
		err = c.Store(fmt.Sprintf("%v", ids[0]), "+FLAGS", []string{imapclient.FlagDeleted})
		if err != nil {
			return fmt.Errorf("flag set error of %q: %v", subject, err)
		}
		err = c.Expunge()
		if err != nil {
			return fmt.Errorf("delete error of %q: %v", subject, err)
		}
	}

	err = c.Append(config.IMAP.Box, nil, *m)
	if err != nil {
		return fmt.Errorf("message append error of %q: %v", subject, err)
	}

	return nil
}

func listMessages(c *imapclient.Client, criteria, keyword string) error {
	var ids []uint32
	var err error

	if strings.Trim(keyword, " ") == "" {
		ids, err = c.Search("ALL")
	} else {
		ids, err = c.Search(criteria, keyword)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to list messages: %v\n", err)
		os.Exit(1)
	}
	if len(ids) == 0 {
		fmt.Fprintf(os.Stderr, "no messages\n")
		return err
	}

	//log.Printf("ids=%#v\n", ids)
	seqset := joinUint32(ids, ",")
	//log.Printf("seqset=%v\n", seqset)
	msgs, err := c.Fetch(seqset)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch messages: %v\n", err)
		os.Exit(1)
	}

	seqs := make([]uint32, 0, len(msgs))
	for k := range msgs {
		seqs = append(seqs, k)
	}
	sort.Sort(uint32slice(seqs))

	for _, seq := range seqs {
		m := msgs[seq]
		dm, err := imapclient.DecodeMailMessage(m, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to decode %dth message: %v\n", seq, err)
			fmt.Fprintf(os.Stderr, "headers:%#v", m.Header)
			os.Exit(1)
		}
		fmt.Printf("%d %v (%v)\n", seq, dm.Header.Get("Subject"), dm.Header.Get("Date"))
	}

	return nil
}

func getMessagesBySeq(c *imapclient.Client, seq string, header bool, dir, output, ext string) error {
	mm, err := c.Fetch(seq)
	if err != nil {
		return err
	}

	// arrange workdir
	if dir != "." {
		if err := os.MkdirAll(dir, os.ModeDir); err != nil {
			return err
		}
	}

	for _, m := range mm {
		orgM := m
		m, err = imapclient.DecodeMailMessage(m)
		if err != nil {
			hm, herr := imapclient.DecodeMailMessage(orgM, true)
			if herr != nil {
				return herr
			}
			return fmt.Errorf("on subject[%v]: %v", hm.Header.Get("Subject"), err)
		}

		file, err := getOutputWriteCloser(output, dir, m.Header.Get("Subject"), ext)
		if err != nil {
			return fmt.Errorf("on subject[%v]: %v", m.Header.Get("Subject"), err)
		}

		if header {
			for k, v := range m.Header {
				file.Write([]byte(k))
				file.Write([]byte{':', ' '})
				file.Write([]byte(v[0]))
				file.Write([]byte{'\r', '\n'})
			}
			file.Write([]byte{'\r', '\n'})
		}

		body, err := ioutil.ReadAll(m.Body)
		if err != nil {
			return fmt.Errorf("on subject[%v]: body reading error: %v", m.Header.Get("Subject"), err)
		}
		file.Write(body)

		if file != os.Stdout {
			name := file.Name()
			file.Close()

			// change timestamps
			tm, err := m.Header.Date()
			if err == nil {
				err = os.Chtimes(name, tm, tm)
				if err != nil {
					fmt.Fprintf(os.Stderr, "on subject[%v]: failed to change timestamp of %q: %v\n", m.Header.Get("Subject"), name, err)
				}
			}
		}
	}

	return nil
}

func getOutputWriteCloser(output, dir, subject, ext string) (*os.File, error) {
	switch output {
	case "stdout":
		return os.Stdout, nil
	case "subject":
		file, err := os.Create(filepath.Join(dir, subject+"."+ext))
		if err != nil {
			return nil, err
		}
		return file, nil
	}

	return nil, nil
}

func resolveSeqBySubject(c *imapclient.Client, subject string) string {
	seq, err := c.Search("SUBJECT", subject)
	if err != nil || len(seq) == 0 {
		return ""
	}

	seqstrs := make([]string, len(seq))
	for i, s := range seq {
		seqstrs[i] = fmt.Sprintf("%v", s)
	}
	return strings.Join(seqstrs, ",")
}

func refreshAccessToken(config *config) (string, error) {
	tokenURL := OAUTH2_TOKEN_BASE_URL
	form := url.Values{}
	form.Add("client_id", apiClientID)
	form.Add("client_secret", apiClientSecret)
	form.Add("refresh_token", config.AUTH.RefreshToken)
	form.Add("grant_type", "refresh_token")
	resp, err := http.PostForm(tokenURL, form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	t := OAuth2AuthedTokens{}
	err = dec.Decode(&t)
	if err == io.EOF {
		return "", fmt.Errorf("auth response from the server is empty")
	} else if err != nil {
		return "", err
	}

	return t.AccessToken, nil
}

func launchRedirectionServer(port int, codeChan chan string) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.FormValue("code")
		codeChan <- code

		var color string
		var icon string
		var result string
		if code != "" {
			//success
			color = "green"
			icon = "&#10003;"
			result = "Successfully authenticated!!"
		} else {
			//fail
			color = "red"
			icon = "&#10008;"
			result = "FAILED!"
		}
		disp := fmt.Sprintf(`<div><span style="font-size:xx-large; color:%s; border:solid thin %s;">%s</span> %s</div>`, color, color, icon, result)

		fmt.Fprintf(w, `
<html>
	<head><title>%s pomi</title></head>
	<body onload="open(location, '_self').close();"> <!-- Chrome won't let me close! -->
		%s
		<hr />
		<p>This is a temporal page.<br />Please close it.</p>
	</body>
</html>
`, icon, disp)
	})
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}

type uint32slice []uint32

func (us uint32slice) Len() int {
	return len(us)
}
func (us uint32slice) Less(i, j int) bool {
	return us[i] < us[j]
}
func (us uint32slice) Swap(i, j int) {
	us[i], us[j] = us[j], us[i]
}

func joinUint32(vv []uint32, sep string) string {
	ss := make([]string, len(vv))
	for i, v := range vv {
		ss[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(ss, sep)
}
