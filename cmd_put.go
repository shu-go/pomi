package main

import (
	"fmt"
	"os"
)

type putCmd struct {
	Name string `help:"if rom stdin, specify the name of a message"`
}

func (c putCmd) Run(g globalCmd, args []string) error {
	config, err := loadConfig(g.Config)
	if err != nil {
		return err
	}
	setAuthVariables(config)

	ic, err := initIMAP(config)
	if err != nil {
		return err
	}
	ic.Logout() // re-connect in goroutine

	disp := func(fn string, err error) {
		if err == nil {
			fmt.Fprintf(os.Stderr, "putting %v\n", fn)
		} else {
			fmt.Fprintf(os.Stderr, "failed to put file %v: %v\n", fn, err)
		}
	}

	if len(c.Name) == 0 {
		fmt.Fprintf(os.Stderr, "searching files in %v\n", g.Dir)
	} else {
		fmt.Fprintf(os.Stderr, "searching files from stdin as %v\n", c.Name)
	}

	cnt, err := putMessages(config, g.Dir, args, c.Name, disp)
	if err != nil {
		return err
	}
	if cnt == 0 {
		fmt.Fprintf(os.Stderr, "no matches\n")
	}

	return nil
}
