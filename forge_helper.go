package forge

import (
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/metrics"
)

type DIKeys struct {
	Container     string
	Logger        string
	Metrics       string
	Config        string
	Health        string
	HealthService string
	Database      string
	EventBus      string
	Middleware    string
	Streaming     string
}

var ContainerKeys = DIKeys{
	Config:        common.ConfigKey,
	Logger:        common.LoggerKey,
	Metrics:       metrics.MetricsKey,
	Health:        common.HealthCheckerKey,
	HealthService: common.HealthServiceKey,
	Container:     common.ContainerKey,
}
