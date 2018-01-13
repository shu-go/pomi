package main

type getCmd struct {
	All     bool   `help:"fetch all messages"`
	Seq     string `help:"fetch by seq. (comma seprated or s1:s2)"`
	Subject string `cli:"subject, subj"  help:"fetch by subject"`
	Ext     string `cli:"ext, e"  default:"txt"  help:"file extension"`
	Header  bool   `cli:"header, H"  help:"output mail headers"`
}

func (c getCmd) Run(g globalCmd) error {
	config, err := loadConfig(g.Config)
	if err != nil {
		return err
	}
	setAuthVariables(config)

	ic, err := initIMAP(config)
	if err != nil {
		return err
	}

	err = getMessages(ic, c.Header, c.All, c.Subject, c.Seq, g.Dir, c.Ext, filesWriter)
	ic.Logout()

	return err
}
