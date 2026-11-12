//go:build !arv_treewalk

package evaluator

// wantTreeWalk is the engine a plain build must run: compiled code
// (Phase 8.4, decision 5).
const wantTreeWalk = false
