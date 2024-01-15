/* SPDX-License-Identifier: Apache-2.0
 *
 * Copyright 2024 Damian Peckett <damian@pecke.tt>.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"log/slog"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/dpeckett/replikator/internal/controller"
	"github.com/go-logr/logr"
	"github.com/urfave/cli/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var logger *slog.Logger

	init := func(c *cli.Context) error {
		handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: (*slog.Level)(c.Generic("log-level").(*logLevelFlag)),
		})
		ctrl.SetLogger(logr.FromSlogHandler(handler))

		logger = slog.New(handler)

		return nil
	}

	app := &cli.App{
		Name:  "replikator",
		Usage: "A simple operator to replicate Kubernetes configmaps and secrets across namespaces",
		Flags: []cli.Flag{
			&cli.GenericFlag{
				Name:  "log-level",
				Usage: "Log level",
				Value: fromLogLevel(slog.LevelInfo),
			},
			&cli.StringFlag{
				Name:  "metrics-bind-address",
				Usage: "The address the metric endpoint binds to",
				Value: ":8080",
			},
			&cli.StringFlag{
				Name:  "health-probe-bind-address",
				Usage: "The address the probe endpoint binds to",
				Value: ":8081",
			},
			&cli.BoolFlag{
				Name:  "leader-elect",
				Usage: "Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager",
				Value: false,
			},
		},
		Before: init,
		Action: func(c *cli.Context) error {
			metricsAddr := c.String("metrics-bind-address")
			probeAddr := c.String("health-probe-bind-address")
			enableLeaderElection := c.Bool("leader-elect")

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                 scheme,
				Metrics:                metricsserver.Options{BindAddress: metricsAddr},
				HealthProbeBindAddress: probeAddr,
				LeaderElection:         enableLeaderElection,
				LeaderElectionID:       "767661ca.pecke.tt",
				// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
				// when the Manager ends. This requires the binary to immediately end when the
				// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
				// speeds up voluntary leader transitions as the new leader don't have to wait
				// LeaseDuration time first.
				//
				// In the default scaffold provided, the program ends immediately after
				// the manager stops, so would be fine to enable this option. However,
				// if you are doing or is intended to do any operation such as perform cleanups
				// after the manager stops then its usage might be unsafe.
				// LeaderElectionReleaseOnCancel: true,
			})
			if err != nil {
				return fmt.Errorf("unable to start manager: %w", err)
			}

			if err = (&controller.ConfigMapReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				return fmt.Errorf("unable to create controller: %w", err)
			}

			if err = (&controller.SecretReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				return fmt.Errorf("unable to create controller: %w", err)
			}

			//+kubebuilder:scaffold:builder

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				return fmt.Errorf("unable to set up health check: %w", err)
			}

			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				return fmt.Errorf("unable to set up ready check: %w", err)
			}

			logger.Info("Starting manager")

			return mgr.Start(ctrl.SetupSignalHandler())
		},
	}

	if err := app.Run(os.Args); err != nil {
		logger.Error("Problem running manager", "error", err)
		os.Exit(1)
	}
}

type logLevelFlag slog.Level

func fromLogLevel(l slog.Level) *logLevelFlag {
	f := logLevelFlag(l)
	return &f
}

func (f *logLevelFlag) Set(value string) error {
	return (*slog.Level)(f).UnmarshalText([]byte(value))
}

func (f *logLevelFlag) String() string {
	return (*slog.Level)(f).String()
}
