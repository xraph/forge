package forge

import (
	"errors"

	"github.com/xraph/confy"
)

// bindConfig binds key into target. Values that can't be converted are logged,
// one warning per key, and left at whatever target held. It returns the keys it
// ignored so callers that overlay by key presence can skip them. A manager that
// can't report them (a mock) falls back to Bind, which logs on its own logger.
func bindConfig(cm ConfigManager, key string, target any, log Logger) (map[string]bool, error) {
	strict, ok := cm.(confy.StrictBinder)
	if !ok {
		return nil, cm.Bind(key, target)
	}

	err := strict.BindStrict(key, target)

	var bindErr *confy.BindError
	if !errors.As(err, &bindErr) {
		return nil, err
	}

	bad := make(map[string]bool, len(bindErr.Fields))

	for _, f := range bindErr.Fields {
		bad[f.Key] = true

		if log != nil {
			log.Warn("config: ignoring invalid value, using the default",
				F("key", f.Key),
				F("value", f.Value),
				F("expected", f.Type),
			)
		}
	}

	return bad, nil
}
