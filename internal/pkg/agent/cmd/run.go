// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"go.elastic.co/apm"
	apmtransport "go.elastic.co/apm/transport"
	"gopkg.in/yaml.v2"

	"github.com/elastic/elastic-agent-libs/api"
	"github.com/elastic/elastic-agent-libs/logp"
	"github.com/elastic/elastic-agent-libs/monitoring"
	"github.com/elastic/elastic-agent-libs/service"
	"github.com/elastic/elastic-agent-system-metrics/report"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/filelock"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/info"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/reexec"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/secret"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade"
	"github.com/elastic/elastic-agent/internal/pkg/agent/cleaner"
	"github.com/elastic/elastic-agent/internal/pkg/agent/configuration"
	"github.com/elastic/elastic-agent/internal/pkg/agent/control/server"
	"github.com/elastic/elastic-agent/internal/pkg/agent/errors"
	"github.com/elastic/elastic-agent/internal/pkg/agent/migration"
	"github.com/elastic/elastic-agent/internal/pkg/agent/storage"
	"github.com/elastic/elastic-agent/internal/pkg/cli"
	"github.com/elastic/elastic-agent/internal/pkg/config"
	"github.com/elastic/elastic-agent/internal/pkg/core/monitoring/beats"
	monitoringCfg "github.com/elastic/elastic-agent/internal/pkg/core/monitoring/config"
	monitoringServer "github.com/elastic/elastic-agent/internal/pkg/core/monitoring/server"
	"github.com/elastic/elastic-agent/internal/pkg/core/status"
	"github.com/elastic/elastic-agent/internal/pkg/fileutil"
	"github.com/elastic/elastic-agent/internal/pkg/release"
	"github.com/elastic/elastic-agent/pkg/core/logger"
	"github.com/elastic/elastic-agent/version"
)

const (
	agentName = "elastic-agent"
)

type cfgOverrider func(cfg *configuration.Configuration)

func newRunCommandWithArgs(_ []string, streams *cli.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the elastic-agent.",
		Run: func(_ *cobra.Command, _ []string) {
			if err := run(nil); err != nil {
				logp.NewLogger("cmd_run").
					Errorw("run command finished with error",
						"error.message", err)
				fmt.Fprintf(streams.Err, "Error: %v\n%s\n", err, troubleshootMessage())

				// TODO: remove it. os.Exit will be called on main and if it's called
				// too early some goroutines with deferred functions related
				// to the shutdown process might not run.
				os.Exit(1)
			}
		},
	}
}

