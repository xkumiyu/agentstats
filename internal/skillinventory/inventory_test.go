package skillinventory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestClassifySkillManifestRecognizesSupportedLayoutsOnly(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantName      string
		wantNamespace string
		wantOK        bool
	}{
		{
			name:     "agents skill",
			path:     "/repo/.agents/skills/review/SKILL.md",
			wantName: "review",
			wantOK:   true,
		},
		{
			name:     "codex system skill",
			path:     "/home/.codex/skills/.system/openai-docs/SKILL.md",
			wantName: "openai-docs",
			wantOK:   true,
		},
		{
			name:          "plugin skill",
			path:          "/home/.codex/plugins/cache/example-plugin/data-analytics/0.2.35-build/skills/index/SKILL.md",
			wantName:      "data-analytics:index",
			wantNamespace: "data-analytics",
			wantOK:        true,
		},
		{
			name: "arbitrary skills directory",
			path: "/tmp/skills/review/SKILL.md",
		},
		{
			name: "skill nested below skill directory",
			path: "/repo/.agents/skills/review/nested/SKILL.md",
		},
		{
			name: "root alias is not a skill",
			path: "/home/.codex/skills/r0/ponytail/SKILL.md",
		},
		{
			name: "scripts directory is not a manifest",
			path: "/repo/.agents/skills/review/scripts/SKILL.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := classifySkillManifest(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("classifySkillManifest() ok = %v, want %v; candidate = %#v", ok, tt.wantOK, got)
			}
			if !tt.wantOK {
				return
			}
			if got.Name != tt.wantName || got.Namespace != tt.wantNamespace {
				t.Fatalf("classifySkillManifest() = %#v, want name=%q namespace=%q", got, tt.wantName, tt.wantNamespace)
			}
		})
	}
}

func TestResolveRootsUsesDefaultAndDeduplicatesExplicitPaths(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")
	got, err := ResolveRoots(nil, home)
	if err != nil {
		t.Fatalf("ResolveRoots(default) error = %v", err)
	}
	want := []string{filepath.Join(home, ".agents", "skills")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveRoots(default) = %#v, want %#v", got, want)
	}

	root := t.TempDir()
	got, err = ResolveRoots([]string{root, filepath.Join(root, ".", "child", "..")}, home)
	if err != nil {
		t.Fatalf("ResolveRoots(explicit) error = %v", err)
	}
	want = []string{root}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ResolveRoots(explicit) = %#v, want %#v", got, want)
	}
}

func TestResolveRootsRejectsEmptyHomeAndRoot(t *testing.T) {
	if _, err := ResolveRoots(nil, " "); err == nil {
		t.Fatal("ResolveRoots() accepted an empty user home")
	}
	if _, err := ResolveRoots([]string{"  "}, "/home/user"); err == nil {
		t.Fatal("ResolveRoots() accepted an empty explicit root")
	}
}

func TestDiscoverAllowsMissingDefaultRootButRejectsMissingExplicitRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Discover(DiscoverOptions{Roots: []string{missing}, AllowMissingRoots: true}); err != nil {
		t.Fatalf("Discover(default missing) error = %v", err)
	}
	if _, err := Discover(DiscoverOptions{Roots: []string{missing}}); err == nil {
		t.Fatal("Discover(explicit missing) returned nil error")
	}
}

func TestDiscoverRejectsExplicitNonDirectoryRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "root")
	if err := writeFile(file, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(DiscoverOptions{Roots: []string{file}}); err == nil {
		t.Fatal("Discover() accepted a file as root")
	}
}

