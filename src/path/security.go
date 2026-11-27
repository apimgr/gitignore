package path

import (
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	ErrPathTraversal = errors.New("path traversal attempt detected")
	ErrInvalidPath   = errors.New("invalid path characters")
	ErrPathTooLong   = errors.New("path exceeds maximum length")
	validPathSegment = regexp.MustCompile(`^[a-z0-9_-]+$`)
	validFileSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// SafePath validates and normalizes a route or configured identifier path.
func SafePath(input string) (string, error) {
	if input == "" {
		return "", nil
	}
	if len(input) > 2048 {
		return "", ErrPathTooLong
	}
	if strings.Contains(input, "..") {
		return "", ErrPathTraversal
	}
	for _, segment := range strings.Split(strings.Trim(input, "/"), "/") {
		if segment == "" {
			continue
		}
		if len(segment) > 64 {
			return "", ErrPathTooLong
		}
		if segment == "." || segment == ".." {
			return "", ErrPathTraversal
		}
		if !validPathSegment.MatchString(segment) {
			return "", ErrInvalidPath
		}
	}
	cleaned := strings.Trim(path.Clean(input), "/")
	if strings.Contains(cleaned, "..") {
		return "", ErrPathTraversal
	}
	return cleaned, nil
}

// SafeResourcePath validates a relative file or resource path without
// changing case or legal dots within a segment.
func SafeResourcePath(input string) (string, error) {
	if len(input) > 2048 {
		return "", ErrPathTooLong
	}
	if strings.Contains(input, "\\") {
		return "", ErrPathTraversal
	}
	segments := strings.Split(strings.Trim(input, "/"), "/")
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if len(segment) > 255 {
			return "", ErrPathTooLong
		}
		if !validFileSegment.MatchString(segment) {
			return "", ErrInvalidPath
		}
		kept = append(kept, segment)
	}
	if len(kept) == 0 {
		return "", ErrInvalidPath
	}
	return strings.Join(kept, "/"), nil
}

// SafeDirPath validates a filesystem directory override while preserving
// absolute paths and resolving relative paths against the current directory.
func SafeDirPath(input string) (string, error) {
	if input == "" {
		return "", ErrInvalidPath
	}
	for _, r := range input {
		if unicode.IsControl(r) {
			return "", ErrInvalidPath
		}
	}
	for _, segment := range strings.Split(filepath.ToSlash(input), "/") {
		if segment == ".." {
			return "", ErrPathTraversal
		}
	}
	cleaned := filepath.Clean(input)
	if !filepath.IsAbs(cleaned) {
		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return "", err
		}
		cleaned = abs
	}
	return cleaned, nil
}

// SafeFilePath validates a resource path and ensures its resolved path stays
// inside baseDir.
func SafeFilePath(baseDir, userPath string) (string, error) {
	for _, segment := range strings.Split(filepath.ToSlash(userPath), "/") {
		if segment == ".." {
			return "", ErrPathTraversal
		}
	}
	safe, err := SafeResourcePath(userPath)
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Join(baseDir, filepath.FromSlash(safe)))
	if err != nil {
		return "", err
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(absBase, fullPath)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrPathTraversal
	}
	return fullPath, nil
}
