// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package path

import (
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/composable"
	"github.com/elastic/elastic-agent/internal/pkg/config"
	corecomp "github.com/elastic/elastic-agent/internal/pkg/core/composable"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

func init() {
	_ = composable.Providers.AddContextProvider("path", ContextProviderBuilder)
}

type contextProvider struct{}

// Run runs the Agent context provider.
func (*contextProvider) Run(comm corecomp.ContextProviderComm) error {
	err := comm.Set(map[string]interface{}{
		"home":   paths.Home(),
		"data":   paths.Data(),
		"config": paths.Config(),
		"logs":   paths.Logs(),
	})
	if err != nil {
		return errors.New(err, "failed to set mapping", errors.TypeUnexpected)
	}
	return nil
}

// ContextProviderBuilder builds the context provider.
func ContextProviderBuilder(_ *logger.Logger, _ *config.Config, managed bool) (corecomp.ContextProvider, error) {
	return &contextProvider{}, nil
}
