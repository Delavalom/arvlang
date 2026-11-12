package evaluator

// isProcFSName reports whether a statfs f_fstypename names a procfs that
// can expose a process's environment: FreeBSD's procfs and linprocfs
// (often at /compat/linux/proc), DragonFly's procfs (review round 2).
func isProcFSName(name string) bool { return name == "procfs" || name == "linprocfs" }

// fsTypeString is a NUL-padded f_fstypename as a string.
func fsTypeString(b []int8) string {
	s := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		s = append(s, byte(c))
	}
	return string(s)
}
