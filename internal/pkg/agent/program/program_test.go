// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package program

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"

	"github.com/elastic/elastic-agent/internal/pkg/agent/internal/yamltest"
	"github.com/elastic/elastic-agent/internal/pkg/agent/transpiler"
)

var (
	generateFlag = flag.Bool("generate", false, "Write golden files")
)

func TestGroupBy(t *testing.T) {
	t.Run("only named output", func(t *testing.T) {
		sConfig := map[string]interface{}{
			"outputs": map[string]interface{}{
				"special": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
				"infosec1": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},

			"inputs": []map[string]interface{}{
				{
					"type":       "log",
					"use_output": "special",
					"streams":    map[string]interface{}{"paths": "/var/log/hello.log"},
				},
				{
					"type":       "system/metrics",
					"use_output": "special",
				},
				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/infosec.log"},
					"use_output": "infosec1",
				},
			},
		}

		ast, err := transpiler.NewAST(sConfig)
		require.NoError(t, err)

		grouped, err := groupByOutputs(ast)
		require.NoError(t, err)
		require.Equal(t, 2, len(grouped))

		c1 := transpiler.MustNewAST(map[string]interface{}{
			"output": map[string]interface{}{
				"elasticsearch": map[string]interface{}{
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/hello.log"},
					"use_output": "special",
				},
				{
					"type":       "system/metrics",
					"use_output": "special",
				},
			},
		})

		c2, _ := transpiler.NewAST(map[string]interface{}{
			"output": map[string]interface{}{
				"elasticsearch": map[string]interface{}{
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/infosec.log"},
					"use_output": "infosec1",
				},
			},
		})

		defaultConfig, ok := grouped["special"]
		require.True(t, ok)
		require.Equal(t, c1.Hash(), defaultConfig.Hash())

		infosec1Config, ok := grouped["infosec1"]

		require.True(t, ok)
		require.Equal(t, c2.Hash(), infosec1Config.Hash())
	})

	t.Run("fail when the referenced named output doesn't exist", func(t *testing.T) {
		sConfig := map[string]interface{}{
			"monitoring": map[string]interface{}{
				"elasticsearch": map[string]interface{}{
					"hosts": "localhost",
				},
			},
			"outputs": map[string]interface{}{
				"default": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
				"infosec1": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},

			"inputs": []map[string]interface{}{
				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/hello.log"},
					"use_output": "special",
				},
				{
					"type":       "system/metrics",
					"use_output": "special",
				},
				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/infosec.log"},
					"use_output": "donotexist",
				},
			},
		}

		ast, err := transpiler.NewAST(sConfig)
		require.NoError(t, err)

		_, err = groupByOutputs(ast)
		require.Error(t, err)
	})

	t.Run("only default output", func(t *testing.T) {
		sConfig := map[string]interface{}{
			"outputs": map[string]interface{}{
				"default": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
				"infosec1": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":    "log",
					"streams": map[string]interface{}{"paths": "/var/log/hello.log"},
				},

				{
					"type": "system/metrics",
				},
				{
					"type":    "log",
					"streams": map[string]interface{}{"paths": "/var/log/infosec.log"},
				},
			},
		}

		ast, err := transpiler.NewAST(sConfig)
		require.NoError(t, err)

		grouped, err := groupByOutputs(ast)
		require.NoError(t, err)
		require.Equal(t, 1, len(grouped))

		c1 := transpiler.MustNewAST(map[string]interface{}{
			"output": map[string]interface{}{
				"elasticsearch": map[string]interface{}{
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":    "log",
					"streams": map[string]interface{}{"paths": "/var/log/hello.log"},
				},

				{
					"type": "system/metrics",
				},

				{
					"type":    "log",
					"streams": map[string]interface{}{"paths": "/var/log/infosec.log"},
				},
			},
		})

		defaultConfig, ok := grouped["default"]
		require.True(t, ok)
		require.Equal(t, c1.Hash(), defaultConfig.Hash())

		_, ok = grouped["infosec1"]

		require.False(t, ok)
	})

	t.Run("default and named output", func(t *testing.T) {
		sConfig := map[string]interface{}{
			"outputs": map[string]interface{}{
				"default": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
				"infosec1": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":    "log",
					"streams": map[string]interface{}{"paths": "/var/log/hello.log"},
				},

				{
					"type": "system/metrics",
				},

				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/infosec.log"},
					"use_output": "infosec1",
				},
			},
		}

		ast, err := transpiler.NewAST(sConfig)
		require.NoError(t, err)

		grouped, err := groupByOutputs(ast)
		require.NoError(t, err)
		require.Equal(t, 2, len(grouped))

		c1 := transpiler.MustNewAST(map[string]interface{}{
			"output": map[string]interface{}{
				"elasticsearch": map[string]interface{}{
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":    "log",
					"streams": map[string]interface{}{"paths": "/var/log/hello.log"},
				},

				{
					"type": "system/metrics",
				},
			},
		})

		c2, _ := transpiler.NewAST(map[string]interface{}{
			"output": map[string]interface{}{
				"elasticsearch": map[string]interface{}{
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},
			"inputs": []map[string]interface{}{
				{
					"type":       "log",
					"streams":    map[string]interface{}{"paths": "/var/log/infosec.log"},
					"use_output": "infosec1",
				},
			},
		})

		defaultConfig, ok := grouped["default"]
		require.True(t, ok)
		require.Equal(t, c1.Hash(), defaultConfig.Hash())

		infosec1Config, ok := grouped["infosec1"]

		require.True(t, ok)
		require.Equal(t, c2.Hash(), infosec1Config.Hash())
	})

	t.Run("streams is an empty list", func(t *testing.T) {
		sConfig := map[string]interface{}{
			"outputs": map[string]interface{}{
				"default": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
				"infosec1": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},
			"datasources": []map[string]interface{}{},
		}

		ast, err := transpiler.NewAST(sConfig)
		require.NoError(t, err)

		grouped, err := groupByOutputs(ast)
		require.NoError(t, err)
		require.Equal(t, 0, len(grouped))
	})

	t.Run("no streams are defined", func(t *testing.T) {
		sConfig := map[string]interface{}{
			"outputs": map[string]interface{}{
				"default": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "xxx",
					"username": "myusername",
					"password": "mypassword",
				},
				"infosec1": map[string]interface{}{
					"type":     "elasticsearch",
					"hosts":    "yyy",
					"username": "anotherusername",
					"password": "anotherpassword",
				},
			},
		}

		ast, err := transpiler.NewAST(sConfig)
		require.NoError(t, err)

		grouped, err := groupByOutputs(ast)
		require.NoError(t, err)
		require.Equal(t, 0, len(grouped))
	})
}

