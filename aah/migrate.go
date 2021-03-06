// Copyright (c) Jeevanandam M. (https://github.com/jeevatkm)
// aahframework.org/tools/aah source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"aahframework.org/aah.v0"
	"aahframework.org/config.v0"
	"aahframework.org/essentials.v0"
	"gopkg.in/urfave/cli.v1"
)

const aahGrammarIdentifier = "migrate.conf"
const aahGrammarFetchLoc = "https://cdn.aahframework.org/" + aahGrammarIdentifier

var migrateCmd = cli.Command{
	Name:    "migrate",
	Aliases: []string{"m"},
	Usage:   "Migrates application codebase to current version of aah (currently beta)",
	Description: `Command migrate is to house migration related sub-commands of aah.
  Currently it supports Go source code migrate.

	To know more about available 'migrate' sub commands:
		aah h m
		aah help migrate

	To know more about individual sub-commands details:
		aah m h c
		aah migrate help code
`,
	Subcommands: []cli.Command{
		cli.Command{
			Name:    "code",
			Aliases: []string{"c"},
			Usage:   "Migrates application codebase by making it compatible with current version of aah",
			Description: `Command code is to fix/upgrade aah's breaking changes and deprecated elements
  in application codebase to the current version of aah.

  The goal of 'Code' command is to keep aah users always up-to-date with latest version of aah.

	Note: Migrate does not take file backup, assumes application use version control.

	Example of script command:
		aah m c -i github.com/user/appname
		aah migrate code --importpath github.com/user/appname
			`,
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "i, importpath",
					Usage: "Import path of aah application",
				},
			},
			Action: migrateCodeAction,
		},
	},
}

func migrateCodeAction(c *cli.Context) error {
	importPath := appImportPath(c)
	if err := aah.Init(importPath); err != nil {
		logFatal(err)
	}

	projectCfg := aahProjectCfg(aah.AppBaseDir())
	cliLog = initCLILogger(projectCfg)

	cliLog.Warn("Migrate command does not take file backup. It assumes application use version control.")
	if c.GlobalBool("y") || c.GlobalBool("yes") {
		fmt.Println("\nWould you like to continue? [y/N]: y")
	} else if !collectYesOrNo(reader, "Would you like to continue? [y/N]") {
		cliLog.Info("Okay, I respect your choice. Bye.")
		return nil
	}

	grammarFile := filepath.Join(aahPath(), aahGrammarIdentifier)
	if !ess.IsFileExists(grammarFile) {
		cliLog.Info("Fetch migrate configuration from ", aahGrammarFetchLoc)
		if err := fetchFile(grammarFile, aahGrammarFetchLoc); err != nil {
			logFatal(err)
		}
	}

	grammarCfg, err := config.LoadFile(grammarFile)
	if err != nil {
		logFatal(err)
	}

	cliLog.Info("\nNote:")
	cliLog.Info("-----")
	cliLog.Info("Command works based on `migrate.conf` file. If you identify a new grammar entry, \n" +
		"create an issue at https://aahframework.org/issues.\n")

	cliLog.Infof("Loaded migrate configuration: %s", grammarFile)
	cliLog.Infof("Loaded aah project file: %s", filepath.Join(aah.AppBaseDir(), aahProjectIdentifier))
	cliLog.Infof("Migrate starts for '%s' [%s]", aah.AppName(), aah.AppImportPath())

	// Go Source files
	cliLog.Infof("Go source code migrate starts ...")
	if migrateGoSrcFiles(projectCfg, grammarCfg) == 0 {
		cliLog.Info("   It seems application Go source code are up-to-date")
	}
	cliLog.Infof("Go source code migrate successful")

	if ess.IsFileExists(filepath.Join(aah.AppBaseDir(), "views")) {
		// View files
		cliLog.Infof("View file migrate starts ...")
		if migrateViewFiles(projectCfg, grammarCfg) == 0 {
			cliLog.Info("   It seems application view files are up-to-date")
		}
		cliLog.Infof("View file migrate successful")
	}

	cliLog.Infof("Migrate successful for '%s' [%s]\n", aah.AppName(), aah.AppImportPath())
	return nil
}

func migrateGoSrcFiles(projectCfg, grammarCfg *config.Config) int {
	count := 0
	grammar, found := grammarCfg.StringList("file.go.upgrade_replacer")
	if !found {
		cliLog.Info("Config 'file.go.upgrades_replacer' not found in the grammar file")
		return count
	}

	fixer := strings.NewReplacer(grammar...)
	excludes, _ := projectCfg.StringList("build.ast_excludes")
	files, _ := ess.FilesPathExcludes(filepath.Join(aah.AppBaseDir(), "app"), true, ess.Excludes(excludes))
	for _, f := range files {
		if filepath.Ext(f) != ".go" {
			continue
		}
		if !migrateFile(f, fixer) {
			continue
		}
		count++
	}

	return count
}

func migrateViewFiles(projectCfg, grammarCfg *config.Config) int {
	count := 0
	grammar, found := grammarCfg.StringList("file.view.upgrade_replacer")
	if !found {
		cliLog.Info("Config 'file.view.upgrades_replacer' not found in the grammar file")
		return count
	}

	fixer := strings.NewReplacer(grammar...)
	files, _ := ess.FilesPath(filepath.Join(aah.AppBaseDir(), "views"), true)
	fileExt := aah.AppConfig().StringDefault("view.ext", ".html")
	for _, f := range files {
		if filepath.Ext(f) != fileExt {
			continue
		}
		if !migrateFile(f, fixer) {
			continue
		}
		count++
	}

	return count
}

func migrateFile(f string, fixer *strings.Replacer) bool {
	df := strings.TrimPrefix(filepath.ToSlash(stripGoSrcPath(f)), aah.AppImportPath()+"/")
	fileBytes, err := ioutil.ReadFile(f)
	if err != nil {
		logError(err)
		cliLog.Infof("  |-- skipped: %s", df)
		return false
	}

	modFileBytes := []byte(fixer.Replace(string(fileBytes)))
	if bytes.Equal(fileBytes, modFileBytes) {
		// not modified
		return false
	}

	if filepath.Ext(f) == ".go" {
		// format go src file
		var err error
		if modFileBytes, err = format.Source(modFileBytes); err != nil {
			logErrorf("While formating: %s", err)
			cliLog.Infof("  |-- skipped: %s", df)
			return false
		}
	}

	if err = os.Truncate(f, 0); err != nil {
		logErrorf("While truncate: %s", err)
		cliLog.Infof("  |-- skipped: %s", df)
		return false
	}

	if err = ioutil.WriteFile(f, modFileBytes, permRWRWRW); err != nil {
		logError(err)
		cliLog.Infof("  |-- [ERROR] processed: %s", df)
	} else {
		cliLog.Infof("  |-- processed: %s", df)
	}

	return true
}
