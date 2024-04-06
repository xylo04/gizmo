package ds

import (
	"github.com/hashicorp/go-hclog"

	"github.com/gizmo-platform/gizmo/pkg/firmware"
)

// WithGSSConfig configures this driver station to use a particular
// GSS configuration set.
func WithGSSConfig(cfg firmware.Config) Option {
	return func(d *DriverStation) { d.cfg = cfg }
}

// WithLogger configures the parent logging interface.
func WithLogger(l hclog.Logger) Option {
	return func(d *DriverStation) { d.l = l.Named("driver-station") }
}
