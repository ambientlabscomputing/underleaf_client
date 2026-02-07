package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// BinaryDownloader handles downloading and verifying binary artifacts.
type BinaryDownloader struct {
	stagingDir string
	httpClient *http.Client
}

// NewBinaryDownloader creates a new binary downloader.
func NewBinaryDownloader(stagingDir string) *BinaryDownloader {
	return &BinaryDownloader{
		stagingDir: stagingDir,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// DownloadResult contains information about a downloaded binary.
type DownloadResult struct {
	Path     string
	SHA256   string
	Size     int64
	Platform string
}

// Download downloads a binary from the given URI, verifies its checksum, and returns its path.
// The URI can be a template with {version}, {os}, and {arch} placeholders.
func (d *BinaryDownloader) Download(artifactURI, version, expectedSHA256 string) (*DownloadResult, error) {
	// Expand URI template
	uri := d.expandURITemplate(artifactURI, version)

	// Ensure staging directory exists
	if err := os.MkdirAll(d.stagingDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Extract filename from URI
	filename := filepath.Base(uri)
	tmpPath := filepath.Join(d.stagingDir, filename+".tmp")
	finalPath := filepath.Join(d.stagingDir, filename)

	// Download to temporary file
	if err := d.downloadFile(uri, tmpPath); err != nil {
		return nil, fmt.Errorf("failed to download binary: %w", err)
	}
	defer os.Remove(tmpPath) // Clean up tmp file on error

	// Calculate SHA256
	actualSHA256, err := d.calculateSHA256(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate SHA256: %w", err)
	}

	// Verify checksum if provided
	if expectedSHA256 != "" && expectedSHA256 != actualSHA256 {
		return nil, fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA256, actualSHA256)
	}

	// Make executable (Unix-like systems)
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to set executable permissions: %w", err)
	}

	// Atomic rename to final path
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return nil, fmt.Errorf("failed to move binary to final path: %w", err)
	}

	// Get file size
	stat, err := os.Stat(finalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat final binary: %w", err)
	}

	return &DownloadResult{
		Path:     finalPath,
		SHA256:   actualSHA256,
		Size:     stat.Size(),
		Platform: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}, nil
}

// DownloadWithChecksumsFile downloads a binary and verifies it against a checksums file.
func (d *BinaryDownloader) DownloadWithChecksumsFile(artifactURI, version, checksumsURI string) (*DownloadResult, error) {
	// Ensure staging directory exists
	if err := os.MkdirAll(d.stagingDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Download checksums file
	checksumsPath := filepath.Join(d.stagingDir, "checksums.txt")
	if err := d.downloadFile(checksumsURI, checksumsPath); err != nil {
		return nil, fmt.Errorf("failed to download checksums file: %w", err)
	}
	defer os.Remove(checksumsPath)

	// Parse checksums file
	checksums, err := d.parseChecksumsFile(checksumsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse checksums file: %w", err)
	}

	// Get the expected checksum for this binary
	uri := d.expandURITemplate(artifactURI, version)
	filename := filepath.Base(uri)
	expectedSHA256, ok := checksums[filename]
	if !ok {
		return nil, fmt.Errorf("checksum not found for %s in checksums file", filename)
	}

	// Download with verification
	return d.Download(artifactURI, version, expectedSHA256)
}

// CleanStaging removes all files from the staging directory.
func (d *BinaryDownloader) CleanStaging() error {
	return os.RemoveAll(d.stagingDir)
}

// expandURITemplate expands {version}, {os}, and {arch} placeholders in the URI.
func (d *BinaryDownloader) expandURITemplate(uri, version string) string {
	uri = strings.ReplaceAll(uri, "{version}", version)
	uri = strings.ReplaceAll(uri, "{os}", runtime.GOOS)
	uri = strings.ReplaceAll(uri, "{arch}", runtime.GOARCH)

	// Handle windows .exe extension
	if runtime.GOOS == "windows" && !strings.HasSuffix(uri, ".exe") {
		uri += ".exe"
	}

	return uri
}

// downloadFile downloads a file from the given URL to the specified path.
// Supports HTTP/HTTPS URLs and file:// URIs for local files.
func (d *BinaryDownloader) downloadFile(url, filepath string) error {
	// Handle file:// URLs for local files
	if strings.HasPrefix(url, "file://") {
		localPath := strings.TrimPrefix(url, "file://")
		return d.copyFile(localPath, filepath)
	}

	// Handle regular paths as local files (for convenience)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return d.copyFile(url, filepath)
	}

	// HTTP/HTTPS download
	resp, err := d.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("HTTP GET failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP GET returned status %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// copyFile copies a local file to the destination path.
func (d *BinaryDownloader) copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}

// calculateSHA256 calculates the SHA256 hash of a file.
func (d *BinaryDownloader) calculateSHA256(filepath string) (string, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// parseChecksumsFile parses a checksums.txt file (sha256sum format).
func (d *BinaryDownloader) parseChecksumsFile(path string) (map[string]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read checksums file: %w", err)
	}

	checksums := make(map[string]string)
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: "<sha256>  <filename>" or "<sha256> <filename>"
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		sha256Hash := parts[0]
		filename := parts[1]
		checksums[filename] = sha256Hash
	}

	return checksums, nil
}
