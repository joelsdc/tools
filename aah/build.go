// Copyright (c) Jeevanandam M. (https://github.com/jeevatkm)
// go-aah/tools/aah source code and usage is governed by a MIT style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"gopkg.in/urfave/cli.v1"

	"aahframework.org/aah.v0"
	"aahframework.org/config.v0"
	"aahframework.org/essentials.v0"
	"aahframework.org/log.v0"
)

var buildCmd = cli.Command{
	Name:    "build",
	Aliases: []string{"b"},
	Usage:   "Builds aah application for deployment (single or non-single)",
	Description: `Builds aah application for deployment. It supports single and non-single
	binary. Its a trade-off learn more https://docs.aahframework.org/build-packaging.html

	Artifact naming convention:  <appbinaryname>-<appversion>-<goos>-<goarch>.zip
	For e.g.: aahwebsite-381eaa8-darwin-amd64.zip

	Examples of short and long flags:
    aah build
		aah build --single
    aah build -e dev
    aah build -i github.com/user/appname -o /Users/jeeva -e qa
		aah build -i github.com/user/appname -o /Users/jeeva/aahwebsite.zip
		aah build --importpath github.com/user/appname --output /Users/jeeva --envprofile qa`,
	Action: buildAction,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "i, importpath",
			Usage: "Import path of aah application",
		},
		cli.StringFlag{
			Name:  "e, envprofile",
			Usage: "Environment profile name to activate (e.g: dev, qa, prod)",
		},
		cli.StringFlag{
			Name:  "o, output",
			Usage: "Output of aah application build artifact; the default is '<appbasedir>/build/<appbinaryname>-<appversion>-<goos>-<goarch>.zip'",
		},
		cli.BoolFlag{
			Name:  "s, single",
			Usage: "Creates aah single application binary",
		},
	},
}

func buildAction(c *cli.Context) error {
	importPath := appImportPath(c)
	if err := aah.Init(importPath); err != nil {
		logFatal(err)
	}

	projectCfg := aahProjectCfg(aah.AppBaseDir())
	cliLog = initCLILogger(projectCfg)

	cliLog.Infof("Loaded aah project file: %s", filepath.Join(aah.AppBaseDir(), aahProjectIdentifier))
	cliLog.Infof("Build starts for '%s' [%s]", aah.AppName(), aah.AppImportPath())

	if c.Bool("s") || c.Bool("single") {
		buildSingleBinary(c, projectCfg)
	} else {
		buildBinary(c, projectCfg)
	}

	return nil
}

func buildBinary(c *cli.Context, projectCfg *config.Config) {
	appBaseDir := aah.AppBaseDir()
	cleanupAutoGenVFSFiles(appBaseDir)

	appBinary, err := compileApp(&compileArgs{
		Cmd:        "BuildCmd",
		ProjectCfg: projectCfg,
		AppPack:    true,
	})
	if err != nil {
		logFatal(err)
	}

	appProfile := firstNonEmpty(c.String("e"), c.String("envprofile"), "prod")
	buildBaseDir, err := copyFilesToWorkingDir(projectCfg, appBaseDir, appBinary, appProfile)
	if err != nil {
		logFatal(err)
	}

	destArchiveFile := createZipArchiveName(c, projectCfg, appBaseDir, appBinary)

	// Creating app archive
	if err = createZipArchive(buildBaseDir, destArchiveFile); err != nil {
		logFatal(err)
	}

	cliLog.Infof("Build successful for '%s' [%s]", aah.AppName(), aah.AppImportPath())
	cliLog.Infof("Application artifact is here: %s\n", destArchiveFile)
}

