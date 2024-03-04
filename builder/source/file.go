package source

import (
	"fmt"
	"io"
	"net/url"
	"os"
)

const (
	dstFileMode  = 0o644
	dstFileFlags = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
)

// IsFileURI returns true if the given URI is a file:// URI.
func IsFileURI(uri *url.URL) bool {
	return uri.Scheme == "file"
}

// CopyFile copies a file.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("unable to open source file: %w", err)
	}

	dstFile, err := os.OpenFile(dst, dstFileFlags, dstFileMode)
	if err != nil {
		return fmt.Errorf("unable to open destination file: %w", err)
	}

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("unable to copy file contents: %w", err)
	}

	return nil
}
