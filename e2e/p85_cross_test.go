package e2e

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every release target of .goreleaser.yaml builds, as goreleaser builds it,
// into a binary for that platform: the right object format and CPU, the
// version and commit goreleaser passes at link time (main.version,
// main.commit), stripped (-s -w), with no path of the machine that built
// it (-trimpath), CGO off, and on Linux statically linked, so the archive
// runs on any Linux of that architecture.
func TestP85ReleaseTargets(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, goos := range []string{"darwin", "linux"} {
		for _, goarch := range []string{"amd64", "arm64"} {
			t.Run(goos+"-"+goarch, func(t *testing.T) {
				t.Parallel()
				bin := filepath.Join(t.TempDir(), "arv")
				releaseBuild(t, bin, "GOOS="+goos, "GOARCH="+goarch)
				b, err := os.ReadFile(bin)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range []string{"v9.8.7", releaseSHA} {
					if !bytes.Contains(b, []byte(want)) {
						t.Errorf("the binary lacks the link-time %q", want)
					}
				}
				if bytes.Contains(b, []byte(root)) {
					t.Errorf("the binary holds the checkout's path %s: built without -trimpath", root)
				}
				checkReleaseInfo(t, bin, goos, goarch)
				if goos == "linux" {
					checkELF(t, bin, goarch)
				} else {
					checkMachO(t, bin, goarch)
				}
			})
		}
	}
}

// checkReleaseInfo checks the build information the go command recorded.
func checkReleaseInfo(t *testing.T, bin, goos, goarch string) {
	t.Helper()
	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Path != cmdArv {
		t.Errorf("build info names package %q, want %s", info.Path, cmdArv)
	}
	want := map[string]string{"GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0", "-trimpath": "true"}
	for _, s := range info.Settings {
		if v, ok := want[s.Key]; ok {
			if s.Value != v {
				t.Errorf("build setting %s=%s, want %s", s.Key, s.Value, v)
			}
			delete(want, s.Key)
		}
	}
	if len(want) != 0 {
		t.Errorf("build info lacks the settings %v", want)
	}
}

// ownSymbol reports whether name is one of arv's own Go symbols, which a
// stripped binary does not list (it keeps only the dynamic imports).
func ownSymbol(name string) bool {
	name = strings.TrimPrefix(name, "_")
	return strings.HasPrefix(name, "main.") || strings.HasPrefix(name, modulePath)
}

func checkELF(t *testing.T, bin, goarch string) {
	t.Helper()
	f, err := elf.Open(bin)
	if err != nil {
		t.Fatalf("not an ELF file: %v", err)
	}
	defer f.Close()
	machine := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[goarch]
	if f.Machine != machine || f.Type != elf.ET_EXEC {
		t.Errorf("ELF %s %s, want an %s executable", f.Machine, f.Type, machine)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP || p.Type == elf.PT_DYNAMIC {
			t.Errorf("ELF has a %s segment: dynamically linked", p.Type)
		}
	}
	if libs, _ := f.ImportedLibraries(); len(libs) != 0 {
		t.Errorf("ELF needs the libraries %v", libs)
	}
	if syms, err := f.Symbols(); err == nil {
		for _, s := range syms {
			if ownSymbol(s.Name) {
				t.Errorf("ELF lists the symbol %s: not stripped", s.Name)
				break
			}
		}
	}
	if _, err := f.DWARF(); err == nil {
		t.Error("ELF has DWARF debug information: built without -w")
	}
}

func checkMachO(t *testing.T, bin, goarch string) {
	t.Helper()
	f, err := macho.Open(bin)
	if err != nil {
		t.Fatalf("not a Mach-O file: %v", err)
	}
	defer f.Close()
	cpu := map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64}[goarch]
	if f.Cpu != cpu || f.Type != macho.TypeExec {
		t.Errorf("Mach-O %s %s, want an %s executable", f.Cpu, f.Type, cpu)
	}
	if f.Symtab != nil {
		for _, s := range f.Symtab.Syms {
			if ownSymbol(s.Name) {
				t.Errorf("Mach-O lists the symbol %s: not stripped", s.Name)
				break
			}
		}
	}
	if _, err := f.DWARF(); err == nil {
		t.Error("Mach-O has DWARF debug information: built without -w")
	}
}
