package widgets

import (
	"video-overlay-cli/internal/layout"
	"video-overlay-cli/internal/render"
)

func gpsLockFactory(element *layout.Element, setup *render.Context) (render.Drawer, error) {
	return gpsIcon(element, setup)
}
