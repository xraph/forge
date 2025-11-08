package forge

// Map is a convenience alias for map[string]interface{}
// Used for JSON responses and generic data structures.
//
// Example:
//
//	app.Router().GET("/api/users", func(c Context) error {
//	    return c.JSON(200, Map{
//	        "users": []string{"alice", "bob"},
//	        "count": 2,
//	        "success": true,
//	    })
//	})
type Map = map[string]any

// StringMap is a convenience alias for map[string]string
// Used for string-to-string mappings like headers, tags, or labels.
//
// Example:
//
//	app.Router().POST("/config", func(c Context) error {
//	    config := StringMap{
//	        "env": "production",
//	        "region": "us-west-2",
//	    }
//	    return c.JSON(200, config)
//	})
type StringMap = map[string]string
