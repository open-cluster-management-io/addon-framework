package assets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyYaml(t *testing.T) {
	dir := t.TempDir()

	cases := []struct {
		name     string
		fileName string
		expected bool
	}{
		{name: "yaml extension", fileName: "manifest.yaml", expected: true},
		{name: "yml extension", fileName: "manifest.yml", expected: true},
		{name: "json extension", fileName: "manifest.json", expected: false},
		{name: "no extension", fileName: "README", expected: false},
	}

	for _, c := range cases {
		p := filepath.Join(dir, c.name+"-"+c.fileName)
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatalf("Name %s : failed to write file: %v", c.name, err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("Name %s : failed to stat file: %v", c.name, err)
		}
		r := OnlyYaml(info)
		if r != c.expected {
			t.Errorf("Name %s : expected %t, but got %t", c.name, c.expected, r)
		}
	}
}

func TestLoadFilesRecursively(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatalf("failed to create nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "top.yaml"), []byte("top: true"), 0644); err != nil {
		t.Fatalf("failed to write top.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "child.yaml"), []byte("child: true"), 0644); err != nil {
		t.Fatalf("failed to write child.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore me"), 0644); err != nil {
		t.Fatalf("failed to write notes.txt: %v", err)
	}

	files, err := LoadFilesRecursively(dir, OnlyYaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
	if string(files["top.yaml"]) != "top: true" {
		t.Errorf("expected top.yaml content %q, got %q", "top: true", string(files["top.yaml"]))
	}
	if string(files[filepath.Join("nested", "child.yaml")]) != "child: true" {
		t.Errorf("expected nested/child.yaml content %q, got %q", "child: true", string(files[filepath.Join("nested", "child.yaml")]))
	}
	if _, ok := files["notes.txt"]; ok {
		t.Errorf("expected notes.txt to be filtered out")
	}

	_, err = LoadFilesRecursively(filepath.Join(dir, "does-not-exist"))
	if err == nil {
		t.Errorf("expected error for missing directory, got none")
	}
}

func TestNew(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cm.yaml"), []byte("name: {{ .Name }}"), 0644); err != nil {
		t.Fatalf("failed to write cm.yaml: %v", err)
	}

	as, err := New(dir, struct{ Name string }{Name: "myaddon"}, OnlyYaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(as) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(as))
	}
	if as[0].Name != "cm.yaml" {
		t.Errorf("expected asset name %q, got %q", "cm.yaml", as[0].Name)
	}
	if string(as[0].Data) != "name: myaddon" {
		t.Errorf("expected rendered data %q, got %q", "name: myaddon", string(as[0].Data))
	}

	if _, err := New(dir, struct{ Name string }{}, func(os.FileInfo) bool { return false }); err != nil {
		t.Fatalf("unexpected error with no matching files: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{{ .Missing }}"), 0644); err != nil {
		t.Fatalf("failed to write bad.yaml: %v", err)
	}
	if _, err := New(dir, struct{ Name string }{Name: "myaddon"}, OnlyYaml); err == nil {
		t.Errorf("expected error for template referencing missing field")
	}
}

func TestMustCreateAssetFromTemplate(t *testing.T) {
	a := MustCreateAssetFromTemplate("test.yaml", []byte("value: {{ .Value }}"), struct{ Value string }{Value: "42"})
	if a.Name != "test.yaml" {
		t.Errorf("expected name %q, got %q", "test.yaml", a.Name)
	}
	if string(a.Data) != "value: 42" {
		t.Errorf("expected data %q, got %q", "value: 42", string(a.Data))
	}

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic for malformed template, got none")
		}
	}()
	MustCreateAssetFromTemplate("bad.yaml", []byte("{{ .Broken"), nil)
}

func TestAssetWriteFile(t *testing.T) {
	dir := t.TempDir()

	a := Asset{Name: "sub/config.yaml", Data: []byte("key: value")}
	if err := a.WriteFile(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	written, err := os.ReadFile(filepath.Join(dir, "sub", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	if string(written) != "key: value" {
		t.Errorf("expected written content %q, got %q", "key: value", string(written))
	}

	info, err := os.Stat(filepath.Join(dir, "sub", "config.yaml"))
	if err != nil {
		t.Fatalf("failed to stat written file: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(PermissionFileDefault) {
		t.Errorf("expected default permission %v, got %v", os.FileMode(PermissionFileDefault), info.Mode().Perm())
	}

	restricted := Asset{Name: "secret.yaml", Data: []byte("token: abc"), FilePermission: PermissionFileRestricted}
	if err := restricted.WriteFile(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err = os.Stat(filepath.Join(dir, "secret.yaml"))
	if err != nil {
		t.Fatalf("failed to stat restricted file: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(PermissionFileRestricted) {
		t.Errorf("expected restricted permission %v, got %v", os.FileMode(PermissionFileRestricted), info.Mode().Perm())
	}
}

func TestAssetsWriteFiles(t *testing.T) {
	dir := t.TempDir()

	as := Assets{
		{Name: "one.yaml", Data: []byte("one")},
		{Name: "two.yaml", Data: []byte("two")},
	}

	if err := as.WriteFiles(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"one.yaml", "two.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}
