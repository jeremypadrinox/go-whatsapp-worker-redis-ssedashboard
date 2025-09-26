package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileStorage struct {
	TempDir string
}

func NewFileStorage(tempDir string) *FileStorage {
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		tempDir = os.TempDir()
	}

	return &FileStorage{
		TempDir: tempDir,
	}
}

func (fs *FileStorage) StoreFile(filename string, content io.Reader) (string, error) {
	ext := filepath.Ext(filename)
	baseName := strings.TrimSuffix(filename, ext)
	timestamp := time.Now().UnixNano()
	uniqueFilename := fmt.Sprintf("%s_%d%s", baseName, timestamp, ext)

	filePath := filepath.Join(fs.TempDir, uniqueFilename)

	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, content)
	if err != nil {
		os.Remove(filePath)
		return "", fmt.Errorf("failed to copy file content: %w", err)
	}

	return filePath, nil
}

func (fs *FileStorage) DeleteFile(filePath string) error {
	if filePath == "" {
		return nil
	}

	if !strings.HasPrefix(filePath, fs.TempDir) {
		return fmt.Errorf("file path is not in temp directory")
	}

	return os.Remove(filePath)
}

// CleanupOldFiles removes files older than the specified duration
func (fs *FileStorage) CleanupOldFiles(maxAge time.Duration) error {
	files, err := os.ReadDir(fs.TempDir)
	if err != nil {
		return fmt.Errorf("failed to read temp directory: %w", err)
	}
	
	cutoff := time.Now().Add(-maxAge)
	var errors []string
	
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		
		info, err := file.Info()
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to get file info for %s: %v", file.Name(), err))
			continue
		}
		
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(fs.TempDir, file.Name())
			if err := os.Remove(filePath); err != nil {
				errors = append(errors, fmt.Sprintf("failed to delete file %s: %v", file.Name(), err))
			}
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errors, "; "))
	}
	
	return nil
}

// GetFileSize returns the size of a file
func (fs *FileStorage) GetFileSize(filePath string) (int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// FileExists checks if a file exists
func (fs *FileStorage) FileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
