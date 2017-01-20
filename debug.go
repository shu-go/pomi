// +build debug

package main

import (
	"bitbucket.org/shu/log"
)

func init() {
	log.SetLevel(log.DebugLevel)
	log.SetFlags(log.DefaultHeader | log.Shortfile)
}
