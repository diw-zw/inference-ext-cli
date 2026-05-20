/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	rawzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	batchv1 "k8s.io/api/batch/v1"
	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	"sigs.k8s.io/rbgs/cli/pkg/autobenchmark/config"
	abcontroller "sigs.k8s.io/rbgs/cli/pkg/autobenchmark/controller"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(workloadsv1alpha2.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
}

func main() {
	var (
		configPath string
		namespace  string
		dataDir    string
	)

	flag.StringVar(&configPath, "config", "/etc/autobenchmark/config.yaml", "Path to auto-benchmark config file")
	flag.StringVar(&namespace, "namespace", "", "Namespace for RBG and benchmark resources")
	flag.StringVar(&dataDir, "data-dir", "/data", "Base directory for experiment data (PVC mount)")

	opts := zap.Options{
		Development: false,
		EncoderConfigOptions: []zap.EncoderConfigOption{
			func(ec *zapcore.EncoderConfig) {
				ec.MessageKey = "message"
				ec.LevelKey = "level"
				ec.TimeKey = "time"
				ec.CallerKey = "caller"
				ec.EncodeLevel = zapcore.CapitalLevelEncoder
				ec.EncodeCaller = zapcore.ShortCallerEncoder
				ec.EncodeTime = zapcore.ISO8601TimeEncoder
			},
		},
		ZapOpts: []rawzap.Option{rawzap.AddCaller()},
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	zapLogger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(zapLogger)

	if namespace == "" {
		data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
		if err != nil {
			setupLog.Error(fmt.Errorf("--namespace is required or must run in-cluster"), "Namespace resolution failed")
			os.Exit(1)
		}
		namespace = strings.TrimSpace(string(data))
	}

	if err := run(configPath, namespace, dataDir, &opts); err != nil {
		setupLog.Error(err, "Auto-benchmark failed")
		os.Exit(1)
	}
}

func run(configPath, namespace, dataDir string, zapOpts *zap.Options) error {
	cfg, err := config.ParseFile(configPath)
	if err != nil {
		return fmt.Errorf("parsing config %q: %w", configPath, err)
	}

	if err := config.Validate(cfg, false); err != nil {
		return fmt.Errorf("validating config: %w", err)
	}

	expDir := filepath.Join(dataDir, cfg.Name)
	stateDir := filepath.Join(expDir, "state")
	reportDir := expDir

	// Set up file-based logging: write to both stderr and {expDir}/controller.log
	if err := os.MkdirAll(expDir, 0755); err != nil {
		return fmt.Errorf("creating experiment dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(expDir, "controller.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("creating controller log file: %w", err)
	}
	defer logFile.Close()

	zapLogger := zap.New(
		zap.UseFlagOptions(zapOpts),
		zap.WriteTo(io.MultiWriter(os.Stderr, logFile)),
	)
	ctrl.SetLogger(zapLogger)

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("getting kubeconfig: %w", err)
	}

	k8sClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	controller, err := abcontroller.NewController(cfg, k8sClient, namespace, stateDir, reportDir)
	if err != nil {
		return fmt.Errorf("creating controller: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := ctrl.Log.WithName("autobenchmark")
	ctx = log.IntoContext(ctx, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		setupLog.Info("Received signal, initiating graceful shutdown", "signal", sig.String())
		cancel()
	}()

	setupLog.Info("Starting auto-benchmark controller", "namespace", namespace)
	return controller.Run(ctx)
}
