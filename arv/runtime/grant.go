package runtime

// Grant returns the least specific grant in s that covers r (the covering
// grant with the shortest scope, so a directory grant wins over a file grant
// inside it), and false if none does. r must already be canonical (Admit
// returns it so). The fs effects use it to confine the open to that grant's
// directory with os.Root, so a directory swapped for a symlink between the
// check and the open cannot lead outside the grant.
//
// The outermost cover is the one that confines: os.OpenRoot follows
// symlinks in the root's own path, so a narrower grant nested inside a
// wider one would put the root's directory where a writer inside the wider
// grant could swap it for a symlink to anywhere. The outermost cover's own
// path lies outside every grant of this kind that covers r. A cover of the
// other fs kind, or one that names the same directory in another spelling,
// is matched by identity in the evaluator (confiningRoot), which then opens
// from that outer directory down to this grant one checked step at a time.
func (s CapSet) Grant(r Capability) (Capability, bool) {
	var best Capability
	found := false
	for _, c := range s.caps {
		if c.Covers(r) && (!found || len(c.Scope) < len(best.Scope)) {
			best, found = c, true
		}
	}
	return best, found
}
