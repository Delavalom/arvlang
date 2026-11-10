package e2e

import (
	"path/filepath"
	"testing"
)

// Phase 8.3 moved read_file and write_file onto os.Root. os.Root refuses a
// symlink that escapes the root, and every absolute symlink, but Canonical
// resolves symlinks before the check and the open uses the resolved path,
// so a tree that does not change keeps every pre-8.3 answer. These cases
// pin that (no e2e expectation flips), through the shapes the new open
// path handles: links inside the grant, nested grants of either kind, a
// spawned child's narrower grant, lone file grants, and a grant reached
// through a symlink.

// p83Tree builds:
//
//	g/a/f       "inside"
//	g/abs  -> ROOT/g/a   absolute link inside the grant
//	g/rel  -> a          relative link inside the grant
//	g/esc  -> ROOT/out   link out of the grant
//	out/s       "SECRET"
//	lg     -> g          a link to the grant directory
func p83Tree(t *testing.T) string {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "g", "a", "f"), "inside")
	mustWrite(t, filepath.Join(root, "out", "s"), "SECRET")
	mustSymlink(t, filepath.Join(root, "g", "a"), filepath.Join(root, "g", "abs"))
	mustSymlink(t, "a", filepath.Join(root, "g", "rel"))
	mustSymlink(t, filepath.Join(root, "out"), filepath.Join(root, "g", "esc"))
	mustSymlink(t, filepath.Join(root, "g"), filepath.Join(root, "lg"))
	return root
}

func TestP83FSReadsUnchanged(t *testing.T) {
	t.Parallel()
	root := p83Tree(t)
	p := func(rel string) string { return filepath.Join(root, rel) }
	read := func(name, path string, allow []string, ok bool) spec {
		s := spec{
			name: name, script: "read_file.arv", dir: root, allow: allow,
			vars: map[string]string{"PATH": path}, stdoutNot: []string{"SECRET"},
		}
		if ok {
			s.stdout = "inside\n"
		} else {
			s.code, s.stderrHas = 1, []string{"capability denied: fs_read("}
		}
		return s
	}
	g := "fs_read=" + p("g")
	runAll(t, []spec{
		read("an absolute link inside the grant", p("g/abs/f"), []string{g}, true),
		read("a relative link inside the grant", p("g/rel/f"), []string{g}, true),
		read("a link out of the grant is denied", p("g/esc/s"), []string{g}, false),
		read("nested grants of one kind", p("g/a/f"), []string{g, "fs_read=" + p("g/a")}, true),
		read("a read grant inside a write grant", p("g/a/f"), []string{"fs_write=" + p("g"), "fs_read=" + p("g/a")}, true),
		read("a lone file grant", p("g/a/f"), []string{"fs_read=" + p("g/a/f")}, true),
		read("a file grant reached through a link", p("g/abs/f"), []string{"fs_read=" + p("g/rel/f")}, true),
		read("a grant spelled through a link", p("g/a/f"), []string{"fs_read=" + p("lg")}, true),
		read("a request spelled through a link to the grant", p("lg/rel/f"), []string{g}, true),
	})
}

func TestP83FSSpawnAndWrite(t *testing.T) {
	t.Parallel()
	root := p83Tree(t)
	p := func(rel string) string { return filepath.Join(root, rel) }
	child := func(name, childGrant, path string, ok bool) spec {
		s := spec{
			name: name, script: "p83_spawn_read.arv", dir: root, allow: []string{"fs_read=" + p("g")},
			vars: map[string]string{"PATH": path, "CHILD": childGrant}, stdoutNot: []string{"SECRET"},
		}
		if ok {
			s.stdout = "inside\n"
		} else {
			s.code, s.stderrHas = 1, []string{"capability denied: fs_read("}
		}
		return s
	}
	write := func(name, path string, allow []string, file string) spec {
		return spec{
			name: name, script: "write_file.arv", dir: root, allow: allow,
			vars: map[string]string{"PATH": path, "TEXT": "new"}, stdout: "ok\n",
			verify: func(t *testing.T) { assertFile(t, p(file), "new") },
		}
	}
	runAll(t, []spec{
		child("a child grant nested in the parent's reads", p("g/a"), p("g/a/f"), true),
		child("a child grant through an inner link reads", p("g/abs"), p("g/rel/f"), true),
		child("a child grant does not reach a sibling", p("g/a"), p("g/esc/s"), false),
		child("a child cannot ask for more than the parent", p("out"), p("out/s"), false),
		write("a new file under nested grants of either kind", p("g/a/n1"),
			[]string{"fs_read=" + p("g"), "fs_write=" + p("g/a")}, "g/a/n1"),
		write("a new file through an absolute link inside the grant", p("g/abs/n2"),
			[]string{"fs_write=" + p("g")}, "g/a/n2"),
		write("a lone grant for a missing file creates it", p("g/a/n3"),
			[]string{"fs_write=" + p("g/a/n3")}, "g/a/n3"),
	})
}
