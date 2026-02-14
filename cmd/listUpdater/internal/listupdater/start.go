package listupdater

// Start starts background refresh and the HTTP server.
func Start(cfg Config) {
	startListUpdater(cfg)
}
