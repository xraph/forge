package profiling

import (
	"fmt"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// sdkLogger adapts forge's logger to pyroscope.Logger. A healthy profiler is
// silent; upload errors go to warn at most once a minute, because the SDK
// retries every upload interval while Pyroscope is down.
type sdkLogger struct {
	log     logger.Logger
	limiter *logger.RateLimited
}

func newSDKLogger(log logger.Logger) *sdkLogger {
	return &sdkLogger{log: log, limiter: logger.NewRateLimited(log, "profiling: upload failed", time.Minute)}
}

func (l *sdkLogger) Infof(format string, args ...any)  { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *sdkLogger) Debugf(format string, args ...any) { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *sdkLogger) Errorf(format string, args ...any) {
	l.limiter.Report(fmt.Sprintf(format, args...))
}
