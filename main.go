/*
Copyright 2021 Michael Gruener <michael.gruener@chaosmoon.net>

This file is part of Helm Spruce.

Helm Spruce is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Helm Spruce is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Helm Spruce. If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"regexp"

	"github.com/imdario/mergo"
	"github.com/mgruener/helm-spruce/internal/wrapper/spruce"
	"sigs.k8s.io/yaml"
)

var (
	valuesArgRegexp *regexp.Regexp
	helmCmdRegex    *regexp.Regexp
)

func init() {
	valuesArgRegexp = regexp.MustCompile("^(-f|--values)(?:=(.+))?$")
	helmCmdRegex = regexp.MustCompile("^.*helm[23]?$")
}

func runHelm() (errs []error) {
	var helmPath string
	var err error

	// make helm-spruce chainable; if it is called as "_helm", it tries
	// to call helm as "__helm"; if it is called as "__helm", it tries
	// to call helm as "___helm" and so  on
	executableName := path.Base(os.Args[0])
	if helmCmdRegex.MatchString(executableName) {
		executableName = fmt.Sprintf("_%s", executableName)

		helmPath, err = exec.LookPath(executableName)

		if err != nil {
			return append(errs, fmt.Errorf("failed to find Helm binary '%s'", executableName))
		}
	} else {
		helmPath, err = exec.LookPath("helm")

		if err != nil {
			return append(errs, fmt.Errorf("failed to find Helm binary 'helm'"))
		}
	}

	temporaryDirectory, err := ioutil.TempDir("", fmt.Sprintf("%s.", path.Base(os.Args[0])))

	if err != nil {
		return append(errs, fmt.Errorf("failed to create temporary directory: %s", err))
	}

	defer func() {
		err := os.RemoveAll(temporaryDirectory)

		if err != nil {
			errs = append(errs, fmt.Errorf("failed to remove temporary directory '%s': %s", temporaryDirectory, err))

			return
		}
	}()

	destValueFile := fmt.Sprintf("%s/%s", temporaryDirectory, "values.yaml")
	sourceValueFiles := make([]string, 0)
	helmArgs := make([]string, 0, len(os.Args[1:]))
	firstValueReplaced := false
loop:
	for args := os.Args[1:]; len(args) > 0; args = args[1:] {
		arg := args[0]

		if valuesArgRegexpMatches := valuesArgRegexp.FindStringSubmatch(arg); valuesArgRegexpMatches != nil {
			var filename string

			switch {
			case len(valuesArgRegexpMatches[2]) > 0:
				filename = valuesArgRegexpMatches[2]
			case len(args) > 1:
				filename = args[1]
			default:
				break loop
			}

			sourceValueFiles = append(sourceValueFiles, filename)

			if firstValueReplaced {
				if len(valuesArgRegexpMatches[2]) < 1 {
					args = args[1:]
				}
				continue
			}

			if len(valuesArgRegexpMatches[2]) > 0 {
				arg = fmt.Sprintf("%s=%s", valuesArgRegexpMatches[1], destValueFile)
			} else {
				helmArgs = append(helmArgs, arg)
				arg = destValueFile
				args = args[1:]
			}

			firstValueReplaced = true
		}
		helmArgs = append(helmArgs, arg)
	}

	var mergedValues map[string]interface{}
	for _, valueFile := range sourceValueFiles {
		data, err := ioutil.ReadFile(valueFile)
		if err != nil {
			return append(errs, fmt.Errorf("failed to open values file '%s': %s", valueFile, err))
		}

		var values map[string]interface{}
		err = yaml.Unmarshal(data, &values)
		if err != nil {
			return append(errs, fmt.Errorf("failed to parse values file '%s' as yaml: %s", valueFile, err))
		}

		if values == nil {
			values = make(map[string]interface{})
		}

		err = mergo.Merge(&mergedValues, values, mergo.WithOverride)
		if err != nil {
			return append(errs, fmt.Errorf("failed to merge values file '%s': %s", valueFile, err))
		}
	}

	if len(sourceValueFiles) > 0 {
		err = spruce.Eval(&mergedValues, false, []string{})
		if err != nil {
			return append(errs, fmt.Errorf("failed to eval merged helm values with spruce: %s", err))
		}

		data, err := yaml.Marshal(mergedValues)
		if err != nil {
			return append(errs, fmt.Errorf("failed to marshal merged and evaluated helm values to yaml: %s", err))
		}

		err = ioutil.WriteFile(destValueFile, data, 0644)
		if err != nil {
			return append(errs, fmt.Errorf("failed to write merged and evaluated helm values to destination file '%s': %s", destValueFile, err))
		}
	}

	cmd := exec.Command(helmPath, helmArgs...)

	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()

	if err != nil {
		return append(errs, fmt.Errorf("failed to run Helm: %s", err))
	}

	return
}

func main() {
	errs := runHelm()

	exitCode := 0

	for _, err := range errs {
		fmt.Fprintf(os.Stderr, "[helm-spruce] Error: %s\n", err)

		exitCode = 1
	}

	os.Exit(exitCode)
}
