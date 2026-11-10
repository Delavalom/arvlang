package runtime

// Grant returns the most specific grant in s that covers r (the covering
// grant with the longest scope), and false if none does. r must already be
// canonical (Admit returns it so). The fs effects use it to confine the
// open to that grant's directory with os.Root, so a directory swapped for a
// symlink between the check and the open cannot lead outside the grant.
func (s CapSet) Grant(r Capability) (Capability, bool) {
	var best Capability
	found := false
	for _, c := range s.caps {
		if c.Covers(r) && (!found || len(c.Scope) > len(best.Scope)) {
			best, found = c, true
		}
	}
	return best, found
}
