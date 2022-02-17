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

package handlers

import (
	"context"
	"fmt"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/info"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/reexec"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/agent/storage/store"
	"github.com/elastic/elastic-agent/internal/pkg/core/logger"
	"github.com/elastic/elastic-agent/internal/pkg/fleetapi"
)

type reexecManager interface {
	ReExec(cb reexec.ShutdownCallbackFn, argOverrides ...string)
}

// Settings handles settings change coming from fleet and updates log level.
type Settings struct {
	log       *logger.Logger
	reexec    reexecManager
	agentInfo *info.AgentInfo
}

// NewSettings creates a new Settings handler.
func NewSettings(
	log *logger.Logger,
	reexec reexecManager,
	agentInfo *info.AgentInfo,
) *Settings {
	return &Settings{
		log:       log,
		reexec:    reexec,
		agentInfo: agentInfo,
	}
}

// Handle handles SETTINGS action.
func (h *Settings) Handle(ctx context.Context, a fleetapi.Action, acker store.FleetAcker) error {
	h.log.Debugf("handlerUpgrade: action '%+v' received", a)
	action, ok := a.(*fleetapi.ActionSettings)
	if !ok {
		return fmt.Errorf("invalid type, expected ActionSettings and received %T", a)
	}

	if !isSupportedLogLevel(action.LogLevel) {
		return fmt.Errorf("invalid log level, expected debug|info|warning|error and received '%s'", action.LogLevel)
	}

	if err := h.agentInfo.SetLogLevel(action.LogLevel); err != nil {
		return errors.New("failed to update log level", err)
	}

	if err := acker.Ack(ctx, a); err != nil {
		h.log.Errorf("failed to acknowledge SETTINGS action with id '%s'", action.ActionID)
	} else if err := acker.Commit(ctx); err != nil {
		h.log.Errorf("failed to commit acker after acknowledging action with id '%s'", action.ActionID)
	}

	h.reexec.ReExec(nil)
	return nil
}

func isSupportedLogLevel(level string) bool {
	return level == "error" || level == "debug" || level == "info" || level == "warning"
}