func run(override cfgOverrider) error {
	// Windows: Mark service as stopped.
	// After this is run, the service is considered by the OS to be stopped.
	// This must be the first deferred cleanup task (last to execute).
	defer func() {
		service.NotifyTermination()
		service.WaitExecutionDone()
	}()

	locker := filelock.NewAppLocker(paths.Data(), paths.AgentLockFileName)
	if err := locker.TryLock(); err != nil {
		return err
	}
	defer func() {
		_ = locker.Unlock()
	}()

	service.BeforeRun()
	defer service.Cleanup()

	// register as a service
	stop := make(chan bool)
	ctx, cancel := context.WithCancel(context.Background())
	var stopBeat = func() {
		close(stop)
	}
	service.HandleSignals(stopBeat, cancel)

	cfg, err := loadConfig(override)
	if err != nil {
		return err
	}

	logger, err := logger.NewFromConfig("", cfg.Settings.LoggingConfig, true)
	if err != nil {
		return err
	}

	cfg, err = tryDelayEnroll(ctx, logger, cfg, override)
	if err != nil {
		err = errors.New(err, "failed to perform delayed enrollment")
		logger.Error(err)
		return err
	}
	pathConfigFile := paths.AgentConfigFile()

	// agent ID needs to stay empty in bootstrap mode
	createAgentID := true
	if cfg.Fleet != nil && cfg.Fleet.Server != nil && cfg.Fleet.Server.Bootstrap {
		createAgentID = false
	}

	// This is specific for the agent upgrade from 8.3.0 - 8.3.2 to 8.x and above on Linux and Windows platforms.
	// Addresses the issue: https://github.com/elastic/elastic-agent/issues/682
	// The vault directory was located in the hash versioned "Home" directory of the agent.
	// This moves the vault directory two levels up into  the "Config" directory next to fleet.enc file
	// in order to be able to "upgrade" the agent from deb/rpm that is not invoking the upgrade handle and
	// doesn't perform the migration of the state or vault.
	// If the agent secret doesn't exist, then search for the newest agent secret in the agent data directories
	// and migrate it into the new vault location.
	err = migration.MigrateAgentSecret(logger)
	logger.Debug("migration of agent secret completed, err: %v", err)
	if err != nil {
		err = errors.New(err, "failed to perfrom the agent secret migration")
		logger.Error(err)
		return err
	}

	// Ensure we have the agent secret created.
	// The secret is not created here if it exists already from the previous enrollment.
	// This is needed for compatibility with agent running in standalone mode,
	// that writes the agentID into fleet.enc (encrypted fleet.yml) before even loading the configuration.
	err = secret.CreateAgentSecret()
	if err != nil {
		return err
	}

	// Check if the fleet.yml or state.yml exists and encrypt them.
	// This is needed to handle upgrade properly.
	// On agent upgrade the older version for example 8.2 unpacks the 8.3 agent
	// and tries to run it.
	// The new version of the agent requires encrypted configuration files or it will not start and upgrade will fail and revert.
	err = encryptConfigIfNeeded(logger)
	if err != nil {
		return err
	}

	// Start the old unencrypted agent configuration file cleaner
	startOldAgentConfigCleaner(ctx, logger)

	agentInfo, err := info.NewAgentInfoWithLog(defaultLogLevel(cfg), createAgentID)
	if err != nil {
		return errors.New(err,
			"could not load agent info",
			errors.TypeFilesystem,
			errors.M(errors.MetaKeyPath, pathConfigFile))
	}

	// initiate agent watcher
	if err := upgrade.InvokeWatcher(logger); err != nil {
		// we should not fail because watcher is not working
		logger.Error(errors.New(err, "failed to invoke rollback watcher"))
	}

	if allowEmptyPgp, _ := release.PGP(); allowEmptyPgp {
		logger.Info("Artifact has been built with security disabled. Elastic Agent will not verify signatures of the artifacts.")
	}

	execPath, err := reexecPath()
	if err != nil {
		return err
	}
	rexLogger := logger.Named("reexec")
	rex := reexec.NewManager(rexLogger, execPath)

	statusCtrl := status.NewController(logger)
	statusCtrl.SetAgentID(agentInfo.AgentID())

	tracer, err := initTracer(agentName, release.Version(), cfg.Settings.MonitoringConfig)
	if err != nil {
		return fmt.Errorf("could not initiate APM tracer: %w", err)
	}
	if tracer != nil {
		logger.Info("APM instrumentation enabled")
		defer func() {
			tracer.Flush(nil)
			tracer.Close()
		}()
	} else {
		logger.Info("APM instrumentation disabled")
	}

	control := server.New(logger.Named("control"), rex, statusCtrl, nil, tracer)
	// start the control listener
	if err := control.Start(); err != nil {
		return err
	}
	defer control.Stop()

	app, err := application.New(logger, rex, statusCtrl, control, agentInfo, tracer)
	if err != nil {
		return err
	}

	control.SetRouteFn(app.Routes)
	control.SetMonitoringCfg(cfg.Settings.MonitoringConfig)

	serverStopFn, err := setupMetrics(agentInfo, logger, cfg.Settings.DownloadConfig.OS(), cfg.Settings.MonitoringConfig, app, tracer, statusCtrl)
	if err != nil {
		return err
	}
	defer func() {
		_ = serverStopFn()
	}()

	if err := app.Start(); err != nil {
		return err
	}

	// listen for signals
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	reexecing := false
	for {
		breakout := false
		select {
		case <-stop:
			logger.Info("service.HandleSignals invoked stop function. Shutting down")
			breakout = true
		case <-rex.ShutdownChan():
			logger.Info("reexec Shutdown channel triggered")
			reexecing = true
			breakout = true
		case sig := <-signals:
			logger.Infof("signal %q received", sig)
			if sig == syscall.SIGHUP {
				logger.Infof("signals syscall.SIGHUP received, triggering agent restart")
				rex.ReExec(nil)
			} else {
				breakout = true
			}
		}
		if breakout {
			if !reexecing {
				logger.Info("Shutting down Elastic Agent and sending last events...")
			} else {
				logger.Info("Restarting Elastic Agent")
			}
			break
		}
	}

	err = app.Stop()
	if !reexecing {
		logger.Info("Shutting down completed.")
		return err
	}
	rex.ShutdownComplete()
	return err
}