func buildSingleBinary(c *cli.Context, projectCfg *config.Config) {
	cliLog.Infof("Embed starts for '%s' [%s]", aah.AppName(), aah.AppImportPath())
	appBaseDir := aah.AppBaseDir()
	defer cleanupAutoGenVFSFiles(appBaseDir)

	excludes, _ := projectCfg.StringList("build.excludes")
	noGzipList, _ := projectCfg.StringList("vfs.no_gzip")

	// Default mount point
	if err := processMount(appBaseDir, "/app", appBaseDir, ess.Excludes(excludes), noGzipList); err != nil {
		logFatal(err)
	}

	// Custom mount points
	mountKeys := projectCfg.KeysByPath("vfs.mount")
	for _, key := range mountKeys {
		vroot := projectCfg.StringDefault("vfs.mount."+key+".mount_path", "")
		proot := projectCfg.StringDefault("vfs.mount."+key+".physical_path", "")

		if !filepath.IsAbs(proot) {
			logErrorf("vfs %s: physical_path is not absolute path, skip mount: %s", proot, vroot)
			continue
		}

		if !ess.IsStrEmpty(vroot) && !ess.IsStrEmpty(proot) {
			cliLog.Infof("|--- Processing mount: '%s' <== '%s'", vroot, proot)
			if err := processMount(appBaseDir, vroot, proot, ess.Excludes(excludes), noGzipList); err != nil {
				logFatal(err)
			}
		}
	}
	cliLog.Infof("Embed successful for '%s' [%s]", aah.AppName(), aah.AppImportPath())

	appBinary, err := compileApp(&compileArgs{
		Cmd:        "BuildCmd",
		ProjectCfg: projectCfg,
		AppPack:    true,
		AppEmbed:   true,
	})
	if err != nil {
		logFatal(err)
	}

	// Creating app archive
	destArchiveFile := createZipArchiveName(c, projectCfg, appBaseDir, appBinary)
	if err = createZipArchive(appBinary, destArchiveFile); err != nil {
		logFatal(err)
	}

	cliLog.Infof("Build successful for '%s' [%s]", aah.AppName(), aah.AppImportPath())
	cliLog.Infof("Application artifact is here: %s\n", destArchiveFile)
}

func copyFilesToWorkingDir(projectCfg *config.Config, appBaseDir, appBinary, appProfile string) (string, error) {
	appBinaryName := filepath.Base(appBinary)
	tmpDir, err := ioutil.TempDir("", appBinaryName)
	if err != nil {
		return "", fmt.Errorf("unable to get temp directory: %s", err)
	}

	buildBaseDir := filepath.Join(tmpDir, ess.StripExt(appBinaryName))
	ess.DeleteFiles(buildBaseDir)
	if err = ess.MkDirAll(buildBaseDir, permRWXRXRX); err != nil {
		return "", err
	}

	// binary file
	binDir := filepath.Join(buildBaseDir, "bin")
	_ = ess.MkDirAll(binDir, permRWXRXRX)
	_, _ = ess.CopyFile(binDir, appBinary)

	// apply executable file mode
	if err = ess.ApplyFileMode(filepath.Join(binDir, appBinaryName), permRWXRXRX); err != nil {
		log.Error(err)
	}

	// build package excludes
	cfgExcludes, _ := projectCfg.StringList("build.excludes")
	excludes := ess.Excludes(cfgExcludes)
	if err = excludes.Validate(); err != nil {
		return "", err
	}

	// aah application and custom directories
	appDirs, _ := ess.DirsPath(appBaseDir, false)
	subTreeExcludes := ess.Excludes(excludeAndCreateSlice(cfgExcludes, "app"))
	for _, srcdir := range appDirs {
		if excludes.Match(filepath.Base(srcdir)) {
			continue
		}

		if ess.IsFileExists(srcdir) {
			if err = ess.CopyDir(buildBaseDir, srcdir, subTreeExcludes); err != nil {
				return "", err
			}
		}
	}

	// startup files
	data := map[string]string{
		"AppName":    ess.StripExt(appBinaryName),
		"AppProfile": appProfile,
		"Backtick":   "`",
	}
	buf := &bytes.Buffer{}
	if err = renderTmpl(buf, aahBashStartupTemplate, data); err != nil {
		return "", err
	}
	if err = ioutil.WriteFile(filepath.Join(buildBaseDir, "aah.sh"), buf.Bytes(), permRWXRXRX); err != nil {
		return "", err
	}

	buf.Reset()
	if err = renderTmpl(buf, aahCmdStartupTemplate, data); err != nil {
		return "", err
	}
	err = ioutil.WriteFile(filepath.Join(buildBaseDir, "aah.cmd"), buf.Bytes(), permRWXRXRX)

	return buildBaseDir, err
}

