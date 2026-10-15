package profiling

import (
	"fmt"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// sdkLogger adapts forge's logger to pyroscope.Logger. A healthy profiler is
// silent; SDK errors go to warn at most once a minute, because the SDK
// retries every upload interval while Pyroscope is down. The SDK reports
// more than failed uploads (a full upload queue, a CPU profiler it couldn't
// start), so the message stays neutral and the detail field says which.
type sdkLogger struct {
	log     logger.Logger
	limiter *logger.RateLimited
}

func newSDKLogger(log logger.Logger) *sdkLogger {
	return &sdkLogger{log: log, limiter: logger.NewRateLimited(log, "profiling: pyroscope client error", time.Minute)}
}

func (l *sdkLogger) Infof(format string, args ...any)  { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *sdkLogger) Debugf(format string, args ...any) { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *sdkLogger) Errorf(format string, args ...any) {
	l.limiter.Report(fmt.Sprintf(format, args...))
}
