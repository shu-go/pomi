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

	"bitbucket.org/shu_go/gli"
	"bitbucket.org/shu_go/imapclient"
	"github.com/BurntSushi/toml"
)

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

type MsgWriter func(syncDirPath, subject, ext string, tm time.Time, r io.Reader) error

var filesWriter MsgWriter = func(syncDirPath, subject, ext string, tm time.Time, r io.Reader) error {
	name := filepath.Join(syncDirPath, subject+"."+ext)
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(name, data, 0x600)
	if err != nil {
		return fmt.Errorf("on subject[%v]: failed to write to %q: %v\n", subject, name, err)
	}

	err = os.Chtimes(name, tm, tm)
	if err != nil {
		return fmt.Errorf("on subject[%v]: failed to change timestamp of %q: %v\n", subject, name, err)
	}

	return nil
}
var stdoutWriter MsgWriter = func(syncDirPath, subject, ext string, tm time.Time, r io.Reader) error {
	_, err := io.Copy(os.Stdout, r)
	return err
}

const (
	defaultIMAPServer = "imap.gmail.com:993"
	defaultIMAPBox    = "Notes/pomera_sync"

	oauth2AuhBaseURL   = "https://accounts.google.com/o/oauth2/auth"
	oauth2TokenBaseURL = "https://accounts.google.com/o/oauth2/token"
	oauth2Scope        = "https://mail.google.com/ email"
)

type globalCmd struct {
	Auth   authCmd   `help:"authenticate with gmail"`
	List   listCmd   `cli:"list, ls, l"  help:"list messages"`
	Show   showCmd   `cli:"show, s"  help:"show messages"`
	Get    getCmd    `cli:"get, g"  help:"get messages"`
	Put    putCmd    `cli:"put, p"  help:"put messages"`
	Delete deleteCmd `cli:"delete, del, d"  help:"delete messages"`

	Config string `cli:"config=CONFIG_FILE, conf"  default:"./pomi.toml"  help:"path to a configuration file"`
	Dir    string `cli:"dir=DIR, d"  default:"./pomera_sync"  help:"path to a local directory"`
}

func main() {
	app := gli.NewWith(&globalCmd{})
	app.Name = "pomi"
	app.Desc = "Pomera Sync IMAP tool"
	app.Version = "0.1.3"
	app.Usage = `1. pomi auth
2. pomi get --all`

	err := app.Run(os.Args)
	if err != nil {
		os.Exit(1)
	}

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

func putMessage(c *imapclient.Client, box, from, subject, ext string, file *os.File, tm time.Time) error {
	var m *mail.Message

	seqs, err := c.Search("SUBJECT", subject)
	msgmap, _ := c.Fetch(joinUint32(seqs, ","))
	if err == nil && len(msgmap) > 0 {
		for _, ref := range msgmap {
			dref, err := imapclient.DecodeMailMessage(ref)
			if err != nil {
				continue
			}
			tref := pickupTextPartMessage(dref)
			if tref == nil {
				continue
			}

			if tref.Header.Get("Subject") == subject {
				m = tref
				break
			}
		}
	}

	if m == nil {
		m = new(mail.Message)
		m.Header = make(mail.Header)
		m.Header["Subject"] = []string{subject}
		m.Header["Content-Type"] = []string{"text/plain; charset=\"utf-8-sig\""}

		// for pomera
		if strings.Index(from, "@") == -1 {
			from += "@gmail.com"
		}
		m.Header["From"] = []string{from}
	}

	if m == nil {
		return fmt.Errorf("miss initializing m")
	}

	m.Header["Date"] = []string{tm.Format(time.RFC1123Z)}
	if len(ext) > 0 {
		m.Header["X-Pomi-Ext"] = []string{ext}
	}

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

	if len(seqs) > 0 {
		err = c.Store(fmt.Sprintf("%v", seqs[0]), "+FLAGS", []string{imapclient.FlagDeleted})
		if err != nil {
			return fmt.Errorf("flag set error of %q: %v", subject, err)
		}
		err = c.Expunge()
		if err != nil {
			return fmt.Errorf("delete error of %q: %v", subject, err)
		}
	}

	err = c.Append(box, nil, *m)
	if err != nil {
		return fmt.Errorf("message append error of %q: %v", subject, err)
	}

	return nil
}

func deleteMessage(ic *imapclient.Client, all bool, subject, seq string) error {
	if all {
		seq = "1:9999999"
	} else if subject != "" {
		seq = resolveSeqBySubject(ic, subject)
	}

	if seq == "" {
		fmt.Fprintf(os.Stderr, "no matches\n")
		return nil
	}

	err := ic.Store(seq, "+FLAGS", []string{imapclient.FlagDeleted})
	if err != nil {
		return err
	}

	err = ic.Expunge()
	if err != nil {
		return err
	}

	return nil
}

func putMessages(config *config, syncDirPath string, patterns []string, stdinName string, disp func(string, error)) (count int, err error) {
	for _, pat := range patterns {
		matches, err := filepath.Glob(filepath.Join(syncDirPath, pat))
		if err != nil {
			continue
		}
		if len(matches) == 0 {
			return 0, nil
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
					if disp != nil {
						mu.Lock()
						disp(fn, err)
						mu.Unlock()
					}
					return
				}

				if disp != nil {
					mu.Lock()
					disp(fn, nil)
					mu.Unlock()
				}

				_, subject := filepath.Split(filepath.Base(fn))
				ext := ""
				extpos := strings.LastIndex(subject, ".")
				if extpos != -1 {
					ext = subject[extpos+1:]
					subject = subject[:extpos]
				}

				var tm time.Time
				info, err := f.Stat()
				if err == nil {
					tm = info.ModTime()
				}

				//log.Debug("putMessage", fn)
				iic, _ := initIMAP(config)
				err = putMessage(iic, config.IMAP.Box, config.IMAP.User, subject, ext, f, tm)
				//log.Debug("end putMessage", fn)
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					return
				}
				mu.Lock()
				count++
				mu.Unlock()

				iic.Logout()
				f.Close()

				wg.Done()
			}(fn)
		}
		wg.Wait()
	}

	return count, nil
}

