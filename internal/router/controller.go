package router

// Controller organizes related routes.
type Controller interface {
	// Name returns the controller identifier
	Name() string

	// Routes registers routes on the router
	Routes(r Router) error
}

// ControllerWithPrefix sets a path prefix for all routes.
type ControllerWithPrefix interface {
	Controller
	Prefix() string
}

// ControllerWithMiddleware applies middleware to all routes.
type ControllerWithMiddleware interface {
	Controller
	Middleware() []Middleware
}

// ControllerWithDependencies declares dependencies for ordering.
type ControllerWithDependencies interface {
	Controller
	Dependencies() []string
}

// ControllerWithTags adds metadata tags to all routes.
type ControllerWithTags interface {
	Controller
	Tags() []string
}
