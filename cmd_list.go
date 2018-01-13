package main

import (
	"fmt"
	"os"
	"strings"
)

type listCmd struct {
	Criteria string `cli:"criteria=SEARCH_KEY, c"  default:"SUBJECT"  help:"filter by the search key"`
}

func (c listCmd) Run(g globalCmd, args []string) error {
	config, err := loadConfig(g.Config)
	if err != nil {
		return err
	}
	setAuthVariables(config)

	ic, err := initIMAP(config)
	if err != nil {
		return err
	}

	keyword := strings.Join(args, " ")
	list, err := listMessages(ic, c.Criteria, keyword)
	if err != nil {
		return fmt.Errorf("listing error: %v", err)
	}
	ic.Logout()

	if len(list) == 0 {
		fmt.Fprintf(os.Stderr, "no messages\n")
	} else {
		for _, e := range list {
			fmt.Printf("%d %v (%v)\n", e.Seq, e.Subject, e.Date)
		}
	}

	return nil
}