func TestDiscoverRecursivelyFindsSupportedLayoutsAndDeduplicatesPaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	paths := []string{
		filepath.Join(repo, ".agents", "skills", "review", "SKILL.md"),
		filepath.Join(repo, ".codex", "skills", ".system", "openai-docs", "SKILL.md"),
		filepath.Join(repo, ".codex", "plugins", "cache", "example-plugin", "data-analytics", "1.0.0", "skills", "index", "SKILL.md"),
		filepath.Join(repo, "skills", "not-a-skill", "SKILL.md"),
	}
	for _, path := range paths {
		if err := writeSkillManifest(path, ""); err != nil {
			t.Fatal(err)
		}
	}

	got, err := Discover(DiscoverOptions{Roots: []string{root, repo}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := []InventoryEntry{
		{Name: "data-analytics:index", Path: filepath.Dir(paths[2]), NameSource: NameSourceDirectory},
		{Name: "openai-docs", Path: filepath.Dir(paths[1]), NameSource: NameSourceDirectory},
		{Name: "review", Path: filepath.Dir(paths[0]), NameSource: NameSourceDirectory},
	}
	if !reflect.DeepEqual(got.Entries, want) {
		t.Fatalf("Discover().Entries = %#v, want %#v", got.Entries, want)
	}
	if got.InstalledCount != len(want) {
		t.Fatalf("Discover().InstalledCount = %d, want %d", got.InstalledCount, len(want))
	}
}

func TestDiscoverDoesNotFollowSymlinkDirectories(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	targetManifest := filepath.Join(target, "SKILL.md")
	if err := writeSkillManifest(targetManifest, ""); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".agents", "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}

	got, err := Discover(DiscoverOptions{Roots: []string{root}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("Discover() followed symlink directory: %#v", got.Entries)
	}
}

func TestDiscoverUsesFrontmatterNameAndReportsMismatch(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, ".agents", "skills", "directory-name", "SKILL.md")
	if err := writeSkillManifest(manifest, "---\nname: canonical-name\n---\n"); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Discover(DiscoverOptions{Roots: []string{root}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := []InventoryEntry{{
		Name:         "canonical-name",
		Path:         filepath.Dir(manifest),
		NameSource:   NameSourceFrontmatter,
		NameMismatch: true,
	}}
	if !reflect.DeepEqual(snapshot.Entries, want) {
		t.Fatalf("Discover().Entries = %#v, want %#v", snapshot.Entries, want)
	}
}

func TestDiscoverPreservesPluginNamespaceAndFallsBackWhenFrontmatterIsInvalid(t *testing.T) {
	root := t.TempDir()
	pluginManifest := filepath.Join(root, ".codex", "plugins", "cache", "example-plugin", "data-analytics", "1.0.0", "skills", "router", "SKILL.md")
	if err := writeSkillManifest(pluginManifest, "---\nname: index\n---\n"); err != nil {
		t.Fatal(err)
	}
	invalidManifest := filepath.Join(root, ".agents", "skills", "fallback", "SKILL.md")
	if err := writeSkillManifest(invalidManifest, "---\nname: invalid/name\n---\n"); err != nil {
		t.Fatal(err)
	}
	overSizedManifest := filepath.Join(root, ".agents", "skills", "oversized", "SKILL.md")
	if err := writeSkillManifest(overSizedManifest, "name: ignored\n"+strings.Repeat("x", 128*1024)); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Discover(DiscoverOptions{Roots: []string{root}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	byPath := make(map[string]InventoryEntry, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		byPath[entry.Path] = entry
	}
	plugin := byPath[filepath.Dir(pluginManifest)]
	if plugin.Name != "data-analytics:index" || plugin.NameSource != NameSourceFrontmatter || !plugin.NameMismatch {
		t.Fatalf("plugin entry = %#v", plugin)
	}
	for _, path := range []string{filepath.Dir(invalidManifest), filepath.Dir(overSizedManifest)} {
		entry := byPath[path]
		if entry.Name != filepath.Base(path) || entry.NameSource != NameSourceDirectory || entry.NameMismatch {
			t.Fatalf("fallback entry for %q = %#v", path, entry)
		}
	}
}

func TestDiscoverReportsRecoverableChildWalkErrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read permission-restricted directories")
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	if info, err := os.Stat(blocked); err != nil || info.Mode().Perm() != 0 {
		t.Skip("filesystem does not preserve permission restriction")
	}

	got, err := Discover(DiscoverOptions{Roots: []string{root}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(got.Warnings) == 0 {
		t.Fatal("Discover() did not report a recoverable child walk error")
	}
}

func writeSkillManifest(path, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFile(path, contents)
}

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o644)
}
