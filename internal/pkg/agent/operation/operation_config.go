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

package operation

import (
	"context"

	"github.com/elastic/elastic-agent/internal/pkg/agent/configuration"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/core/logger"
	"github.com/elastic/elastic-agent/internal/pkg/core/plugin/process"
	"github.com/elastic/elastic-agent/internal/pkg/core/state"
)

var (
	// ErrClientNotFound is an error when client is not found
	ErrClientNotFound = errors.New("client not found, check if process is running")
	// ErrClientNotConfigurable happens when stored client does not implement Config func
	ErrClientNotConfigurable = errors.New("client does not provide configuration")
)

// Configures running process by sending a configuration to its
// grpc endpoint
type operationConfig struct {
	logger         *logger.Logger
	operatorConfig *configuration.SettingsConfig
	cfg            map[string]interface{}
}

func newOperationConfig(
	logger *logger.Logger,
	operatorConfig *configuration.SettingsConfig,
	cfg map[string]interface{}) *operationConfig {
	return &operationConfig{
		logger:         logger,
		operatorConfig: operatorConfig,
		cfg:            cfg,
	}
}

// Name is human readable name identifying an operation
func (o *operationConfig) Name() string {
	return "operation-config"
}

// Check checks whether config needs to be run.
//
// Always returns true.
func (o *operationConfig) Check(_ context.Context, _ Application) (bool, error) { return true, nil }

// Run runs the operation
func (o *operationConfig) Run(ctx context.Context, application Application) (err error) {
	defer func() {
		if err != nil {
			// application failed to apply config but is running.
			s := state.Degraded
			if errors.Is(err, process.ErrAppNotRunning) {
				s = state.Failed
			}

			application.SetState(s, err.Error(), nil)
		}
	}()
	return application.Configure(ctx, o.cfg)
}
