package forge

import (
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/di"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/router"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

type ConfigManager = common.ConfigManager

type RouterAdapter = router.RouterAdapter
type HttpRouterAdapter = router.HttpRouterAdapter

type DefaultErrorHandler = common.DefaultErrorHandler

var NewDefaultErrorHandler = common.NewDefaultErrorHandler

type Container = common.Container
type ContainerConfig = di.ContainerConfig
type Middleware = common.Middleware
type Router = common.Router
type RouteDefinition = common.RouteDefinition

func NewContainer(config ContainerConfig) Container {
	return di.NewContainer(config)
}

type Context = common.Context

type OpenAPIConfig = common.OpenAPIConfig
type AsyncAPIConfig = common.AsyncAPIConfig
type StreamMessage = streamingcore.Message
type Logger = logger.Logger
type Service = common.Service
type ServiceDefinition = common.ServiceDefinition
type Plugin = common.Plugin
type PluginMetrics = common.PluginMetrics
type Hook = common.Hook
type ConfigSchema = common.ConfigSchema
type CLICommand = common.CLICommand
type PluginDependency = common.PluginDependency
type PluginCapability = common.PluginCapability
type PluginType = common.PluginType

const (
	PluginTypeMiddleware  = common.PluginTypeMiddleware
	PluginTypeService     = common.PluginTypeService
	PluginTypeHandler     = common.PluginTypeHandler
	PluginTypeFilter      = common.PluginTypeFilter
	PluginTypeDatabase    = common.PluginTypeDatabase
	PluginTypeAuth        = common.PluginTypeAuth
	PluginTypeCache       = common.PluginTypeCache
	PluginTypeStorage     = common.PluginTypeStorage
	PluginTypeMessaging   = common.PluginTypeMessaging
	PluginTypeMonitoring  = common.PluginTypeMonitoring
	PluginTypeAI          = common.PluginTypeAI
	PluginTypeSecurity    = common.PluginTypeSecurity
	PluginTypeIntegration = common.PluginTypeIntegration
	PluginTypeUtility     = common.PluginTypeUtility
	PluginTypeExtension   = common.PluginTypeExtension
)
