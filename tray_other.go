//go:build !windows

package main

import "github.com/CCoupel/GhostDrive/internal/app"

// runSystray is a no-op on non-Windows platforms.
// The notification-area icon is Windows-only in v0.2.0.
func runSystray(_ *app.App) {}
