package vialite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocateBinaryExplicitPath(t *testing.T) {
	path := writeExecutable(t, "vialite")
	got, err := locateBinary(context.Background(), Options{BinaryPath: path, Offline: true})
	if err != nil {
		t.Fatalf("locateBinary: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func TestLocateBinaryEnvPath(t *testing.T) {
	path := writeExecutable(t, "vialite-env")
	t.Setenv("VIALITE_BINARY", path)
	got, err := locateBinary(context.Background(), Options{Offline: true})
	if err != nil {
		t.Fatalf("locateBinary: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func TestLocateLibraryExplicitPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libvialite.so")
	if err := os.WriteFile(path, []byte("so"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := locateLibrary(context.Background(), Options{LibraryPath: path, Offline: true})
	if err != nil {
		t.Fatalf("locateLibrary: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func TestLocateLibraryEnvPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libvialite-env.so")
	if err := os.WriteFile(path, []byte("so"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIALITE_LIBRARY", path)
	got, err := locateLibrary(context.Background(), Options{Offline: true})
	if err != nil {
		t.Fatalf("locateLibrary: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}

func TestLocateOfflineErrors(t *testing.T) {
	_, err := locateBinary(context.Background(), Options{Offline: true})
	if !errors.Is(err, ErrNoBinary) {
		t.Fatalf("binary error = %v, want ErrNoBinary", err)
	}
	_, err = locateLibrary(context.Background(), Options{Offline: true})
	if !errors.Is(err, ErrNoLibrary) {
		t.Fatalf("library error = %v, want ErrNoLibrary", err)
	}
}

func writeExecutable(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
