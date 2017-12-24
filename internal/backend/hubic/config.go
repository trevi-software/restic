package hubic

import (
	"os"
	"strings"

	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/options"
)

// Config contains basic configuration needed to specify swift location for a swift server
type Config struct {
	HubicRefreshToken  string
	HubicAuthorization string

	Container string
	Prefix    string

	Connections uint `option:"connections" help:"set a limit for the number of concurrent connections (default: 5)"`
}

func init() {
	options.Register("hubic", Config{})
}

// NewConfig returns a new config with the default values filled in.
func NewConfig() Config {
	return Config{
		Connections: 5,
	}
}

// ParseConfig parses the string s and extract swift's container name and prefix.
func ParseConfig(s string) (interface{}, error) {
	data := strings.SplitN(s, ":", 3)
	if len(data) != 3 {
		return nil, errors.New("invalid URL, expected: hubic:container-name:/[prefix]")
	}

	scheme, container, prefix := data[0], data[1], data[2]
	if scheme != "hubic" {
		return nil, errors.Errorf("unexpected prefix: %s", data[0])
	}

	if len(prefix) == 0 {
		return nil, errors.Errorf("prefix is empty")
	}

	if prefix[0] != '/' {
		return nil, errors.Errorf("prefix does not start with slash (/)")
	}
	prefix = prefix[1:]

	cfg := NewConfig()
	cfg.Container = container
	cfg.Prefix = prefix

	return cfg, nil
}

// ApplyEnvironment saves values from the environment to the config.
func ApplyEnvironment(prefix string, cfg interface{}) error {
	c := cfg.(*Config)
	for _, val := range []struct {
		s   *string
		env string
	}{
		{&c.HubicAuthorization, prefix + "HUBIC_AUTH"},
		{&c.HubicRefreshToken, prefix + "HUBIC_TOKEN"},
	} {
		if *val.s == "" {
			*val.s = os.Getenv(val.env)
		}
	}
	return nil
}
