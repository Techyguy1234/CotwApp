package main

import (
	"math"

	"github.com/hajimehoshi/ebiten/v2"
)

type Camera struct {
	X, Y       float64 // Camera offset in screen space
	Zoom       float64 // Scale factor (1.0 = 100%)
	isDragging bool
	dragStartX int
	dragStartY int
}

func NewCamera() *Camera {
	return &Camera{
		Zoom: 1.0,
	}
}

func (c *Camera) Update() {
	mx, my := ebiten.CursorPosition()

	// 1. Mouse Panning (Middle or Right Mouse Button)
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonMiddle) {
		if !c.isDragging {
			c.isDragging = true
			c.dragStartX = mx
			c.dragStartY = my
		} else {
			// Move camera by cursor delta
			c.X += float64(mx - c.dragStartX)
			c.Y += float64(my - c.dragStartY)
			c.dragStartX = mx
			c.dragStartY = my
		}
	} else {
		c.isDragging = false
	}

	// 2. Mouse Wheel Zooming (Centered on Cursor)
	_, wheelY := ebiten.Wheel()
	if wheelY != 0 {
		oldZoom := c.Zoom
		zoomFactor := 1.1 // 10% zoom step per scroll tick

		if wheelY > 0 {
			c.Zoom *= zoomFactor
		} else {
			c.Zoom /= zoomFactor
		}

		// Clamp zoom levels to prevent flipping or extreme scaling
		c.Zoom = math.Max(0.1, math.Min(c.Zoom, 10.0))

		// Adjust X and Y so the world point under the cursor stays stationary
		scaleRatio := c.Zoom / oldZoom
		c.X = float64(mx) - (float64(mx)-c.X)*scaleRatio
		c.Y = float64(my) - (float64(my)-c.Y)*scaleRatio
	}
}

// Matrix returns the transformation matrix for world-to-screen rendering
func (c *Camera) Matrix() ebiten.GeoM {
	m := ebiten.GeoM{}
	m.Scale(c.Zoom, c.Zoom)
	m.Translate(c.X, c.Y)
	return m
}
