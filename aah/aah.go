// Copyright (c) Jeevanandam M (https://github.com/jeevatkm)
// go-aah/tools source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"aahframework.org/essentials"
	"aahframework.org/log"
)

const (
	header = `–––––––––––––––––––––––––––––––––––––––––––––––
   aah framework -  https://aahframework.org
–––––––––––––––––––––––––––––––––––––––––––––––
`
	isWindows     = (runtime.GOOS == "windows")
	aahImportPath = "aahframework.org/aah"
)

var (
	// Version no. of aah CLI tool
	Version = "0.1"

	gopath   string
	gocmd    string
	gosrcDir string
	subCmds  commands
)

// aah cli tool entry point
func main() {
	// if panic happens, recover and abort nicely :)
	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(error); ok {
				log.Fatalf("this is unexpected!!! | %s", err)
			}
			log.Fatal(r)
		}
	}()

	// check go is installed or not
	if !ess.LookExecutable("go") {
		log.Fatal("Unable to find Go executable in PATH")
	}

	var err error

	// get GOPATH, refer https://godoc.org/aahframework.org/essentials#GoPath
	if gopath, err = ess.GoPath(); err != nil {
		log.Fatal(err)
	}

	if gocmd, err = exec.LookPath("go"); err != nil {
		log.Fatal(err)
	}

	flag.Parse()
	args := flag.Args()
	gosrcDir = filepath.Join(gopath, "src")

	printHeader()
	if len(args) == 0 {
		displayUsage()
	}

	// find the command
	cmd, err := subCmds.Find(args[0])
	if err != nil {
		commandNotFound(args[0])
	}

	// Validate command arguments count
	if len(args)-1 > cmd.ArgsCount {
		log.Errorf("Too many arguments given. Run 'aah help command'.\n\n")
		os.Exit(2)
	}

	// running command
	cmd.Run(args[1:])
	return
}

//‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾‾
// Unexported methods
//___________________________________

func printHeader() {
	if !isWindows {
		fmt.Fprintf(os.Stdout, fmt.Sprintf("\033[1;32m%v\033[0m\n", header))
		return
	}
	fmt.Fprintf(os.Stdout, header)
}

func init() {
	_ = log.SetPattern("%level:-5 %message")

	// Adding list of commands. The order here is the order in
	// which commands are printed by 'aah help'.
	subCmds = commands{
		newCmd,
		runCmd,
		versionCmd,
		helpCmd,
	}
}