func loadConfig(override cfgOverrider) (*configuration.Configuration, error) {
	pathConfigFile := paths.ConfigFile()
	rawConfig, err := config.LoadFile(pathConfigFile)
	if err != nil {
		return nil, errors.New(err,
			fmt.Sprintf("could not read configuration file %s", pathConfigFile),
			errors.TypeFilesystem,
			errors.M(errors.MetaKeyPath, pathConfigFile))
	}

	if err := getOverwrites(rawConfig); err != nil {
		return nil, errors.New(err, "could not read overwrites")
	}

	cfg, err := configuration.NewFromConfig(rawConfig)
	if err != nil {
		return nil, errors.New(err,
			fmt.Sprintf("could not parse configuration file %s", pathConfigFile),
			errors.TypeFilesystem,
			errors.M(errors.MetaKeyPath, pathConfigFile))
	}

	if override != nil {
		override(cfg)
	}

	return cfg, nil
}

func reexecPath() (string, error) {
	// set executable path to symlink instead of binary
	// in case of updated symlinks we should spin up new agent
	potentialReexec := filepath.Join(paths.Top(), agentName)

	// in case it does not exists fallback to executable
	if _, err := os.Stat(potentialReexec); os.IsNotExist(err) {
		return os.Executable()
	}

	return potentialReexec, nil
}

func getOverwrites(rawConfig *config.Config) error {
	cfg, err := configuration.NewFromConfig(rawConfig)
	if err != nil {
		return err
	}

	if !cfg.Fleet.Enabled {
		// overrides should apply only for fleet mode
		return nil
	}
	path := paths.AgentConfigFile()
	store := storage.NewEncryptedDiskStore(path)

	reader, err := store.Load()
	if err != nil && errors.Is(err, os.ErrNotExist) {
		// no fleet file ignore
		return nil
	} else if err != nil {
		return errors.New(err, "could not initialize config store",
			errors.TypeFilesystem,
			errors.M(errors.MetaKeyPath, path))
	}

	config, err := config.NewConfigFrom(reader)
	if err != nil {
		return errors.New(err,
			fmt.Sprintf("fail to read configuration %s for the elastic-agent", path),
			errors.TypeFilesystem,
			errors.M(errors.MetaKeyPath, path))
	}

	err = rawConfig.Merge(config)
	if err != nil {
		return errors.New(err,
			fmt.Sprintf("fail to merge configuration with %s for the elastic-agent", path),
			errors.TypeConfig,
			errors.M(errors.MetaKeyPath, path))
	}

	return nil
}

func defaultLogLevel(cfg *configuration.Configuration) string {
	if configuration.IsStandalone(cfg.Fleet) {
		// for standalone always take the one from config and don't override
		return ""
	}

	defaultLogLevel := logger.DefaultLogLevel.String()
	if configuredLevel := cfg.Settings.LoggingConfig.Level.String(); configuredLevel != "" && configuredLevel != defaultLogLevel {
		// predefined log level
		return configuredLevel
	}

	return defaultLogLevel
}

func setupMetrics(
	_ *info.AgentInfo,
	logger *logger.Logger,
	operatingSystem string,
	cfg *monitoringCfg.MonitoringConfig,
	app application.Application,
	tracer *apm.Tracer,
	statusCtrl status.Controller,
) (func() error, error) {
	if err := report.SetupMetrics(logger, agentName, version.GetDefaultVersion()); err != nil {
		return nil, err
	}

	// start server for stats
	endpointConfig := api.Config{
		Enabled: true,
		Host:    beats.AgentMonitoringEndpoint(operatingSystem, cfg.HTTP),
	}

	bufferEnabled := cfg.HTTP.Buffer != nil && cfg.HTTP.Buffer.Enabled
	s, err := monitoringServer.New(logger, endpointConfig, monitoring.GetNamespace, app.Routes, isProcessStatsEnabled(cfg.HTTP), bufferEnabled, tracer, statusCtrl)
	if err != nil {
		return nil, errors.New(err, "could not start the HTTP server for the API")
	}
	s.Start()

	if cfg.Pprof != nil && cfg.Pprof.Enabled {
		s.AttachPprof()
	}

	// return server stopper
	return s.Stop, nil
}

