package forge

import (
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/di"
	"github.com/xraph/forge/pkg/logger"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

type DefaultErrorHandler = common.DefaultErrorHandler

var NewDefaultErrorHandler = common.NewDefaultErrorHandler

type Container = common.Container

var NewContainer = di.NewContainer

type Context = common.Context

type OpenAPIConfig = common.OpenAPIConfig
type AsyncAPIConfig = common.AsyncAPIConfig
type StreamMessage = streamingcore.Message
type Logger = logger.Logger
