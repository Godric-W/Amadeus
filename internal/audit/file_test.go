package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestJSONLFileCreatesSecureAppendOnlyLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "audit", "agent.jsonl")
	for index := 0; index < 2; index++ {
		file, err := OpenJSONLFile(path)
		if err != nil {
			t.Fatalf("open audit JSONL file: %v", err)
		}
		record := validAuditRecord()
		record.RequestID = []string{"request-1", "request-2"}[index]
		if err := file.Write(context.Background(), record); err != nil {
			t.Fatalf("write audit JSONL file: %v", err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("close audit JSONL file: %v", err)
		}
		if err := file.Write(context.Background(), record); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("unexpected write-after-close error: %v", err)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat audit JSONL file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected audit file permissions: %o", info.Mode().Perm())
	}
	opened, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit file for query: %v", err)
	}
	defer opened.Close()
	scanner := bufio.NewScanner(opened)
	var requestIDs []string
	for scanner.Scan() {
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode audit file line: %v", err)
		}
		requestIDs = append(requestIDs, record.RequestID)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan audit file: %v", err)
	}
	if len(requestIDs) != 2 || requestIDs[0] != "request-1" || requestIDs[1] != "request-2" {
		t.Fatalf("audit file did not append stable records: %v", requestIDs)
	}
}

func TestJSONLFileRejectsUnsafePaths(t *testing.T) {
	if file, err := OpenJSONLFile(" "); err == nil || file != nil {
		t.Fatalf("unexpected empty path result: file=%#v err=%v", file, err)
	}
	directory := t.TempDir()
	if file, err := OpenJSONLFile(directory); err == nil || file != nil {
		t.Fatalf("unexpected directory path result: file=%#v err=%v", file, err)
	}
	target := filepath.Join(t.TempDir(), "target.jsonl")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	link := filepath.Join(t.TempDir(), "audit.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create audit symlink: %v", err)
	}
	if file, err := OpenJSONLFile(link); err == nil || file != nil {
		t.Fatalf("unexpected symlink path result: file=%#v err=%v", file, err)
	}

	var file *JSONLFile
	if err := file.Write(context.Background(), validAuditRecord()); err == nil {
		t.Fatal("nil audit file did not fail")
	}
	if err := file.Close(); err != nil {
		t.Fatalf("nil audit file close failed: %v", err)
	}
}