func isProcessStatsEnabled(cfg *monitoringCfg.MonitoringHTTPConfig) bool {
	return cfg != nil && cfg.Enabled
}

func tryDelayEnroll(ctx context.Context, logger *logger.Logger, cfg *configuration.Configuration, override cfgOverrider) (*configuration.Configuration, error) {
	enrollPath := paths.AgentEnrollFile()
	if _, err := os.Stat(enrollPath); err != nil {
		// no enrollment file exists or failed to stat it; nothing to do
		return cfg, nil
	}
	contents, err := ioutil.ReadFile(enrollPath)
	if err != nil {
		return nil, errors.New(
			err,
			"failed to read delay enrollment file",
			errors.TypeFilesystem,
			errors.M("path", enrollPath))
	}
	var options enrollCmdOption
	err = yaml.Unmarshal(contents, &options)
	if err != nil {
		return nil, errors.New(
			err,
			"failed to parse delay enrollment file",
			errors.TypeConfig,
			errors.M("path", enrollPath))
	}
	options.DelayEnroll = false
	options.FleetServer.SpawnAgent = false
	c, err := newEnrollCmd(
		logger,
		&options,
		paths.ConfigFile(),
	)
	if err != nil {
		return nil, err
	}
	err = c.Execute(ctx, cli.NewIOStreams())
	if err != nil {
		return nil, err
	}
	err = os.Remove(enrollPath)
	if err != nil {
		logger.Warn(errors.New(
			err,
			"failed to remove delayed enrollment file",
			errors.TypeFilesystem,
			errors.M("path", enrollPath)))
	}
	logger.Info("Successfully performed delayed enrollment of this Elastic Agent.")
	return loadConfig(override)
}

func initTracer(agentName, version string, mcfg *monitoringCfg.MonitoringConfig) (*apm.Tracer, error) {
	apm.DefaultTracer.Close()

	if !mcfg.Enabled || !mcfg.MonitorTraces {
		return nil, nil
	}

	cfg := mcfg.APM

	//nolint:godox // the TODO is intentional
	// TODO(stn): Ideally, we'd use apmtransport.NewHTTPTransportOptions()
	// but it doesn't exist today. Update this code once we have something
	// available via the APM Go agent.
	const (
		envVerifyServerCert = "ELASTIC_APM_VERIFY_SERVER_CERT"
		envServerCert       = "ELASTIC_APM_SERVER_CERT"
		envCACert           = "ELASTIC_APM_SERVER_CA_CERT_FILE"
	)
	if cfg.TLS.SkipVerify {
		os.Setenv(envVerifyServerCert, "false")
		defer os.Unsetenv(envVerifyServerCert)
	}
	if cfg.TLS.ServerCertificate != "" {
		os.Setenv(envServerCert, cfg.TLS.ServerCertificate)
		defer os.Unsetenv(envServerCert)
	}
	if cfg.TLS.ServerCA != "" {
		os.Setenv(envCACert, cfg.TLS.ServerCA)
		defer os.Unsetenv(envCACert)
	}

	ts, err := apmtransport.NewHTTPTransport()
	if err != nil {
		return nil, err
	}

	if len(cfg.Hosts) > 0 {
		hosts := make([]*url.URL, 0, len(cfg.Hosts))
		for _, host := range cfg.Hosts {
			u, err := url.Parse(host)
			if err != nil {
				return nil, fmt.Errorf("failed parsing %s: %w", host, err)
			}
			hosts = append(hosts, u)
		}
		ts.SetServerURL(hosts...)
	}
	if cfg.APIKey != "" {
		ts.SetAPIKey(cfg.APIKey)
	} else {
		ts.SetSecretToken(cfg.SecretToken)
	}

	return apm.NewTracerOptions(apm.TracerOptions{
		ServiceName:        agentName,
		ServiceVersion:     version,
		ServiceEnvironment: cfg.Environment,
		Transport:          ts,
	})
}

