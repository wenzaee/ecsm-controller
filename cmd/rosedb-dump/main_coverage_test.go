package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rosedblabs/rosedb/v2"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func writeTextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunCLIExitCodesAndActions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	dbDir := filepath.Join(dir, "db")
	writeTextFile(t, configPath, "registry:\n  rosedb:\n    dir_path: "+dbDir+"\n")
	out := &bytes.Buffer{}

	if code := runCLI([]string{"-bad-flag"}, out); code != 2 {
		t.Fatalf("runCLI() with bad flag = %d, want 2", code)
	}
	if code := runCLI([]string{"-config", filepath.Join(dir, "missing.yaml")}, out); code != 1 {
		t.Fatalf("runCLI() with missing config = %d, want 1", code)
	}
	if code := runCLI([]string{"-config", ""}, out); code != 1 {
		t.Fatalf("runCLI() without rosedb dir = %d, want 1", code)
	}
	blocker := filepath.Join(dir, "blocker")
	writeTextFile(t, blocker, "block")
	if code := runCLI([]string{"-config", configPath, "-dir", blocker}, out); code != 1 {
		t.Fatalf("runCLI() with unusable dir = %d, want 1", code)
	}
	if code := runCLI([]string{"-config", configPath, "-action", "unknown"}, out); code != 1 {
		t.Fatalf("runCLI() with unknown action = %d, want 1", code)
	}

	putArgs := []string{"-config", configPath, "-action", "put", "-key", "desired/api@1.0.0", "-value", `{"replicas":1}`}
	if code := runCLI(putArgs, out); code != 0 {
		t.Fatalf("runCLI() put = %d, want 0", code)
	}
	out.Reset()
	if code := runCLI([]string{"-config", configPath, "-action", "list", "-prefix", "desired/"}, out); code != 0 {
		t.Fatalf("runCLI() list = %d, want 0", code)
	}
	if !strings.Contains(out.String(), `"count": 1`) || !strings.Contains(out.String(), "desired/api@1.0.0") {
		t.Fatalf("list output = %s", out.String())
	}
	out.Reset()
	if code := runCLI([]string{"-config", configPath, "-action", "delete", "-key", "desired/api@1.0.0"}, out); code != 0 {
		t.Fatalf("runCLI() delete = %d, want 0", code)
	}
	out.Reset()
	if code := runCLI([]string{"-config", configPath, "-action", "list"}, failingWriter{}); code != 1 {
		t.Fatalf("runCLI() with failing writer = %d, want 1", code)
	}
}

func TestRunRemainingActionBranches(t *testing.T) {
	db := testDB(t)
	if _, err := run(db, options{action: "get"}); err == nil {
		t.Fatal("get without key unexpectedly succeeded")
	}
	if _, err := run(db, options{action: "delete"}); err == nil {
		t.Fatal("delete without key unexpectedly succeeded")
	}
	if _, err := run(db, options{action: "put", key: "k"}); err == nil {
		t.Fatal("put without value unexpectedly succeeded")
	}
	if _, err := run(db, options{action: "put", key: "k", value: "v"}); err != nil {
		t.Fatal(err)
	}
	if _, err := run(db, options{action: "delete", key: "k"}); err != nil {
		t.Fatalf("delete = %v", err)
	}
}

func TestPutReadsValueFromFileAndReportsDBErrors(t *testing.T) {
	db := testDB(t)
	valueFile := filepath.Join(t.TempDir(), "value.json")
	writeTextFile(t, valueFile, `{"from":"file"}`)
	got, err := run(db, options{action: "put", key: "file-key", valueFile: valueFile})
	if err != nil || !got.OK {
		t.Fatalf("put with value-file = (%+v, %v)", got, err)
	}
	if _, err := run(db, options{action: "put", key: "x", valueFile: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("put accepted missing value file")
	}

	closed := openRawDB(t)
	_ = closed.Close()
	if _, err := run(closed, options{action: "put", key: "k", value: "v"}); err == nil {
		t.Fatal("put accepted closed database")
	}
	if _, err := run(closed, options{action: "get", key: "k"}); err == nil {
		t.Fatal("get accepted closed database")
	}
	if _, err := run(closed, options{action: "delete", key: "k"}); err == nil {
		t.Fatal("delete accepted closed database")
	}
	if err := deleteKeys(closed, []string{"k"}); err == nil {
		t.Fatal("deleteKeys accepted closed database")
	}
}

func TestClearWithPrefixFiltersKeys(t *testing.T) {
	db := testDB(t)
	for _, key := range []string{"desired/a", "status/a"} {
		if err := db.Put([]byte(key), []byte("v")); err != nil {
			t.Fatal(err)
		}
	}
	got, err := run(db, options{action: "clear", yes: true, prefix: "desired/"})
	if err != nil || got.Count != 1 {
		t.Fatalf("clear with prefix = (%+v, %v)", got, err)
	}
	listed, err := run(db, options{action: "list"})
	if err != nil || listed.Count != 1 || listed.Data[0].Key != "status/a" {
		t.Fatalf("list after clear = (%+v, %v)", listed, err)
	}

	original := deleteKeys
	deleteKeys = func(*rosedb.DB, []string) error { return errors.New("delete failed") }
	defer func() { deleteKeys = original }()
	if _, err := run(db, options{action: "clear", yes: true}); err == nil {
		t.Fatal("clear accepted failing delete")
	}
}

func TestPrintJSONErrors(t *testing.T) {
	if err := printJSON(&bytes.Buffer{}, make(chan int)); err == nil {
		t.Fatal("printJSON() accepted unmarshalable value")
	}
	if err := printJSON(failingWriter{}, result{OK: true}); err == nil {
		t.Fatal("printJSON() accepted failing writer")
	}
}

func TestFirstNonEmptyHandlesAllBlank(t *testing.T) {
	if got := firstNonEmpty("", " "); got != "" {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
}

func openRawDB(t *testing.T) *rosedb.DB {
	t.Helper()
	opts := rosedb.DefaultOptions
	opts.DirPath = t.TempDir()
	db, err := rosedb.Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMainInvokesRunCLI(t *testing.T) {
	var code int
	originalExit := osExit
	originalArgs := os.Args
	osExit = func(c int) { code = c; panic("exit") }
	os.Args = []string{"rosedb-dump", "-config", ""}
	defer func() {
		recover()
		osExit = originalExit
		os.Args = originalArgs
		if code != 1 {
			t.Fatalf("main() exit code = %d, want 1", code)
		}
	}()
	main()
}
