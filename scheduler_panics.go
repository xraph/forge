package forge

import (
	"fmt"

	"github.com/xraph/forge/internal/scheduler"
)

// installSchedulerPanicLog sends panics from forge's shared scheduler to log.
// The scheduler belongs to the process, not to one app, so the last app built
// wins, the same as the OTel globals.
func installSchedulerPanicLog(log Logger) {
	if log == nil {
		return
	}

	scheduler.SetPanicHandler(func(job string, recovered any, stack []byte) {
		log.Error("scheduler: job panicked",
			F("job", job),
			F("panic", fmt.Sprint(recovered)),
			F("stack", string(stack)),
		)
	})
}
