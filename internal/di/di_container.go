package di

// newContainer creates a new container implementation.
func newContainer() Container {
	return newContainerImpl()
}