func TestConfiguration(t *testing.T) {
	defer os.Remove("fleet.yml")

	testcases := map[string]struct {
		programs map[string][]string
		err      bool
	}{
		"namespace": {
			programs: map[string][]string{
				"default": {"filebeat", "fleet-server", "heartbeat", "metricbeat", "endpoint", "packetbeat"},
			},
		},
		"logstash_config": {
			programs: map[string][]string{
				"default":       {"filebeat", "fleet-server", "metricbeat", "endpoint", "packetbeat"},
				"elasticsearch": {"filebeat"},
			},
		},
		"single_config": {
			programs: map[string][]string{
				"default": {"filebeat", "fleet-server", "heartbeat", "metricbeat", "endpoint", "packetbeat"},
			},
		},
		"audit_config": {
			programs: map[string][]string{
				"default": {"auditbeat"},
			},
		},
		"fleet_server": {
			programs: map[string][]string{
				"default": {"fleet-server"},
			},
		},
		"synthetics_config": {
			programs: map[string][]string{
				"default": {"heartbeat"},
			},
		},
		"enabled_true": {
			programs: map[string][]string{
				"default": {"filebeat"},
			},
		},
		"enabled_false": {
			programs: map[string][]string{
				"default": {},
			},
		},
		"enabled_output_true": {
			programs: map[string][]string{
				"default": {"filebeat"},
			},
		},
		"enabled_output_false": {
			programs: map[string][]string{},
		},
		"endpoint_basic": {
			programs: map[string][]string{
				"default": {"endpoint"},
			},
		},
		"endpoint_no_fleet": {
			programs: map[string][]string{
				"default": {},
			},
		},
		"endpoint_unknown_output": {
			programs: map[string][]string{
				"default": {},
			},
		},
		"endpoint_arm": {
			programs: map[string][]string{
				"default": {},
			},
		},
	}

	for name, test := range testcases {
		t.Run(name, func(t *testing.T) {
			singleConfig, err := ioutil.ReadFile(filepath.Join("testdata", name+".yml"))
			require.NoError(t, err)

			var m map[string]interface{}
			err = yaml.Unmarshal(singleConfig, &m)
			require.NoError(t, err)

			ast, err := transpiler.NewAST(m)
			require.NoError(t, err)

			programs, err := Programs(&fakeAgentInfo{}, ast)
			if test.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, len(test.programs), len(programs))

			if len(programs) > 0 {
				_, containsDefault := programs["default"]
				require.True(t, containsDefault)
			}

			for progKey, progs := range programs {
				testPrograms, isExpectedProgram := test.programs[progKey]
				require.True(t, isExpectedProgram)
				require.Equal(t, len(testPrograms), len(progs))

				for _, program := range progs {
					filename := name + "-" + strings.ToLower(program.Spec.Cmd)
					if progKey != "default" {
						filename += "-" + progKey
					}
					programConfig, err := ioutil.ReadFile(filepath.Join(
						"testdata",
						filename+".yml",
					))

					require.NoError(t, err)
					var m map[string]interface{}
					err = yamltest.FromYAML(programConfig, &m)
					require.NoError(t, errors.Wrap(err, program.Cmd()))

					compareMap := &transpiler.MapVisitor{}
					program.Config.Accept(compareMap)

					if !assert.True(t, cmp.Equal(m, compareMap.Content)) {
						diff := cmp.Diff(m, compareMap.Content)
						if diff != "" {
							t.Errorf("%s-%s mismatch (-want +got):\n%s", name, program.Spec.Name, diff)
						}
					}
				}
			}

		})
	}
}

