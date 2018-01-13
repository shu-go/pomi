package main

type deleteCmd struct {
	All     bool   `help:"delete all messages"`
	Seq     string `help:"delete by seq. (comma seprated or s1:s2)"`
	Subject string `cli:"subject, subj"  help:"delete by subject"`
}

func (c deleteCmd) Run(g globalCmd) error {
	config, err := loadConfig(g.Config)
	if err != nil {
		return err
	}
	setAuthVariables(config)

	ic, err := initIMAP(config)
	if err != nil {
		return err
	}

	err = deleteMessage(ic, c.All, c.Subject, c.Seq)

	return err
}