func createZipArchive(buildBaseDir, destArchiveFile string) error {
	ess.DeleteFiles(destArchiveFile)

	archiveBaseDir := filepath.Dir(destArchiveFile)
	if err := ess.MkDirAll(archiveBaseDir, permRWXRXRX); err != nil {
		return err
	}
	return ess.Zip(destArchiveFile, buildBaseDir)
}

func createZipArchiveName(c *cli.Context, projectCfg *config.Config, appBaseDir, appBinary string) string {
	var err error
	outputFile := firstNonEmpty(c.String("o"), c.String("output"))
	archiveName := ess.StripExt(filepath.Base(appBinary)) + "-" + getAppVersion(appBaseDir, projectCfg)
	archiveName = addTargetBuildInfo(archiveName)

	var destArchiveFile string
	if ess.IsStrEmpty(outputFile) {
		destArchiveFile = filepath.Join(appBaseDir, "build", archiveName)
	} else {
		destArchiveFile, err = filepath.Abs(outputFile)
		if err != nil {
			logFatal(err)
		}

		if !strings.HasSuffix(destArchiveFile, ".zip") {
			destArchiveFile = filepath.Join(destArchiveFile, archiveName)
		}
	}

	if !strings.HasSuffix(destArchiveFile, ".zip") {
		destArchiveFile = destArchiveFile + ".zip"
	}
	return destArchiveFile
}

const aahBashStartupTemplate = `#!/usr/bin/env bash

# The MIT License (MIT)
#
# Copyright (c) Jeevanandam M., https://myjeeva.com <jeeva@myjeeva.com>
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in all
# copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

###########################################
# Start and Stop script for aah application
###########################################

APP_NAME="{{ .AppName }}"
APP_ENV_PROFILE="{{ .AppProfile }}"
APP_EXT_CONFIG=""

if [ ! -z "$2" ]; then
  APP_ENV_PROFILE=$2
fi

if [ ! -z "$3" ]; then
  APP_EXT_CONFIG="-config=$3"
fi

# resolve links - $0 may be a softlink
PRG="$0"
while [ -h "$PRG" ] ; do
  ls={{ .Backtick }}ls -ld "$PRG"{{ .Backtick }}
  link={{ .Backtick }}expr "$ls" : '.*-> \(.*\)$'{{ .Backtick }}
  if expr "$link" : '/.*' > /dev/null; then
    PRG="$link"
  else
    PRG={{ .Backtick }}dirname "$PRG"{{ .Backtick }}/"$link"
  fi
done

# resolve APP_DIR and set executable
APP_DIR=$(cd "$(dirname $PRG)"; pwd)
APP_EXECUTABLE="$APP_DIR"/bin/"$APP_NAME"
APP_PID="$APP_DIR"/"$APP_NAME".pid

if [ ! -x "$APP_EXECUTABLE" ]; then
  echo "Cannot find aah application executable: $APP_EXECUTABLE"
  exit 1
fi

# go to application base directory
cd "$APP_DIR"

start() {
  if [ ! -z "$APP_PID" ]; then # not empty
    if [ -f "$APP_PID" ]; then # exists and regular file
      if [ -s "$APP_PID" ]; then # not-empty
        echo "Existing PID file found during start."
        if [ -r "$APP_PID" ]; then
          PID={{ .Backtick }}cat "$APP_PID"{{ .Backtick }}
          ps -p $PID >/dev/null 2>&1
          if [ $? -eq 0 ] ; then
            echo "$APP_NAME appears to still be running with PID $PID. Start aborted."
            ps -f -p $PID
            exit 1
          fi
        fi
      fi
    fi
  fi

  nohup "$APP_EXECUTABLE" -profile="$APP_ENV_PROFILE" "$APP_EXT_CONFIG" > appstart.log 2>&1 &
  echo "$APP_NAME started."
}

stop() {
  if [ ! -z "$APP_PID" ]; then # not empty
    if [ -f "$APP_PID" ]; then # exists and regular file
      if [ -s "$APP_PID" ]; then # not-empty
				PID={{ .Backtick }}cat "$APP_PID"{{ .Backtick }}
        kill -15 "$PID" >/dev/null 2>&1
        if [ $? -gt 0 ]; then
          echo "$APP_PID file found but no matching process was found. Stop aborted."
          exit 1
        else
          rm -f "$APP_PID" >/dev/null 2>&1
          echo "$APP_NAME stopped."
        fi
      else
        echo "$APP_PID file is empty and has been ignored."
      fi
    else
      echo "$APP_PID file does not exists. Stop aborted."
      exit 1
    fi
  fi
}

version() {
  "$APP_EXECUTABLE" -version
  echo ""
}

case "$1" in
start)
  start
  ;;
stop)
  stop
  ;;
restart)
  stop
  sleep 2
  start
  ;;
version)
  version
  ;;
*)
  echo "Usage: $0 {start|stop|restart|version}"
	echo ""
  exit 1
esac

exit 0
`

