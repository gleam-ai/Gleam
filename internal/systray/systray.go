package systray

import (
	"errors"
	"sync"
)

type MenuItem struct {
	Title    string
	OnClick  func()
	Disabled bool
	SepAfter bool
}

type Config struct {
	Tooltip    string
	IconBytes  []byte
	OnActivate func()
	MenuItems  []MenuItem
}

type Runner struct {
	mu     sync.Mutex
	cfg    Config
	quit   chan struct{}
	closed bool
}

var ErrNotImplemented = errors.New("systray: not implemented on this platform")

func Run(cfg Config) error {
	return runPlatform(cfg)
}

func Quit() { quitPlatform() }

func SetTooltip(tip string) { setTooltipPlatform(tip) }

func SetIcon(ico []byte) { setIconPlatform(ico) }

func UpdateMenu(items []MenuItem) { updateMenuPlatform(items) }
