package plugin

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestIsLocalPath(t *testing.T) {
	if !IsLocalPath("./x") || IsLocalPath("mem0") {
		t.Fatal("IsLocalPath")
	}
}

func TestRejectLocalSource(t *testing.T) {
	if RejectLocalSource("./x") == nil {
		t.Fatal("expected reject")
	}
	if RejectLocalSource("mem0") != nil {
		t.Fatal("hub ok")
	}
}

func TestInstallFromLocal(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte(`{"id":"demo","name":"Demo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := InstallFromLocal(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "demo" {
		t.Fatalf("%+v", res)
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallFromZipRoot(t *testing.T) {
	data := makeZip(t, map[string]string{
		"plugin.json": `{"id":"zipdemo","name":"Z"}`,
		"a.py":        "x",
	})
	dst := t.TempDir()
	res, err := InstallFromZip(data, dst)
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "zipdemo" {
		t.Fatal(res.ID)
	}
}

func TestInstallFromZipWrapped(t *testing.T) {
	data := makeZip(t, map[string]string{
		"p/plugin.json": `{"id":"wrapped"}`,
	})
	res, err := InstallFromZip(data, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != "wrapped" {
		t.Fatal(res.ID)
	}
}

func TestInstallFromZipMissingManifest(t *testing.T) {
	data := makeZip(t, map[string]string{"readme": "x"})
	if _, err := InstallFromZip(data, t.TempDir()); err == nil {
		t.Fatal("expected err")
	}
}

func TestInstallFromZipMissingID(t *testing.T) {
	data := makeZip(t, map[string]string{"plugin.json": `{"name":"n"}`})
	if _, err := InstallFromZip(data, t.TempDir()); err == nil {
		t.Fatal("expected err")
	}
}
