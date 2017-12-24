package hubic

import (
	"net/http"

	"github.com/restic/restic/internal/backend/swift"
	"github.com/restic/restic/internal/restic"
)

// Open opens the hubic backend at a container. The container is
// created if it does not exist yet.
func Open(cfg Config, rt http.RoundTripper) (restic.Backend, error) {
	// Translate configuration and delegate to Swift backend
	swiftCfg := swift.NewConfig()
	swiftCfg.Auth = &hubicAuthenticator{
		HubicAuthorization: cfg.HubicAuthorization,
		HubicRefreshToken:  cfg.HubicRefreshToken,
		transport:          rt,
	}
	swiftCfg.Container = cfg.Container
	swiftCfg.Prefix = cfg.Prefix
	return swift.Open(swiftCfg, rt)
}
