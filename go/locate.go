package vialite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func locateBinary(ctx context.Context, opts Options) (string, error) {
	if opts.BinaryPath != "" {
		if err := executableAt(opts.BinaryPath); err != nil {
			return "", fmt.Errorf("vialite: BinaryPath %q: %w", opts.BinaryPath, err)
		}
		return opts.BinaryPath, nil
	}
	if env := os.Getenv("VIALITE_BINARY"); env != "" {
		if err := executableAt(env); err != nil {
			return "", fmt.Errorf("vialite: $VIALITE_BINARY %q: %w", env, err)
		}
		return env, nil
	}
	if path, ok, err := extractEmbeddedBinary(); err != nil && !errors.Is(err, ErrUnsupportedEmbeddedMode) {
		return "", err
	} else if ok {
		return path, nil
	}
	if path, err := exec.LookPath("vialite"); err == nil {
		return path, nil
	}
	if !opts.Offline {
		if path, err := downloadAsset(ctx, opts, assetKindBinary); err == nil {
			return path, nil
		} else {
			return "", fmt.Errorf("%w: auto-download failed: %v", ErrNoBinary, err)
		}
	}
	return "", ErrNoBinary
}

func locateLibrary(ctx context.Context, opts Options) (string, error) {
	if opts.LibraryPath != "" {
		if err := fileAt(opts.LibraryPath); err != nil {
			return "", fmt.Errorf("vialite: LibraryPath %q: %w", opts.LibraryPath, err)
		}
		return opts.LibraryPath, nil
	}
	if env := os.Getenv("VIALITE_LIBRARY"); env != "" {
		if err := fileAt(env); err != nil {
			return "", fmt.Errorf("vialite: $VIALITE_LIBRARY %q: %w", env, err)
		}
		return env, nil
	}
	if path, ok, err := extractEmbeddedLibrary(); err != nil && !errors.Is(err, ErrUnsupportedEmbeddedMode) {
		return "", err
	} else if ok {
		return path, nil
	}
	for _, dir := range systemLibDirs() {
		p := filepath.Join(dir, libraryName())
		if fileAt(p) == nil {
			return p, nil
		}
	}
	if !opts.Offline {
		if path, err := downloadAsset(ctx, opts, assetKindLibrary); err == nil {
			return path, nil
		} else {
			return "", fmt.Errorf("%w: auto-download failed: %v", ErrNoLibrary, err)
		}
	}
	return "", ErrNoLibrary
}

func libraryName() string {
	switch runtime.GOOS {
	case "darwin":
		return "libvialite.dylib"
	case "windows":
		return "vialite.dll"
	default:
		return "libvialite.so"
	}
}

func systemLibDirs() []string {
	dirs := []string{"/usr/local/lib", "/usr/lib"}
	if env := os.Getenv("LD_LIBRARY_PATH"); env != "" {
		dirs = append(filepath.SplitList(env), dirs...)
	}
	return dirs
}

func executableAt(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("is a directory")
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}

func fileAt(p string) error {
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("is a directory")
	}
	return nil
}
