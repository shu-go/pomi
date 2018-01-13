package main

type showCmd struct {
	All     bool   `help:"show all messages"`
	Seq     string `help:"show by seq. (comma seprated or s1:s2)"`
	Subject string `cli:"subject, subj"  help:"show by subject"`
	Header  bool   `cli:"header, H"  help:"output mail headers"`
}

func (c showCmd) Run(g globalCmd) error {
	config, err := loadConfig(g.Config)
	if err != nil {
		return err
	}
	setAuthVariables(config)

	ic, err := initIMAP(config)
	if err != nil {
		return err
	}

	err = getMessages(ic, c.Header, c.All, c.Subject, c.Seq, g.Dir, "", stdoutWriter)
	ic.Logout()

	return err
}
