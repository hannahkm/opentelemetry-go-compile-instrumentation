// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package vault

import (
	"context"
	"runtime/debug"
	"sync"

	"github.com/hashicorp/vault/api"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/inst"
	"github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/shared"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/open-telemetry/opentelemetry-go-compile-instrumentation/pkg/instrumentation/vault"
	instrumentationKey  = "VAULT"
)

var (
	logger   = shared.Logger()
	tracer   trace.Tracer
	initOnce sync.Once
)

// dbClientEnabler controls whether client instrumentation is enabled
type dbClientEnabler struct{}

func (n dbClientEnabler) Enable() bool {
	return shared.Instrumented(instrumentationKey)
}

var clientEnabler = dbClientEnabler{}

func AfterNewClient(ictx inst.HookContext, client *api.Client, err error) {
	if !clientEnabler.Enable() {
		return
	}
}

// moduleVersion extracts the version from the Go module system.
// Falls back to "dev" if version cannot be determined.
func moduleVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	// Return the main module version
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}

	return "dev"
}

func instrumentStart(
	ictx inst.HookContext,
	ctx context.Context,
	spanName, query, endpoint, driverName, dsn, dbName string,
	args ...interface{},
) {
	if !clientEnabler.Enable() {
		logger.Debug("Db client instrumentation disabled")
		return
	}
	initInstrumentation()

	// TODO: start span, store data, etc
}

func instrumentEnd(ictx inst.HookContext, err error) {
	if !clientEnabler.Enable() {
		logger.Debug("Db client instrumentation disabled")
		return
	}
	if ictx.GetData() == nil {
		return
	}
	span, ok := ictx.GetKeyData("span").(trace.Span)
	if !ok || span == nil {
		logger.Debug("instrumentEnd: no span from before hook")
		return
	}
	defer span.End()
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
}

func initInstrumentation() {
	initOnce.Do(func() {
		version := moduleVersion()
		if err := shared.SetupOTelSDK("go.opentelemetry.io/compile-instrumentation/databasesql", version); err != nil {
			logger.Error("failed to setup OTel SDK", "error", err)
		}
		tracer = otel.GetTracerProvider().Tracer(
			instrumentationName,
			trace.WithInstrumentationVersion(version),
		)

		// Start runtime metrics (respects OTEL_GO_ENABLED/DISABLED_INSTRUMENTATIONS)
		if err := shared.StartRuntimeMetrics(); err != nil {
			logger.Error("failed to start runtime metrics", "error", err)
		}

		logger.Info("DB client instrumentation initialized")
	})
}
