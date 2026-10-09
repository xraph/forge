package model

import "slices"

// Supports reports whether the target can host resources of type t with
// lifecycle l.
func (c Capabilities) Supports(t ResourceType, l Lifecycle) bool {
	return slices.Contains(c.Resources[t], l)
}
