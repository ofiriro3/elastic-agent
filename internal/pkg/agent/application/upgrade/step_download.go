// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package upgrade

import (
	"context"
	"strings"

	"go.elastic.co/apm"

	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/artifact"
	"github.com/elastic/elastic-agent/internal/pkg/artifact/download"
	"github.com/elastic/elastic-agent/internal/pkg/artifact/download/composed"
	"github.com/elastic/elastic-agent/internal/pkg/artifact/download/fs"
	"github.com/elastic/elastic-agent/internal/pkg/artifact/download/http"
	downloader "github.com/elastic/elastic-agent/internal/pkg/artifact/download/localremote"
	"github.com/elastic/elastic-agent/internal/pkg/artifact/download/snapshot"
	"github.com/elastic/elastic-agent/internal/pkg/release"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

func (u *Upgrader) downloadArtifact(ctx context.Context, version, sourceURI string) (_ string, err error) {
	span, ctx := apm.StartSpan(ctx, "downloadArtifact", "app.internal")
	defer func() {
		apm.CaptureError(ctx, err).Send()
		span.End()
	}()
	// do not update source config
	settings := *u.settings
	if sourceURI != "" {
		if strings.HasPrefix(sourceURI, "file://") {
			// update the DropPath so the fs.Downloader can download from this
			// path instead of looking into the installed downloads directory
			settings.DropPath = strings.TrimPrefix(sourceURI, "file://")
		} else {
			settings.SourceURI = sourceURI
		}
	}

	u.log.Debugw("Downloading upgrade artifact", "version", version,
		"source_uri", settings.SourceURI, "drop_path", settings.DropPath,
		"target_path", settings.TargetDirectory, "install_path", settings.InstallPath)

	verifier, err := newVerifier(version, u.log, &settings)
	if err != nil {
		return "", errors.New(err, "initiating verifier")
	}

	fetcher, err := newDownloader(version, u.log, &settings)
	if err != nil {
		return "", errors.New(err, "initiating fetcher")
	}

	path, err := fetcher.Download(ctx, agentSpec, version)
	if err != nil {
		return "", errors.New(err, "failed upgrade of agent binary")
	}

	if err := verifier.Verify(agentSpec, version); err != nil {
		return "", errors.New(err, "failed verification of agent binary")
	}

	return path, nil
}

func newDownloader(version string, log *logger.Logger, settings *artifact.Config) (download.Downloader, error) {
	if !strings.HasSuffix(version, "-SNAPSHOT") {
		return downloader.NewDownloader(log, settings)
	}

	// try snapshot repo before official
	snapDownloader, err := snapshot.NewDownloader(log, settings, version)
	if err != nil {
		return nil, err
	}

	httpDownloader, err := http.NewDownloader(log, settings)
	if err != nil {
		return nil, err
	}

	return composed.NewDownloader(fs.NewDownloader(settings), snapDownloader, httpDownloader), nil
}

func newVerifier(version string, log *logger.Logger, settings *artifact.Config) (download.Verifier, error) {
	allowEmptyPgp, pgp := release.PGP()
	if !strings.HasSuffix(version, "-SNAPSHOT") {
		return downloader.NewVerifier(log, settings, allowEmptyPgp, pgp)
	}

	fsVerifier, err := fs.NewVerifier(settings, allowEmptyPgp, pgp)
	if err != nil {
		return nil, err
	}

	snapshotVerifier, err := snapshot.NewVerifier(settings, allowEmptyPgp, pgp, version)
	if err != nil {
		return nil, err
	}

	remoteVerifier, err := http.NewVerifier(settings, allowEmptyPgp, pgp)
	if err != nil {
		return nil, err
	}

	return composed.NewVerifier(fsVerifier, snapshotVerifier, remoteVerifier), nil
}
