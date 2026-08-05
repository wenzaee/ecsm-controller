package main

import (
	"testing"

	"github.com/rosedblabs/rosedb/v2"
)

func testDB(t *testing.T) *rosedb.DB {
	t.Helper()
	opts := rosedb.DefaultOptions
	opts.DirPath = t.TempDir()
	db, err := rosedb.Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRunCRUDAndClear(t *testing.T) {
	db := testDB(t)
	if _, err := run(db, options{action: "put", key: "a", value: `{"count":1}`}); err != nil {
		t.Fatal(err)
	}
	got, err := run(db, options{action: "get", key: "a"})
	if err != nil || !got.OK {
		t.Fatalf("get = (%+v, %v)", got, err)
	}
	listed, err := run(db, options{action: "list"})
	if err != nil || listed.Count != 1 {
		t.Fatalf("list = (%+v, %v)", listed, err)
	}
	if _, err := run(db, options{action: "clear", yes: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := run(db, options{action: "get", key: "a"}); err == nil {
		t.Fatal("get after clear unexpectedly succeeded")
	}
}

func TestDumpHelpersRejectInvalidInput(t *testing.T) {
	db := testDB(t)
	if _, err := run(db, options{action: "unknown"}); err == nil {
		t.Fatal("run() accepted unknown action")
	}
	if _, err := run(db, options{action: "put"}); err == nil {
		t.Fatal("put without key unexpectedly succeeded")
	}
	if _, err := run(db, options{action: "clear"}); err == nil {
		t.Fatal("clear without confirmation unexpectedly succeeded")
	}
	if value := decodeValue([]byte("plain"), false); value != "plain" {
		t.Fatalf("decodeValue() = %#v", value)
	}
	if got := firstNonEmpty("", " value "); got != "value" {
		t.Fatalf("firstNonEmpty() = %q", got)
	}
}