const aahCmdStartupTemplate = `@ECHO OFF

REM The MIT License (MIT)
REM
REM Copyright (c) Jeevanandam M., https://myjeeva.com <jeeva@myjeeva.com>
REM
REM Permission is hereby granted, free of charge, to any person obtaining a copy
REM of this software and associated documentation files (the "Software"), to deal
REM in the Software without restriction, including without limitation the rights
REM to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
REM copies of the Software, and to permit persons to whom the Software is
REM furnished to do so, subject to the following conditions:
REM
REM The above copyright notice and this permission notice shall be included in all
REM copies or substantial portions of the Software.
REM
REM THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
REM IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
REM FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
REM AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
REM LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
REM OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
REM SOFTWARE.

REM ##########################################
REM Start and Stop script for aah application
REM ##########################################

SETLOCAL ENABLEEXTENSIONS ENABLEDELAYEDEXPANSION

SET APP_NAME={{ .AppName }}
SET APP_ENV_PROFILE={{ .AppProfile }}
SET APP_EXT_CONFIG=""

IF NOT "%2" == "" (
	SET APP_ENV_PROFILE="%2"
)

IF NOT "%3" == "" (
  SET APP_EXT_CONFIG="-config %3"
)

REM resolve APP_DIR and set executable
SET APP_DIR=%~dp0
SET APP_EXECUTABLE=%APP_DIR%bin\%APP_NAME%.exe
SET APP_PID=%APP_DIR%%APP_NAME%.pid

REM change directory
cd %APP_DIR%

if ""%1"" == """" GOTO :cmdUsage
if ""%1"" == ""start"" GOTO :doStart
if ""%1"" == ""stop"" GOTO :doStop
if ""%1"" == ""version"" GOTO :doVersion

:doStart
REM check app is running already
tasklist /FI "IMAGENAME eq %APP_NAME%.exe" 2>NUL | find /I /N "%APP_NAME%.exe">NUL
IF "%ERRORLEVEL%" == "0" (
  ECHO %APP_NAME% appears to still be running. Start aborted.
  GOTO :end
)

START "" /B "%APP_EXECUTABLE%" -profile "%APP_ENV_PROFILE%" "%APP_EXT_CONFIG%" > appstart.log 2>&1
ECHO {{ .AppName }} started.
GOTO :end

:doStop
SET /P PID= < %APP_PID%
IF NOT %PID% == "" (
  taskkill /pid %PID% /f
	ECHO {{ .AppName }} stopped.
)
GOTO :end

:doVersion
%APP_EXECUTABLE% -version
GOTO :end

:cmdUsage
echo Usage: %0 {start or stop or version}
GOTO :end

:end
ENDLOCAL
`
