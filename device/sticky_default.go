//go:build !linux

package device

import (
	"github.com/oberon-core/oberon/v1/conn"
	"github.com/oberon-core/oberon/v1/rwcancel"
)

func (device *Device) startRouteListener(_ conn.Bind) (*rwcancel.RWCancel, error) {
	return nil, nil
}
