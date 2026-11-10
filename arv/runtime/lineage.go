package runtime

// LineageCaps returns the grants of t and of every task that spawned it:
// t's own first, then its parent's, up to the root's. It is not authority:
// checks use t.Caps alone. It tells the fs effects where a writer could
// tamper with the tree. A spawned task's grant may be nested inside a
// grant only an ancestor holds, and a writer inside that one can swap a
// directory on the way to the task's grant, so the open must start from
// the outermost such directory, as it does for one task holding both
// (review round 3). Attenuation only narrows, so every grant listed is
// covered by one of the root's.
func (t *Task) LineageCaps() []Capability {
	var out []Capability
	for p := t; p != nil; p = p.parent {
		out = append(out, p.Caps.caps...)
	}
	return out
}