type listElement struct {
	Seq     uint32
	Subject string
	Date    string
}

func listMessages(c *imapclient.Client, criteria, keyword string) ([]listElement, error) {
	var seqs []uint32
	var err error

	if strings.Trim(keyword, " ") == "" {
		seqs, err = c.Search("ALL")
	} else {
		seqs, err = c.Search(criteria, keyword)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list messages: %v\n", err)
	}
	if len(seqs) == 0 {
		return nil, nil
	}

	//log.Printf("seqs=%#v\n", seqs)
	seqset := joinUint32(seqs, ",")
	//log.Printf("seqset=%v\n", seqset)
	msgs, err := c.Fetch(seqset, true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %v\n", err)
	}

	// convert random []seq in map[seq]msg to sorted []seqs
	seqs = make([]uint32, 0, len(msgs))
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

func getMessages(ic *imapclient.Client, header, all bool, subject, seq string, syncDirPath, ext string, msgWriter MsgWriter) error {
	if all {
		seq = "1:9999999"
	} else if subject != "" {
		seq = resolveSeqBySubject(ic, subject)
	}

	if seq == "" {
		fmt.Fprintf(os.Stderr, "no matches\n")
		return nil
	}

	mm, err := ic.Fetch(seq)
	if err != nil {
		return err
	}

	// arrange workdir
	if syncDirPath != "." {
		if err := os.MkdirAll(syncDirPath, os.ModeDir); err != nil {
			return err
		}
	}

	for _, m := range mm {
		textMsg, err := decodeMessageAsTextMessage(m, false)
		if err != nil {
			return err
		}

		if err := writeMessage(textMsg, header, syncDirPath, ext, msgWriter); err != nil {
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

func writeMessage(msg *mail.Message, header bool, syncDirPath, ext string, msgWriter MsgWriter) error {
	buff := new(bytes.Buffer)

	if header {
		for k, v := range msg.Header {
			buff.Write([]byte(k))
			buff.Write([]byte{':', ' '})
			buff.Write([]byte(v[0]))
			buff.Write([]byte{'\r', '\n'})
		}
		buff.Write([]byte{'\r', '\n'})
	}

	body, err := ioutil.ReadAll(msg.Body)
	if err != nil {
		return fmt.Errorf("on subject[%v]: body reading error: %v", msg.Header.Get("Subject"), err)
	}
	buff.Write(body)

	tm, err := msg.Header.Date()
	if err != nil {
		return err
	}

	pomiExt := msg.Header.Get("X-Pomi-Ext")
	if len(pomiExt) > 0 {
		ext = pomiExt
	}

	err = msgWriter(syncDirPath, msg.Header.Get("Subject"), ext, tm, buff)
	return err
}

func getOutputWriteCloser(dir, subject, ext string) (*os.File, error) {
	file, err := os.Create(filepath.Join(dir, subject+"."+ext))
	if err != nil {
		return nil, err
	}
	return file, nil
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