// encryptConfigIfNeeded encrypts fleet.yml or state.yml if fleet.enc or state.enc does not exist already.
func encryptConfigIfNeeded(log *logger.Logger) (err error) {
	log.Debug("encrypt config if needed")

	files := []struct {
		Src string
		Dst string
	}{
		{
			Src: paths.AgentStateStoreYmlFile(),
			Dst: paths.AgentStateStoreFile(),
		},
		{
			Src: paths.AgentConfigYmlFile(),
			Dst: paths.AgentConfigFile(),
		},
	}
	for _, f := range files {
		var b []byte

		// Check if .yml file modification timestamp and existence
		log.Debugf("check if the yml file %v exists", f.Src)
		ymlModTime, ymlExists, err := fileutil.GetModTimeExists(f.Src)
		if err != nil {
			log.Errorf("failed to access yml file %v: %v", f.Src, err)
			return err
		}

		if !ymlExists {
			log.Debugf("yml file %v doesn't exists, continue", f.Src)
			continue
		}

		// Check if .enc file modification timestamp and existence
		log.Debugf("check if the enc file %v exists", f.Dst)
		encModTime, encExists, err := fileutil.GetModTimeExists(f.Dst)
		if err != nil {
			log.Errorf("failed to access enc file %v: %v", f.Dst, err)
			return err
		}

		// If enc file exists and the yml file modification time is before enc file modification time then skip encryption.
		// The reasoning is that the yml was not modified since the last time it was migrated to the encrypted file.
		// The modification of the yml is possible in the cases where the agent upgrade failed and rolled back, leaving .enc file on the disk for example
		if encExists && ymlModTime.Before(encModTime) {
			log.Debugf("enc file %v already exists, and the yml was not modified after migration, yml mod time: %v, enc mod time: %v", f.Dst, ymlModTime, encModTime)
			continue
		}

		log.Debugf("read file: %v", f.Src)
		b, err = ioutil.ReadFile(f.Src)
		if err != nil {
			log.Debugf("read file: %v, err: %v", f.Src, err)
			return err
		}

		// Encrypt yml file
		log.Debugf("encrypt file %v into %v", f.Src, f.Dst)
		store := storage.NewEncryptedDiskStore(f.Dst)
		err = store.Save(bytes.NewReader(b))
		if err != nil {
			log.Debugf("failed to encrypt file: %v, err: %v", f.Dst, err)
			return err
		}
	}

	if err != nil {
		return err
	}

	// Remove state.yml file if no errors
	fp := paths.AgentStateStoreYmlFile()
	// Check if state.yml exists
	exists, err := fileutil.FileExists(fp)
	if err != nil {
		log.Warnf("failed to check if file %s exists, err: %v", fp, err)
	}
	if exists {
		if err := os.Remove(fp); err != nil {
			// Log only
			log.Warnf("failed to remove file: %s, err: %v", fp, err)
		}
	}

	// The agent can't remove fleet.yml, because it can be rolled back by the older version of the agent "watcher"
	// and pre 8.3 version needs unencrypted fleet.yml file in order to start.
	// The fleet.yml file removal is performed by the cleaner on the agent start after the .enc configuration was stable for the grace period after upgrade

	return nil
}

// startOldAgentConfigCleaner starts the cleaner that removes fleet.yml and fleet.yml.lock files after 15 mins by default
// The interval is calculated from the last modified time of fleet.enc. It's possible that the fleet.enc
// will be modified again during that time, the assumption is that at some point there will be 15 mins interval when the fleet.enc is not modified.
// The modification time is used because it's the most cross-patform compatible timestamp on the files.
// This is tied to grace period, default 10 mins, when the agent is considered "stable" after the upgrade.
// The old agent watcher doesn't know anything about configuration encryption so we have to delete the old configuration files here.
// The cleaner is only started if fleet.yml exists
func startOldAgentConfigCleaner(ctx context.Context, log *logp.Logger) {
	// Start cleaner only when fleet.yml exists
	fp := paths.AgentConfigYmlFile()
	exists, err := fileutil.FileExists(fp)
	if err != nil {
		log.Warnf("failed to check if file %s exists, err: %v", fp, err)
	}
	if !exists {
		return
	}

	c := cleaner.New(log, paths.AgentConfigFile(), []string{fp, fmt.Sprintf("%s.lock", fp)})
	go func() {
		err := c.Run(ctx)
		if err != nil {
			log.Warnf("failed running the old configuration files cleaner, err: %v", err)
		}
	}()
}
