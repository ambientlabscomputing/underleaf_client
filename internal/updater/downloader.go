package updater

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Downloader handles downloading and verifying release binaries
type Downloader struct {
	httpClient  *http.Client
	stagingPath string
}

// NewDownloader creates a new downloader
func NewDownloader(stagingPath string) *Downloader {
	return &Downloader{
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
		stagingPath: stagingPath,
	}
}

// Download downloads a binary asset and verifies its SHA256
func (d *Downloader) Download(asset *ReleaseAsset, expectedSHA256 string) (string, error) {
	// Ensure staging directory exists
	if err := os.MkdirAll(d.stagingPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create staging directory: %w", err)
	}

	// Create temporary file for download
	tmpFile := filepath.Join(d.stagingPath, fmt.Sprintf("%s.tmp", asset.Name))
	finalFile := filepath.Join(d.stagingPath, asset.Name)

	// Download to temporary file
	if err := d.downloadFile(asset.DownloadURL, tmpFile); err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("failed to download %s: %w", asset.Name, err)
	}

	// Verify SHA256 if provided
	if expectedSHA256 != "" {
		actualSHA256, err := calculateSHA256(tmpFile)
		if err != nil {
			os.Remove(tmpFile)
			return "", fmt.Errorf("failed to calculate SHA256: %w", err)
		}

		if actualSHA256 != expectedSHA256 {
			os.Remove(tmpFile)
			return "", fmt.Errorf("SHA256 mismatch: expected %s, got %s", expectedSHA256, actualSHA256)
		}
	}

	// Make executable
	if err := os.Chmod(tmpFile, 0755); err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Atomic rename to final location
	if err := os.Rename(tmpFile, finalFile); err != nil {
		os.Remove(tmpFile)
		return "", fmt.Errorf("failed to move binary to final location: %w", err)
	}

	return finalFile, nil
}

// downloadFile downloads a file from URL to path
func (d *Downloader) downloadFile(url, path string) error {
	resp, err := d.httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// calculateSHA256 calculates the SHA256 hash of a file
func calculateSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// CleanStaging removes all files from the staging directory
func (d *Downloader) CleanStaging() error {
	if err := os.RemoveAll(d.stagingPath); err != nil {
		return fmt.Errorf("failed to clean staging directory: %w", err)
	}
	return os.MkdirAll(d.stagingPath, 0755)
}
