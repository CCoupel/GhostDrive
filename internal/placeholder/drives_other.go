//go:build !windows

package placeholder

// AvailableDriveLetters returns nil on non-Windows platforms.
// Drive letters are a Windows-only concept.
func AvailableDriveLetters() []string { return nil }

// IsLetterInUse always returns false on non-Windows platforms.
func IsLetterInUse(_ string) bool { return false }