func TestUseCases(t *testing.T) {
	defer os.Remove("fleet.yml")

	useCasesPath := filepath.Join("testdata", "usecases")
	useCases, err := filepath.Glob(filepath.Join(useCasesPath, "*.yml"))
	require.NoError(t, err)

	generatedFilesDir := filepath.Join(useCasesPath, "generated")

	// Cleanup all generated files to make sure not having any left overs
	if *generateFlag {
		err := os.RemoveAll(generatedFilesDir)
		require.NoError(t, err)
	}

	for _, usecase := range useCases {
		t.Run(usecase, func(t *testing.T) {

			useCaseName := strings.TrimSuffix(filepath.Base(usecase), ".yml")
			singleConfig, err := ioutil.ReadFile(usecase)
			require.NoError(t, err)

			var m map[string]interface{}
			err = yaml.Unmarshal(singleConfig, &m)
			require.NoError(t, err)

			ast, err := transpiler.NewAST(m)
			require.NoError(t, err)

			programs, err := Programs(&fakeAgentInfo{}, ast)
			require.NoError(t, err)

			require.Equal(t, 1, len(programs))

			defPrograms, ok := programs["default"]
			require.True(t, ok)

			for _, program := range defPrograms {
				generatedPath := filepath.Join(
					useCasesPath, "generated",
					useCaseName+"."+strings.ToLower(program.Spec.Cmd)+".golden.yml",
				)

				compareMap := &transpiler.MapVisitor{}
				program.Config.Accept(compareMap)

				// Generate new golden file for programm
				if *generateFlag {
					d, err := yaml.Marshal(&compareMap.Content)
					require.NoError(t, err)

					err = os.MkdirAll(generatedFilesDir, 0755)
					require.NoError(t, err)
					err = ioutil.WriteFile(generatedPath, d, 0644)
					require.NoError(t, err)
				}

				programConfig, err := ioutil.ReadFile(generatedPath)
				require.NoError(t, err)

				var m map[string]interface{}
				err = yamltest.FromYAML(programConfig, &m)
				require.NoError(t, errors.Wrap(err, program.Cmd()))

				if !assert.True(t, cmp.Equal(m, compareMap.Content)) {
					diff := cmp.Diff(m, compareMap.Content)
					if diff != "" {
						t.Errorf("%s-%s mismatch (-want +got):\n%s", usecase, program.Spec.Name, diff)
					}
				}
			}
		})
	}
}

type fakeAgentInfo struct{}

func (*fakeAgentInfo) AgentID() string {
	return "agent-id"
}

func (*fakeAgentInfo) Version() string {
	return "8.0.0"
}

func (*fakeAgentInfo) Snapshot() bool {
	return false
}

func (*fakeAgentInfo) Headers() map[string]string {
	return map[string]string{
		"h1": "test-header",
	}
}
