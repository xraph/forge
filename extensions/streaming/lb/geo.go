package lb

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/xraph/forge/errors"
)

// geoProximityBalancer routes to nearest node.
type geoProximityBalancer struct {
	nodes      map[string]*NodeInfo
	geoLocator GeoLocator
	fallback   LoadBalancer
	mu         sync.RWMutex

	nodeStore NodeStore
}

// GeoLocator determines geographic location.
type GeoLocator interface {
	// GetLocation returns lat/long for user/IP
	GetLocation(ctx context.Context, userID string, metadata map[string]any) (lat, long float64, err error)
}

// NewGeoProximityBalancer creates a geographic proximity load balancer.
func NewGeoProximityBalancer(locator GeoLocator, fallback LoadBalancer, store NodeStore) LoadBalancer {
	return &geoProximityBalancer{
		nodes:      make(map[string]*NodeInfo),
		geoLocator: locator,
		fallback:   fallback,
		nodeStore:  store,
	}
}

// SelectNode selects nearest node.
func (gpb *geoProximityBalancer) SelectNode(ctx context.Context, userID string, metadata map[string]any) (*NodeInfo, error) {
	// Get user location
	userLat, userLong, err := gpb.geoLocator.GetLocation(ctx, userID, metadata)
	if err != nil {
		// Can't determine location, use fallback
		if gpb.fallback != nil {
			return gpb.fallback.SelectNode(ctx, userID, metadata)
		}

		return nil, fmt.Errorf("failed to determine location: %w", err)
	}

	gpb.mu.RLock()
	defer gpb.mu.RUnlock()

	if len(gpb.nodes) == 0 {
		return nil, errors.New("no nodes available")
	}

	// Find nearest healthy node
	var nearest *NodeInfo

	minDistance := math.MaxFloat64

	for _, node := range gpb.nodes {
		if !node.Healthy {
			continue
		}

		distance := gpb.calculateDistance(userLat, userLong, node.Latitude, node.Longitude)
		if distance < minDistance {
			minDistance = distance
			nearest = node
		}
	}

	if nearest == nil {
		return nil, errors.New("no healthy nodes available")
	}

	return nearest, nil
}

// GetNode returns node for connection.
func (gpb *geoProximityBalancer) GetNode(ctx context.Context, connID string) (*NodeInfo, error) {
	if gpb.fallback != nil {
		return gpb.fallback.GetNode(ctx, connID)
	}

	return nil, errors.New("not implemented")
}

// RegisterNode adds node.
func (gpb *geoProximityBalancer) RegisterNode(ctx context.Context, node *NodeInfo) error {
	gpb.mu.Lock()
	defer gpb.mu.Unlock()

	gpb.nodes[node.ID] = node

	if gpb.nodeStore != nil {
		return gpb.nodeStore.Save(ctx, node)
	}

	if gpb.fallback != nil {
		return gpb.fallback.RegisterNode(ctx, node)
	}

	return nil
}

// UnregisterNode removes node.
func (gpb *geoProximityBalancer) UnregisterNode(ctx context.Context, nodeID string) error {
	gpb.mu.Lock()
	defer gpb.mu.Unlock()

	delete(gpb.nodes, nodeID)

	if gpb.nodeStore != nil {
		return gpb.nodeStore.Delete(ctx, nodeID)
	}

	if gpb.fallback != nil {
		return gpb.fallback.UnregisterNode(ctx, nodeID)
	}

	return nil
}

// Health checks node health.
func (gpb *geoProximityBalancer) Health(ctx context.Context, nodeID string) error {
	gpb.mu.RLock()
	node, ok := gpb.nodes[nodeID]
	gpb.mu.RUnlock()

	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}

	if !node.Healthy {
		return fmt.Errorf("node unhealthy: %s", nodeID)
	}

	return nil
}

// GetNodes returns all nodes.
func (gpb *geoProximityBalancer) GetNodes(ctx context.Context) ([]*NodeInfo, error) {
	gpb.mu.RLock()
	defer gpb.mu.RUnlock()

	nodes := make([]*NodeInfo, 0, len(gpb.nodes))
	for _, node := range gpb.nodes {
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// GetHealthyNodes returns healthy nodes.
func (gpb *geoProximityBalancer) GetHealthyNodes(ctx context.Context) ([]*NodeInfo, error) {
	gpb.mu.RLock()
	defer gpb.mu.RUnlock()

	nodes := make([]*NodeInfo, 0)
	for _, node := range gpb.nodes {
		if node.Healthy {
			nodes = append(nodes, node)
		}
	}

	return nodes, nil
}

// calculateDistance calculates distance between two points using Haversine formula.
func (gpb *geoProximityBalancer) calculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371 // km

	// Convert to radians
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180

	// Haversine formula
	dlat := lat2Rad - lat1Rad
	dlon := lon2Rad - lon1Rad

	a := math.Sin(dlat/2)*math.Sin(dlat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(dlon/2)*math.Sin(dlon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}

// simpleGeoLocator is a basic geo locator.
type simpleGeoLocator struct {
	locations map[string]location // userID/IP -> location
	mu        sync.RWMutex
}

type location struct {
	lat  float64
	long float64
}

// NewSimpleGeoLocator creates a simple geo locator.
func NewSimpleGeoLocator() GeoLocator {
	return &simpleGeoLocator{
		locations: make(map[string]location),
	}
}

func (sgl *simpleGeoLocator) GetLocation(ctx context.Context, userID string, metadata map[string]any) (lat, long float64, err error) {
	// Try to get from metadata first
	if metadata != nil {
		if latVal, ok := metadata["latitude"].(float64); ok {
			if longVal, ok := metadata["longitude"].(float64); ok {
				return latVal, longVal, nil
			}
		}

		// Try IP-based lookup
		if ip, ok := metadata["ip"].(string); ok {
			return sgl.lookupIP(ip)
		}
	}

	// Check cache
	sgl.mu.RLock()
	loc, ok := sgl.locations[userID]
	sgl.mu.RUnlock()

	if ok {
		return loc.lat, loc.long, nil
	}

	return 0, 0, fmt.Errorf("location not found for user: %s", userID)
}

func (sgl *simpleGeoLocator) lookupIP(ip string) (float64, float64, error) {
	// Placeholder for IP geolocation
	// In production, use a service like MaxMind GeoIP
	return 0, 0, errors.New("IP geolocation not implemented")
}

// SetLocation manually sets location for testing.
func (sgl *simpleGeoLocator) SetLocation(userID string, lat, long float64) {
	sgl.mu.Lock()
	defer sgl.mu.Unlock()

	sgl.locations[userID] = location{lat: lat, long: long}
}
