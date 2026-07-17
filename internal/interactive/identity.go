package interactive

func ProcessIdentityMatches(pid int, expected string) bool {
	if expected == "" {
		return false
	}
	actual, err := ReadProcessIdentity(pid)
	return err == nil && actual == expected
}
