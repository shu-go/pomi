package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"bitbucket.org/shu/imapclient"
	"bitbucket.org/shu/log"
	"github.com/BurntSushi/toml"
	"github.com/pkg/browser"
	"github.com/urfave/cli"
)

var _ = log.Print

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

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

type oAuth2AuthedTokens struct {
	RefreshToken string `json:"refresh_token"`
	AccessToken  string `json:"access_token"`
}
type oAuth2Email struct {
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
	defaultIMAPServer = "imap.gmail.com:993"
	defaultIMAPBox    = "Notes/pomera_sync"

	oauth2AuhBaseURL   = "https://accounts.google.com/o/oauth2/auth"
	oauth2TokenBaseURL = "https://accounts.google.com/o/oauth2/token"
	oauth2Scope        = "https://mail.google.com/ email"
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
				config, err := loadConfig(c.GlobalString("config"))
				if err != nil {
					return err
				}
				setAuthVariables(config)

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

				authURL := oauth2AuhBaseURL
				form := url.Values{}
				form.Add("client_id", apiClientID)
				form.Add("redirect_uri", redirectURI)
				form.Add("scope", oauth2Scope)
				form.Add("response_type", "code")
				browser.OpenURL(fmt.Sprintf("%s?%s", authURL, form.Encode()))

				var authorizationCode string
				if port == 0 {
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
					saveConfig(config, c.GlobalString("config"))
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

				saveConfig(config, c.GlobalString("config"))

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
				configPath := c.GlobalString("config")
				criteria := c.String("criteria")
				return runList(configPath, criteria, strings.Join(c.Args(), " "))
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
				config, err := loadConfig(c.GlobalString("config"))
				if err != nil {
					return err
				}
				setAuthVariables(config)

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
				config, err := loadConfig(c.GlobalString("config"))
				if err != nil {
					return err
				}
				setAuthVariables(config)

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
				config, err := loadConfig(c.GlobalString("config"))
				if err != nil {
					return err
				}
				setAuthVariables(config)

				ic, err := initIMAP(config)
				if err != nil {
					return err
				}
				ic.Logout()

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

					var mu sync.Mutex
					errs := []error{}
					var wg sync.WaitGroup

					for _, fn := range matches {
						wg.Add(1)

						go func(fn string) {
							//log.Debug(fn)

							f, err := os.Open(fn)
							if err != nil {
								mu.Lock()
								fmt.Fprintf(os.Stderr, "failed to open file %v: %v\n", fn, err)
								mu.Unlock()
								return
							}

							mu.Lock()
							fmt.Fprintf(os.Stderr, "putting %v\n", fn)
							mu.Unlock()

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

							//log.Debug("putMessage", fn)
							iic, _ := initIMAP(config)
							err = putMessage(config, iic, subject, f, tm)
							//log.Debug("end putMessage", fn)
							if err != nil {
								mu.Lock()
								errs = append(errs, err)
								mu.Unlock()
								return
							}

							iic.Logout()
							f.Close()

							wg.Done()
						}(fn)
					}
					wg.Wait()
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
				config, err := loadConfig(c.GlobalString("config"))
				if err != nil {
					return err
				}
				setAuthVariables(config)

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

func setAuthVariables(config *config) {
	// use own client id?
	if config.AUTH.ClientID != "" {
		apiClientID = config.AUTH.ClientID
		apiClientSecret = config.AUTH.ClientSecret
	}
}

func loadConfig(path string) (*config, error) {
	config := new(config)
	_, err := toml.DecodeFile(path, config)
	if err != nil {
		//return nil, fmt.Errorf("failed to open %v: %v\n", c.GlobalString("config"), err)
		fmt.Fprintf(os.Stderr, "missing %v. -> creating with minimal contents...", path)
		config.IMAP.Server = defaultIMAPServer
		config.IMAP.Box = defaultIMAPBox
		if err = saveConfig(config, path); err != nil {
			return nil, fmt.Errorf("failed to access to config: %v", err)
		}
		fmt.Fprintf(os.Stderr, "created.\n")
	}

	return config, nil
}

func saveConfig(config *config, path string) error {
	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(config); err != nil {
		return err
	}
	return ioutil.WriteFile(path, buf.Bytes(), 0700)
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
		accessToken, err := refreshAccessToken(config)
		if err == nil {
			data := fmt.Sprintf("user=%s\001auth=Bearer %s\001\001", config.IMAP.User, accessToken)
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

		mm, err := imapclient.DecodeMailMessage(m)
		if err != nil {
			return fmt.Errorf("message(%q) decode error: %v", subject, err)
		}
		m = pickupTextPartMessage(mm)

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

		if bytes.Index(buff.Bytes(), utf8BOM) == -1 {
			bombuff := bytes.NewBuffer(utf8BOM)
			bombuff.Write(buff.Bytes())
			buff = bombuff
		}

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

func runList(configPath, criteria, keyword string) error {
	config, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	ic, err := initIMAP(config)
	if err != nil {
		return err
	}

	list, err := listMessages(ic, criteria, keyword)
	if err != nil {
		return fmt.Errorf("listing error: %v", err)
	}

	if len(list) == 0 {
		fmt.Fprintf(os.Stderr, "no messages\n")
	} else {
		for _, e := range list {
			fmt.Printf("%d %v (%v)\n", e.Seq, e.Subject, e.Date)
		}
	}

	return nil
}

type listElement struct {
	Seq     uint32
	Subject string
	Date    string
}

func listMessages(c *imapclient.Client, criteria, keyword string) ([]listElement, error) {
	var ids []uint32
	var err error

	if strings.Trim(keyword, " ") == "" {
		ids, err = c.Search("ALL")
	} else {
		ids, err = c.Search(criteria, keyword)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %v\n", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	//log.Printf("ids=%#v\n", ids)
	seqset := joinUint32(ids, ",")
	//log.Printf("seqset=%v\n", seqset)
	msgs, err := c.Fetch(seqset, true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %v\n", err)
	}

	// convert random []seq in map[seq]msg to sorted []seqs
	seqs := make([]uint32, 0, len(msgs))
	for seq, _ := range msgs {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool {
		return seqs[i] < seqs[j]
	})

	list := make([]listElement, 0, len(msgs))
	for _, seq := range seqs {
		msg := msgs[seq]
		textMsg, err := decodeMessageAsTextMessage(msg, true)
		if err != nil {
			return nil, err
		}

		list = append(list, listElement{
			Seq:     seq,
			Subject: textMsg.Header.Get("Subject"),
			Date:    textMsg.Header.Get("Date"),
		})
	}

	return list, nil
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
		if _, _, err := writeMessageToFile(m, header, output, dir, ext); err != nil {
			return err
		}
	}

	return nil
}

func decodeMessageAsTextMessage(msg *mail.Message, header bool) (*mail.Message, error) {
	decodedMsg, err := imapclient.DecodeMailMessage(msg, header)
	if err != nil {
		// try to refer headers
		headerMsg, herr := imapclient.DecodeMailMessage(msg, true)
		if herr != nil {
			return nil, herr
		}
		decodedHeaderMsg := pickupTextPartMessage(headerMsg)
		return nil, fmt.Errorf("on subject[%v]: %v", decodedHeaderMsg.Header.Get("Subject"), err)
	}
	if len(decodedMsg) == 0 {
		return nil, fmt.Errorf("no decodable messages found on subject[%v]", msg.Header.Get("Subject"))
	}

	textMsg := pickupTextPartMessage(decodedMsg)
	if textMsg == nil {
		return nil, fmt.Errorf("no text part found on subject[%v]", decodedMsg[0].Header.Get("Subject"))
	}

	return textMsg, nil
}

func writeMessageToFile(msg *mail.Message, header bool, output, dir, ext string) (filename string, ts time.Time, err error) {
	textMsg, err := decodeMessageAsTextMessage(msg, false)
	if err != nil {
		return "", time.Time{}, err
	}

	file, err := getOutputWriteCloser(output, dir, textMsg.Header.Get("Subject"), ext)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("on subject[%v]: %v", textMsg.Header.Get("Subject"), err)
	}

	if header {
		for k, v := range textMsg.Header {
			file.Write([]byte(k))
			file.Write([]byte{':', ' '})
			file.Write([]byte(v[0]))
			file.Write([]byte{'\r', '\n'})
		}
		file.Write([]byte{'\r', '\n'})
	}

	body, err := ioutil.ReadAll(textMsg.Body)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("on subject[%v]: body reading error: %v", textMsg.Header.Get("Subject"), err)
	}
	file.Write(body)

	if file != os.Stdout {
		name := file.Name()
		file.Close()

		// change timestamps
		tm, err := textMsg.Header.Date()
		if err == nil {
			err = os.Chtimes(name, tm, tm)
			if err != nil {
				fmt.Fprintf(os.Stderr, "on subject[%v]: failed to change timestamp of %q: %v\n", textMsg.Header.Get("Subject"), name, err)
			}
		}
		return name, tm, nil
	} else {
		return "", time.Time{}, nil
	}

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
	tokenURL := oauth2TokenBaseURL
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
	t := oAuth2AuthedTokens{}
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

func joinUint32(vv []uint32, sep string) string {
	ss := make([]string, len(vv))
	for i, v := range vv {
		ss[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(ss, sep)
}

func pickupTextPartMessage(msgs []*mail.Message) *mail.Message {
	if msgs == nil || len(msgs) == 0 {
		return nil
	}

	if len(msgs) == 1 {
		return msgs[0]
	}

	for _, m := range msgs {
		if strings.Contains(m.Header.Get("Content-Type"), "text") {
			return m
		}
	}
	return msgs[len(msgs)-1]
}
