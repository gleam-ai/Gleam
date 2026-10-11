package desktop

func OpenWindow(url string, onClose func()) bool {
	return openWindowPlatform(url, onClose)
}

func CloseWindow() { closeWindowPlatform() }

func WindowOpen() bool { return windowOpenPlatform() }

func OpenURL(rawURL string) error { return openURLPlatform(rawURL) }
