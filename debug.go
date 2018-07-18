// +build debug

package main

import (
	"os"
)

func init() {
	//log.SetLevel(log.DebugLevel)
	//log.SetFlags(log.DefaultHeader | log.Shortfile)

	if dest := os.Getenv("POMI_LOG"); dest != "" {
		flags := os.O_CREATE | os.O_APPEND
		if dest[0] == '*' {
			flags = os.O_CREATE | os.O_RDWR
			dest = dest[1:]
		}
		f, err := os.OpenFile(dest, flags, 0x600)
		if err != nil {
			panic(err)
		}
		//log.SetOutput(f)
	}
}
