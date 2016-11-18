package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"bitbucket.org/shu/imapclient"
	"github.com/BurntSushi/toml"
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
}

func main() {
	app := cli.NewApp()
	app.Commands = []cli.Command{
		{
			Name:    "list",
			Aliases: []string{"l", "ls"},
			Usage:   "list messages in the box",
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
			Name:    "get",
			Aliases: []string{"g"},
			Usage:   "get messages from the box",
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "all", Usage: "fetch all messages"},
				cli.StringFlag{Name: "seq", Usage: "fetch by seq. (comma separated or s1:s2"},
				cli.StringFlag{Name: "subject, subj, s", Usage: "fetch by subject"},
				cli.StringFlag{Name: "output, out, o", Value: "stdout", Usage: "{stdout, subject}"},
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

				//fmt.Fprintf(os.Stdout, "%v\n", seq)
				//return nil
				return getMessagesBySeq(ic, seq, c.Bool("header"), c.String("output"), c.String("ext"))
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
				for _, fn := range filenames {
					f, err := os.Open(fn)
					if err != nil {
						fmt.Fprintf(os.Stderr, "failed to open file %v: %v", fn, err)
						continue
					}

					_, subject := filepath.Split(fn)
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
				return nil
			},
		},
	}
	app.Name = "pomi"
	app.Usage = "get or put contents in IMAP box"
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "config, conf", Value: "./pomi.toml", Usage: "load the configuration from `CONFIG`"},
	}
	app.Run(os.Args)
	return
}

func loadConfig(c *cli.Context) (*config, error) {
	config := new(config)
	_, err := toml.DecodeFile(c.GlobalString("config"), config)
	if err != nil {
		return nil, fmt.Errorf("failed to open %v: %v\n", c.String("config"), err)
	}
	return config, nil
}

func initIMAP(config *config) (*imapclient.Client, error) {
	if config.IMAP.User == "" {
		config.IMAP.User = os.Getenv("IMAP_USER")
	}
	if config.IMAP.Pass == "" {
		config.IMAP.Pass = os.Getenv("IMAP_PASS")
	}

	c, err := imapclient.NewClient("tcp", config.IMAP.Server)
	if err != nil {
		return nil, fmt.Errorf("can't connect to %v: %v\n", config.IMAP.Server, err)
	}

	err = c.Login(config.IMAP.User, config.IMAP.Pass)
	if err != nil {
		return nil, fmt.Errorf("can't login as %v: %v\n", config.IMAP.User, err)
	}

	err = c.Select(config.IMAP.Box)
	if err != nil {
		return nil, fmt.Errorf("can't select box %v: %v\n", config.IMAP.Box, err)
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

func getMessagesBySeq(c *imapclient.Client, seq string, header bool, output, ext string) error {
	mm, err := c.Fetch(seq)
	if err != nil {
		return err
	}

	for _, m := range mm {
		m, err = imapclient.DecodeMailMessage(m)
		if err != nil {
			return err
		}

		file, err := getOutputWriteCloser(output, m.Header.Get("Subject"), ext)
		if err != nil {
			return err
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
			return fmt.Errorf("body reading error: %v", err)
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
					fmt.Fprintf(os.Stderr, "failed to change timestamp of %q: %v\n", name, err)
				}
			}
		}
	}

	return nil
}

func getOutputWriteCloser(output, subject, ext string) (*os.File, error) {
	switch output {
	case "stdout":
		return os.Stdout, nil
	case "subject":
		file, err := os.Create(subject + "." + ext)
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
