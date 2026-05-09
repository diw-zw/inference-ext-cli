/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsSafeRelativePath_ValidNames tests that valid names are accepted.
func TestIsSafeRelativePath_ValidNames(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	// Create server to get absolute dataDir
	server, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	tests := []struct {
		name    string
		subpath string
		wantOk  bool
	}{
		{"simple name", "experiment1", true},
		{"name with hyphen", "my-experiment", true},
		{"name with underscore", "my_experiment", true},
		{"name with dots", "experiment.v1.0", true},
		{"alphanumeric", "exp123", true},
		{"empty string", "", false},
		{"single dot", ".", false},
		{"double dot", "..", false},
		{"path traversal", "../outside", false},
		{"absolute path", "/etc/passwd", false},
		{"path separator", "subdir/file", false},
		{"backslash", "subdir\\file", false},
		{"parent reference", "exp/../file", false},
		{"special chars", "exp@#$%", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := isSafeRelativePath(server.dataDir, tt.subpath)
			if ok != tt.wantOk {
				t.Errorf("isSafeRelativePath(%q) = %v, want %v", tt.subpath, ok, tt.wantOk)
			}
		})
	}
}

// TestIsSafeRelativePath_PathTraversal tests that path traversal attempts are rejected.
func TestIsSafeRelativePath_PathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outsideDir := filepath.Join(tempDir, "outside")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}

	server, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create a secret file outside data directory
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("sensitive"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Path traversal attempts should all be rejected
	traversalAttempts := []string{
		".." + string(filepath.Separator) + "outside",
		"..",
		"../outside/secret.txt",
	}

	for _, attempt := range traversalAttempts {
		_, ok := isSafeRelativePath(server.dataDir, attempt)
		if ok {
			t.Errorf("isSafeRelativePath(%q) = true, want false - path traversal should be rejected", attempt)
		}
	}

	// Note: "...." is actually a valid filename (just dots), not a path traversal
	// filepath.Join(dataDir, "....") would create a file named "...." inside dataDir
}

// TestIsSafeRelativePath_SymlinkTraversal tests that symlink-based path traversal is rejected.
func TestIsSafeRelativePath_SymlinkTraversal(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outsideDir := filepath.Join(tempDir, "outside")

	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("failed to create outside dir: %v", err)
	}

	server, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create a secret file outside data directory
	secretFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("sensitive"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Create a symlink inside dataDir that points outside
	symlinkPath := filepath.Join(dataDir, "evil-symlink")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Attempt to access files through symlink should be rejected
	_, ok := isSafeRelativePath(server.dataDir, "evil-symlink")
	if ok {
		t.Errorf("isSafeRelativePath(%q) = true, want false - symlink to outside dir should be rejected", "evil-symlink")
	}

	// Attempt to access secret file through symlink should be rejected
	_, ok = isSafeRelativePath(server.dataDir, "evil-symlink/secret.txt")
	if ok {
		t.Errorf("isSafeRelativePath(%q) = true, want false - symlink path traversal should be rejected", "evil-symlink/secret.txt")
	}

	// Create a valid experiment directory with a symlink inside
	expDir := filepath.Join(dataDir, "exp1")
	if err := os.MkdirAll(expDir, 0755); err != nil {
		t.Fatalf("failed to create experiment dir: %v", err)
	}

	// Valid experiment directory should be accepted
	_, ok = isSafeRelativePath(server.dataDir, "exp1")
	if !ok {
		t.Errorf("isSafeRelativePath(%q) = false, want true - valid experiment dir should be accepted", "exp1")
	}
}

// TestNewServer_AbsolutePath tests that NewServer converts dataDir to absolute path.
func TestNewServer_AbsolutePath(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("failed to create data dir: %v", err)
	}

	server, err := NewServer(dataDir)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Server's dataDir should be absolute
	if !filepath.IsAbs(server.dataDir) {
		t.Errorf("server.dataDir = %q, want absolute path", server.dataDir)
	}
}
