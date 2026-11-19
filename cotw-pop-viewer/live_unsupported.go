//go:build !windows

package cotw

import "errors"

// The live reader uses Windows process APIs (ReadProcessMemory and
// VirtualQueryEx). Other platforms get this stub so the package still builds
// with the same public names; reading saves and game data works everywhere.

// ErrLiveUnsupported is returned on platforms without a live reader.
var ErrLiveUnsupported = errors.New("cotw: live game reading is only implemented for Windows")

// LiveGame is a placeholder on platforms without a live reader.
type LiveGame struct{}

// OpenLive always reports ErrLiveUnsupported off Windows.
func (g *Game) OpenLive() (*LiveGame, error) { return nil, ErrLiveUnsupported }

// OpenLiveWith always reports ErrLiveUnsupported off Windows.
func (g *Game) OpenLiveWith(opts Options) (*LiveGame, error) { return nil, ErrLiveUnsupported }

// Status always reports ErrLiveUnsupported off Windows.
func (l *LiveGame) Status() (LiveStatus, error) {
	return LiveStatus{ReserveIndex: -1}, ErrLiveUnsupported
}

// Positions always reports ErrLiveUnsupported off Windows.
func (l *LiveGame) Positions() ([]LiveAnimal, error) { return nil, ErrLiveUnsupported }

// Running always reports false off Windows.
func (l *LiveGame) Running() bool { return false }

// PID always reports 0 off Windows.
func (l *LiveGame) PID() int { return 0 }

// Close does nothing off Windows.
func (l *LiveGame) Close() {}
