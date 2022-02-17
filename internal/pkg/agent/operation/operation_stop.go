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
	"github.com/elastic/elastic-agent/internal/pkg/core/logger"
	"github.com/elastic/elastic-agent/internal/pkg/core/state"
)

// operationStop stops the running process
// skips if process is already skipped
type operationStop struct {
	logger         *logger.Logger
	operatorConfig *configuration.SettingsConfig
}

func newOperationStop(
	logger *logger.Logger,
	operatorConfig *configuration.SettingsConfig) *operationStop {
	return &operationStop{
		logger:         logger,
		operatorConfig: operatorConfig,
	}
}

// Name is human readable name identifying an operation
func (o *operationStop) Name() string {
	return "operation-stop"
}

// Check checks whether application needs to be stopped.
//
// If the application state is not stopped then stop should be performed.
func (o *operationStop) Check(_ context.Context, application Application) (bool, error) {
	if application.State().Status != state.Stopped {
		return true, nil
	}
	return false, nil
}

// Run runs the operation
func (o *operationStop) Run(ctx context.Context, application Application) (err error) {
	application.Stop()
	return nil
}
