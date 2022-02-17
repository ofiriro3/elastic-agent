// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

// Code generated by elastic-agent/internals/dev-tools/cmd/buildspec/buildspec.go - DO NOT EDIT.

package program

import (
	"strings"

	"github.com/elastic/elastic-agent/internal/pkg/packer"
)

var Supported []Spec
var SupportedMap map[string]Spec

func init() {
	// Packed Files
	// internal/spec/apm-server.yml
	// internal/spec/endpoint.yml
	// internal/spec/filebeat.yml
	// internal/spec/fleet-server.yml
	// internal/spec/heartbeat.yml
	// internal/spec/metricbeat.yml
	// internal/spec/osquerybeat.yml
	// internal/spec/packetbeat.yml
	unpacked := packer.MustUnpack("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")
	SupportedMap = make(map[string]Spec)

	for f, v := range unpacked {
		s, err := NewSpecFromBytes(v)
		if err != nil {
			panic("Cannot read spec from " + f)
		}
		Supported = append(Supported, s)
		SupportedMap[strings.ToLower(s.Cmd)] = s
	}
}
