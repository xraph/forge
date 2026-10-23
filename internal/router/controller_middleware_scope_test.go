package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainController is a ControllerWithMiddleware without a prefix: its routes
// land on the router it is registered with.
type plainController struct {
	mw []Middleware
	h  Handler
}

func (c *plainController) Name() string { return "plain" }

func (c *plainController) Middleware() []Middleware { return c.mw }

func (c *plainController) Routes(r Router) error { return r.GET("/ctl", c.h) }

// TestControllerMiddleware_PrefixlessDoesNotLeak covers a controller without a
// prefix. Its middleware guards its own routes, once, and never reaches a route
// registered on the same router before or after it.
func TestControllerMiddleware_PrefixlessDoesNotLeak(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			log := &callLog{}
			r := NewRouter(WithAdapter(newAdapter()))

			require.NoError(t, r.GET("/before", log.handler("h")))
			require.NoError(t, r.RegisterController(&plainController{
				mw: []Middleware{log.mw("ctl")},
				h:  log.handler("h"),
			}))
			require.NoError(t, r.GET("/after", log.handler("h")))

			cases := map[string][]string{
				"/ctl":    {"ctl", "h"},
				"/before": {"h"},
				"/after":  {"h"},
			}

			for path, want := range cases {
				log.reset()

				rec := doGet(t, r, path)
				require.Equal(t, http.StatusOK, rec.Code, "GET %s", path)
				assert.Equal(t, want, log.snapshot(), "GET %s", path)
			}
		})
	}
}

// TestControllerMiddleware_PrefixlessOnGroup registers a prefix-less controller
// on a group. The group's middleware wraps the controller's, the same order a
// prefixed controller gets, and a route added to the group afterwards runs the
// group's middleware without the controller's.
func TestControllerMiddleware_PrefixlessOnGroup(t *testing.T) {
	for name, newAdapter := range groupMiddlewareAdapters {
		t.Run(name, func(t *testing.T) {
			log := &callLog{}
			r := NewRouter(WithAdapter(newAdapter()))

			g := r.Group("/g", WithGroupMiddleware(log.mw("g")))
			g.Use(log.mw("g-use"))

			require.NoError(t, g.RegisterController(&plainController{
				mw: []Middleware{log.mw("ctl")},
				h:  log.handler("h"),
			}))
			require.NoError(t, g.GET("/after", log.handler("h")))

			rec := doGet(t, r, "/g/ctl")
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, []string{"g-use", "g", "ctl", "h"}, log.snapshot())

			log.reset()

			rec = doGet(t, r, "/g/after")
			require.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, []string{"g-use", "g", "h"}, log.snapshot())
		})
	}
}
