// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	c "github.com/elastic/beats/v7/libbeat/common/cli"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/info"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/internal/pkg/agent/install"
	"github.com/elastic/elastic-agent/internal/pkg/cli"
)

func newUninstallCommandWithArgs(_ []string, streams *cli.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall permanent Elastic Agent from this system",
		Long: `This will uninstall permanent Elastic Agent from this system and will no longer be managed by this system.

Unless -f is used this command will ask confirmation before performing removal.
`,
		Run: func(c *cobra.Command, args []string) {
			if err := uninstallCmd(streams, c, args); err != nil {
				fmt.Fprintf(streams.Err, "Error: %v\n%s\n", err, troubleshootMessage())
				os.Exit(1)
			}
		},
	}

	cmd.Flags().BoolP("force", "f", false, "Force overwrite the current and do not prompt for confirmation")

	return cmd
}

func uninstallCmd(streams *cli.IOStreams, cmd *cobra.Command, args []string) error {
	isAdmin, err := install.HasRoot()
	if err != nil {
		return fmt.Errorf("unable to perform command while checking for administrator rights, %v", err)
	}
	if !isAdmin {
		return fmt.Errorf("unable to perform command, not executed with %s permissions", install.PermissionUser)
	}
	status, reason := install.Status()
	if status == install.NotInstalled {
		return fmt.Errorf("not installed")
	}
	if status == install.Installed && !info.RunningInstalled() {
		return fmt.Errorf("can only be uninstall by executing the installed Elastic Agent at: %s", install.ExecutablePath())
	}

	force, _ := cmd.Flags().GetBool("force")
	if status == install.Broken {
		if !force {
			fmt.Fprintf(streams.Out, "Elastic Agent is installed but currently broken: %s\n", reason)
			confirm, err := c.Confirm(fmt.Sprintf("Continuing will uninstall the broken Elastic Agent at %s. Do you want to continue?", paths.InstallPath), true)
			if err != nil {
				return fmt.Errorf("problem reading prompt response")
			}
			if !confirm {
				return fmt.Errorf("uninstall was cancelled by the user")
			}
		}
	} else {
		if !force {
			confirm, err := c.Confirm(fmt.Sprintf("Elastic Agent will be uninstalled from your system at %s. Do you want to continue?", paths.InstallPath), true)
			if err != nil {
				return fmt.Errorf("problem reading prompt response")
			}
			if !confirm {
				return fmt.Errorf("uninstall was cancelled by the user")
			}
		}
	}

	err = install.Uninstall(paths.ConfigFile())
	if err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "Elastic Agent has been uninstalled.\n")

	install.RemovePath(paths.InstallPath)
	return nil
}
