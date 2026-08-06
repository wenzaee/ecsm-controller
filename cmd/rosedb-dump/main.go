package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/wenzaee/ecsm-controller/pkg/config"

	"github.com/rosedblabs/rosedb/v2"
)

type kvEntry struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

type result struct {
	OK      bool      `json:"ok"`
	Message string    `json:"message,omitempty"`
	Key     string    `json:"key,omitempty"`
	Count   int       `json:"count,omitempty"`
	Data    []kvEntry `json:"data,omitempty"`
	Value   any       `json:"value,omitempty"`
}

// osExit 便于测试拦截进程退出。
var osExit = os.Exit

func main() {
	osExit(runCLI(os.Args[1:], os.Stdout))
}

// runCLI 执行 RoseDB dump 命令行流程，返回进程退出码。
func runCLI(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("rosedb-dump", flag.ContinueOnError)
	configPath := flags.String("config", "configs/demo.yaml", "YAML config file path")
	dirPath := flags.String("dir", "", "RoseDB data dir, overrides config registry.rosedb.dir_path")
	action := flags.String("action", "list", "action: list, get, put, delete, clear")
	key := flags.String("key", "", "key for get, put, delete")
	value := flags.String("value", "", "value for put")
	valueFile := flags.String("value-file", "", "file path to read value for put")
	prefix := flags.String("prefix", "", "only list or clear keys with this prefix")
	raw := flags.Bool("raw", false, "print raw value strings instead of decoding JSON values")
	yes := flags.Bool("yes", false, "confirm destructive clear action")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("读取配置失败: %v", err)
		return 1
	}

	dbDir := firstNonEmpty(*dirPath, cfg.Registry.RoseDB.DirPath)
	if dbDir == "" {
		log.Print("RoseDB dir is required, set -dir or registry.rosedb.dir_path")
		return 1
	}

	opts := rosedb.DefaultOptions
	opts.DirPath = dbDir
	db, err := rosedb.Open(opts)
	if err != nil {
		log.Printf("打开 RoseDB 失败: %v", err)
		return 1
	}
	defer func() { _ = db.Close() }()

	got, err := run(db, options{
		action:    strings.ToLower(strings.TrimSpace(*action)),
		key:       strings.TrimSpace(*key),
		value:     *value,
		valueFile: strings.TrimSpace(*valueFile),
		prefix:    strings.TrimSpace(*prefix),
		raw:       *raw,
		yes:       *yes,
	})
	if err != nil {
		log.Print(err)
		return 1
	}
	if err := printJSON(out, got); err != nil {
		log.Printf("JSON 编码失败: %v", err)
		return 1
	}
	return 0
}

type options struct {
	action    string
	key       string
	value     string
	valueFile string
	prefix    string
	raw       bool
	yes       bool
}

func run(db *rosedb.DB, opts options) (result, error) {
	switch opts.action {
	case "list", "dump":
		entries := listEntries(db, opts.prefix, opts.raw)
		return result{OK: true, Count: len(entries), Data: entries}, nil
	case "get":
		return getEntry(db, opts.key, opts.raw)
	case "put", "set":
		return putEntry(db, opts)
	case "delete", "del", "rm":
		return deleteEntry(db, opts.key)
	case "clear":
		return clearEntries(db, opts)
	default:
		return result{}, fmt.Errorf("unsupported action: %s", opts.action)
	}
}

func listEntries(db *rosedb.DB, prefix string, raw bool) []kvEntry {
	var entries []kvEntry
	db.Ascend(func(key []byte, value []byte) (bool, error) {
		keyString := string(key)
		if prefix != "" && !strings.HasPrefix(keyString, prefix) {
			return true, nil
		}
		entries = append(entries, kvEntry{
			Key:   keyString,
			Value: decodeValue(value, raw),
		})
		return true, nil
	})
	return entries
}

func getEntry(db *rosedb.DB, key string, raw bool) (result, error) {
	if key == "" {
		return result{}, fmt.Errorf("key is required")
	}
	value, err := db.Get([]byte(key))
	if err != nil {
		return result{}, fmt.Errorf("get key %s: %w", key, err)
	}
	return result{OK: true, Key: key, Value: decodeValue(value, raw)}, nil
}

func putEntry(db *rosedb.DB, opts options) (result, error) {
	if opts.key == "" {
		return result{}, fmt.Errorf("key is required")
	}
	value, err := readValue(opts.value, opts.valueFile)
	if err != nil {
		return result{}, err
	}
	if err := db.Put([]byte(opts.key), value); err != nil {
		return result{}, fmt.Errorf("put key %s: %w", opts.key, err)
	}
	return result{OK: true, Message: "put success", Key: opts.key}, nil
}

func deleteEntry(db *rosedb.DB, key string) (result, error) {
	if key == "" {
		return result{}, fmt.Errorf("key is required")
	}
	if err := db.Delete([]byte(key)); err != nil {
		return result{}, fmt.Errorf("delete key %s: %w", key, err)
	}
	return result{OK: true, Message: "delete success", Key: key, Count: 1}, nil
}

func clearEntries(db *rosedb.DB, opts options) (result, error) {
	if !opts.yes {
		return result{}, errors.New("clear is destructive, pass -yes to confirm")
	}

	entries := listEntries(db, opts.prefix, true)
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.Key)
	}
	if err := deleteKeys(db, keys); err != nil {
		return result{}, err
	}
	return result{OK: true, Message: "clear success", Count: len(entries)}, nil
}

// deleteKeys 逐个删除 key，便于测试注入删除失败场景。
var deleteKeys = func(db *rosedb.DB, keys []string) error {
	for _, key := range keys {
		if err := db.Delete([]byte(key)); err != nil {
			return fmt.Errorf("delete key %s: %w", key, err)
		}
	}
	return nil
}

func readValue(value string, valueFile string) ([]byte, error) {
	if valueFile != "" {
		data, err := os.ReadFile(valueFile)
		if err != nil {
			return nil, fmt.Errorf("read value file %s: %w", valueFile, err)
		}
		return data, nil
	}
	if value == "" {
		return nil, fmt.Errorf("value or value-file is required")
	}
	return []byte(value), nil
}

func decodeValue(value []byte, raw bool) any {
	if raw {
		return string(value)
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return string(value)
	}
	return decoded
}

func printJSON(out io.Writer, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(payload))
	return err
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
