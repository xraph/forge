package helpers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// FSHelper provides file system operations for the CLI
type FSHelper struct {
	// Cache for templates to avoid repeated parsing
	templateCache map[string]*template.Template
}

// NewFSHelper creates a new file system helper
func NewFSHelper() *FSHelper {
	return &FSHelper{
		templateCache: make(map[string]*template.Template),
	}
}

// FileExists checks if a file exists
func (fs *FSHelper) FileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// DirExists checks if a directory exists
func (fs *FSHelper) DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// CreateDir creates a directory with all parent directories
func (fs *FSHelper) CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// CreateDirIfNotExists creates a directory only if it doesn't exist
func (fs *FSHelper) CreateDirIfNotExists(path string) error {
	if fs.DirExists(path) {
		return nil
	}
	return fs.CreateDir(path)
}

// ReadFile reads the entire file content
func (fs *FSHelper) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ReadFileString reads file content as string
func (fs *FSHelper) ReadFileString(path string) (string, error) {
	content, err := fs.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteFile writes content to a file
func (fs *FSHelper) WriteFile(path string, content []byte) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := fs.CreateDirIfNotExists(dir); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	return os.WriteFile(path, content, 0644)
}

// WriteFileString writes string content to a file
func (fs *FSHelper) WriteFileString(path string, content string) error {
	return fs.WriteFile(path, []byte(content))
}

// AppendToFile appends content to a file
func (fs *FSHelper) AppendToFile(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(content)
	return err
}

// CopyFile copies a file from source to destination
func (fs *FSHelper) CopyFile(src, dest string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Ensure destination directory exists
	destDir := filepath.Dir(dest)
	if err := fs.CreateDirIfNotExists(destDir); err != nil {
		return err
	}

	destFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Copy file permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	return os.Chmod(dest, sourceInfo.Mode())
}

// CopyDir copies a directory recursively
func (fs *FSHelper) CopyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(dest, relPath)

		if info.IsDir() {
			return fs.CreateDirIfNotExists(destPath)
		}

		return fs.CopyFile(path, destPath)
	})
}

// MoveFile moves a file from source to destination
func (fs *FSHelper) MoveFile(src, dest string) error {
	// Try rename first (works if on same filesystem)
	if err := os.Rename(src, dest); err == nil {
		return nil
	}

	// Fall back to copy and delete
	if err := fs.CopyFile(src, dest); err != nil {
		return err
	}

	return os.Remove(src)
}

// DeleteFile deletes a file
func (fs *FSHelper) DeleteFile(path string) error {
	return os.Remove(path)
}

// DeleteDir deletes a directory and all its contents
func (fs *FSHelper) DeleteDir(path string) error {
	return os.RemoveAll(path)
}

// ListFiles lists all files in a directory
func (fs *FSHelper) ListFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	return files, nil
}

// ListDirs lists all directories in a directory
func (fs *FSHelper) ListDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(dir, entry.Name()))
		}
	}

	return dirs, nil
}

// WalkDir walks a directory tree and calls walkFn for each file or directory
func (fs *FSHelper) WalkDir(root string, walkFn filepath.WalkFunc) error {
	return filepath.Walk(root, walkFn)
}

// FindFiles finds files matching a pattern in a directory tree
func (fs *FSHelper) FindFiles(root string, pattern string) ([]string, error) {
	var matches []string

	err := fs.WalkDir(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			matched, err := filepath.Match(pattern, info.Name())
			if err != nil {
				return err
			}
			if matched {
				matches = append(matches, path)
			}
		}

		return nil
	})

	return matches, err
}

// GetFileSize returns the size of a file in bytes
func (fs *FSHelper) GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// GetFileInfo returns file information
func (fs *FSHelper) GetFileInfo(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// IsExecutable checks if a file is executable
func (fs *FSHelper) IsExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0111 != 0
}

// MakeExecutable makes a file executable
func (fs *FSHelper) MakeExecutable(path string) error {
	return os.Chmod(path, 0755)
}

// ReadLines reads a file and returns lines as a slice
func (fs *FSHelper) ReadLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

// WriteLines writes lines to a file
func (fs *FSHelper) WriteLines(path string, lines []string) error {
	content := strings.Join(lines, "\n")
	return fs.WriteFileString(path, content)
}

// ReplaceInFile replaces text in a file
func (fs *FSHelper) ReplaceInFile(path string, old, new string) error {
	content, err := fs.ReadFileString(path)
	if err != nil {
		return err
	}

	content = strings.ReplaceAll(content, old, new)
	return fs.WriteFileString(path, content)
}

