//go:build linux

package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	crmetadata "github.com/checkpoint-restore/checkpointctl/lib"
)

func writeTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckpointCacheHelpers(t *testing.T) {
	sourceDir := t.TempDir()
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.SpecDumpFile), "spec")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.ConfigDumpFile), "config")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.StatusDumpFile), "status")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.RootFsDiffTar), "rootfs")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.CheckpointDirectory, "pages-1.img"), "checkpoint")

	cacheDir := filepath.Join(t.TempDir(), "cache")
	if checkpointCacheReady(cacheDir) {
		t.Fatalf("cache %s unexpectedly reported ready before population", cacheDir)
	}
	if err := populateCheckpointCache(context.Background(), cacheDir, sourceDir); err != nil {
		t.Fatalf("populate checkpoint cache: %v", err)
	}
	if !checkpointCacheReady(cacheDir) {
		t.Fatalf("cache %s did not report ready after population", cacheDir)
	}

	// A second population should reuse the completed cache instead of rewriting it.
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.SpecDumpFile), "new-spec")
	if err := populateCheckpointCache(context.Background(), cacheDir, sourceDir); err != nil {
		t.Fatalf("repopulate checkpoint cache: %v", err)
	}
	specDump, err := os.ReadFile(filepath.Join(cacheDir, crmetadata.SpecDumpFile))
	if err != nil {
		t.Fatalf("read cached spec dump: %v", err)
	}
	if string(specDump) != "spec" {
		t.Fatalf("cache contents changed unexpectedly: got %q", string(specDump))
	}

	containerRootDir := t.TempDir()
	if err := symlinkCheckpointCache(cacheDir, containerRootDir); err != nil {
		t.Fatalf("symlink checkpoint cache: %v", err)
	}

	for _, name := range []string{
		crmetadata.SpecDumpFile,
		crmetadata.ConfigDumpFile,
		crmetadata.StatusDumpFile,
		crmetadata.RootFsDiffTar,
		crmetadata.CheckpointDirectory,
	} {
		path := filepath.Join(containerRootDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("lstat %s: %v", path, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s is not a symlink", path)
		}
		target, err := os.Readlink(path)
		if err != nil {
			t.Fatalf("readlink %s: %v", path, err)
		}
		if target != filepath.Join(cacheDir, name) {
			t.Fatalf("unexpected symlink target for %s: got %q", path, target)
		}
	}
}

func TestPopulateCheckpointCacheRejectsIncompleteFinalCache(t *testing.T) {
	sourceDir := t.TempDir()
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.SpecDumpFile), "spec")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.ConfigDumpFile), "config")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.StatusDumpFile), "status")
	writeTestFile(t, filepath.Join(sourceDir, crmetadata.CheckpointDirectory, "pages-1.img"), "checkpoint")

	cacheDir := filepath.Join(t.TempDir(), "cache")
	writeTestFile(t, filepath.Join(cacheDir, crmetadata.SpecDumpFile), "stale-spec")

	err := populateCheckpointCache(context.Background(), cacheDir, sourceDir)
	if err == nil {
		t.Fatalf("populate checkpoint cache unexpectedly succeeded with incomplete final cache")
	}
	if !strings.Contains(err.Error(), "exists but is incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}

	specDump, readErr := os.ReadFile(filepath.Join(cacheDir, crmetadata.SpecDumpFile))
	if readErr != nil {
		t.Fatalf("read stale spec dump: %v", readErr)
	}
	if string(specDump) != "stale-spec" {
		t.Fatalf("incomplete cache contents changed unexpectedly: got %q", string(specDump))
	}
}
