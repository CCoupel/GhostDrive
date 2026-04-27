//go:build windows

package placeholder

import "golang.org/x/sys/windows"

// AvailableDriveLetters returns the list of unused Windows drive letters (A–Z)
// in "X:" format, using GetLogicalDrives() to determine which are occupied.
func AvailableDriveLetters() []string {
	mask, _ := windows.GetLogicalDrives()
	avail := make([]string, 0, 26)
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			avail = append(avail, string(rune('A'+i))+":")
		}
	}
	return avail
}

// IsLetterInUse reports whether the given drive letter (e.g. "G:" or "G") is
// already assigned to a local or network drive.
func IsLetterInUse(driveLetter string) bool {
	if len(driveLetter) == 0 {
		return false
	}
	c := driveLetter[0]
	if c >= 'a' && c <= 'z' {
		c -= 32 // to upper
	}
	if c < 'A' || c > 'Z' {
		return false
	}
	mask, _ := windows.GetLogicalDrives()
	return mask&(1<<uint(c-'A')) != 0
}