// InsertAtLine inserts text at a specific line number (1-indexed)
func (fs *FSHelper) InsertAtLine(path string, lineNum int, text string) error {
	lines, err := fs.ReadLines(path)
	if err != nil {
		return err
	}

	if lineNum < 1 || lineNum > len(lines)+1 {
		return fmt.Errorf("line number %d is out of range", lineNum)
	}

	// Convert to 0-indexed
	lineNum--

	// Insert text at the specified line
	newLines := make([]string, len(lines)+1)
	copy(newLines[:lineNum], lines[:lineNum])
	newLines[lineNum] = text
	copy(newLines[lineNum+1:], lines[lineNum:])

	return fs.WriteLines(path, newLines)
}

// YAML operations

// ReadYAML reads and unmarshals YAML file
func (fs *FSHelper) ReadYAML(path string, v interface{}) error {
	content, err := fs.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.Unmarshal(content, v)
}

// WriteYAML marshals and writes YAML file
func (fs *FSHelper) WriteYAML(path string, v interface{}) error {
	content, err := yaml.Marshal(v)
	if err != nil {
		return err
	}

	return fs.WriteFile(path, content)
}

// UpdateYAML updates specific fields in a YAML file
func (fs *FSHelper) UpdateYAML(path string, updates map[string]interface{}) error {
	var data map[string]interface{}

	// Read existing data if file exists
	if fs.FileExists(path) {
		if err := fs.ReadYAML(path, &data); err != nil {
			return err
		}
	} else {
		data = make(map[string]interface{})
	}

	// Apply updates
	for key, value := range updates {
		data[key] = value
	}

	return fs.WriteYAML(path, data)
}

// Template operations

// WriteTemplate writes content using a template
func (fs *FSHelper) WriteTemplate(path string, tmpl *template.Template, data interface{}) error {
	// Create a buffer to capture template output
	var buf strings.Builder

	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute template: %w", err)
	}

	return fs.WriteFileString(path, buf.String())
}

// ProcessTemplate processes a template file and writes the result
func (fs *FSHelper) ProcessTemplate(templatePath, outputPath string, data interface{}) error {
	// Check if template is cached
	tmpl, exists := fs.templateCache[templatePath]
	if !exists {
		// Read and parse template
		content, err := fs.ReadFileString(templatePath)
		if err != nil {
			return err
		}

		tmpl, err = template.New(filepath.Base(templatePath)).Parse(content)
		if err != nil {
			return fmt.Errorf("failed to parse template %s: %w", templatePath, err)
		}

		// Cache the template
		fs.templateCache[templatePath] = tmpl
	}

	return fs.WriteTemplate(outputPath, tmpl, data)
}

// Command execution

// RunCommand executes a command in a specific directory
func (fs *FSHelper) RunCommand(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// RunCommandWithOutput executes a command and returns its output
func (fs *FSHelper) RunCommandWithOutput(dir string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	return cmd.Output()
}

// RunCommandSilent executes a command without output
func (fs *FSHelper) RunCommandSilent(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	return cmd.Run()
}

// Utility functions

// GetWorkingDir returns the current working directory
func (fs *FSHelper) GetWorkingDir() (string, error) {
	return os.Getwd()
}

// ChangeDir changes the current working directory
func (fs *FSHelper) ChangeDir(dir string) error {
	return os.Chdir(dir)
}

// GetHomeDir returns the user's home directory
func (fs *FSHelper) GetHomeDir() (string, error) {
	return os.UserHomeDir()
}

// ExpandPath expands ~ to home directory and resolves relative paths
func (fs *FSHelper) ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		homeDir, err := fs.GetHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(homeDir, path[2:])
	}

	return filepath.Abs(path)
}

// EnsureFileExtension ensures a file has the specified extension
func (fs *FSHelper) EnsureFileExtension(filename, ext string) string {
	if !strings.HasSuffix(filename, ext) {
		return filename + ext
	}
	return filename
}

// GetRelativePath returns the relative path from base to target
func (fs *FSHelper) GetRelativePath(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

// CleanPath cleans and normalizes a file path
func (fs *FSHelper) CleanPath(path string) string {
	return filepath.Clean(path)
}

// JoinPath joins path elements and cleans the result
func (fs *FSHelper) JoinPath(elements ...string) string {
	return filepath.Clean(filepath.Join(elements...))
}
