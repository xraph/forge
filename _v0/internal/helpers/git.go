// cmd/forge/internal/git.go
package helpers

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitHelper provides Git operations for the CLI
type GitHelper struct {
	fsHelper *FSHelper
}

// GitStatus represents the status of a Git repository
type GitStatus struct {
	IsRepository   bool     `json:"is_repository"`
	IsClean        bool     `json:"is_clean"`
	Branch         string   `json:"branch"`
	Commit         string   `json:"commit"`
	CommitShort    string   `json:"commit_short"`
	CommitMessage  string   `json:"commit_message"`
	HasUntracked   bool     `json:"has_untracked"`
	HasModified    bool     `json:"has_modified"`
	HasStaged      bool     `json:"has_staged"`
	ModifiedFiles  []string `json:"modified_files"`
	UntrackedFiles []string `json:"untracked_files"`
	StagedFiles    []string `json:"staged_files"`
	RemoteURL      string   `json:"remote_url"`
	HasUpstream    bool     `json:"has_upstream"`
	AheadBy        int      `json:"ahead_by"`
	BehindBy       int      `json:"behind_by"`
}

// GitCommit represents a Git commit
type GitCommit struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Message   string `json:"message"`
	Author    string `json:"author"`
	Date      string `json:"date"`
}

// GitRemote represents a Git remote
type GitRemote struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	FetchURL string `json:"fetch_url"`
	PushURL  string `json:"push_url"`
}

// NewGitHelper creates a new Git helper
func NewGitHelper() *GitHelper {
	return &GitHelper{
		fsHelper: NewFSHelper(),
	}
}

// IsGitInstalled checks if Git is installed and available
func (g *GitHelper) IsGitInstalled() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// GetVersion returns the Git version
func (g *GitHelper) GetVersion() (string, error) {
	if !g.IsGitInstalled() {
		return "", fmt.Errorf("git is not installed")
	}

	output, err := exec.Command("git", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git version: %w", err)
	}

	version := strings.TrimSpace(string(output))
	return strings.TrimPrefix(version, "git version "), nil
}

// IsRepository checks if a directory is a Git repository
func (g *GitHelper) IsRepository(path string) bool {
	gitDir := filepath.Join(path, ".git")
	return g.fsHelper.DirExists(gitDir) || g.fsHelper.FileExists(gitDir)
}

// InitRepository initializes a new Git repository
func (g *GitHelper) InitRepository(path string) error {
	if !g.IsGitInstalled() {
		return fmt.Errorf("git is not installed")
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to initialize git repository: %w", err)
	}

	// Create initial .gitignore if it doesn't exist
	gitignorePath := filepath.Join(path, ".gitignore")
	if !g.fsHelper.FileExists(gitignorePath) {
		defaultGitignore := `# Binaries for programs and plugins
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary, built with 'go test -c'
*.test

# Output of the go coverage tool, specifically when used with LiteIDE
*.out

# Go workspace file
go.work

# IDE
.vscode/
.idea/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db
`
		if err := g.fsHelper.WriteFileString(gitignorePath, defaultGitignore); err != nil {
			return fmt.Errorf("failed to create .gitignore: %w", err)
		}
	}

	return nil
}

// CloneRepository clones a Git repository
func (g *GitHelper) CloneRepository(url, path string, options ...string) error {
	if !g.IsGitInstalled() {
		return fmt.Errorf("git is not installed")
	}

	args := []string{"clone"}
	args = append(args, options...)
	args = append(args, url, path)

	cmd := exec.Command("git", args...)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	return nil
}

// GetStatus returns the status of a Git repository
func (g *GitHelper) GetStatus(path string) (*GitStatus, error) {
	status := &GitStatus{
		IsRepository: g.IsRepository(path),
	}

	if !status.IsRepository {
		return status, nil
	}

	// Get current branch
	branch, err := g.GetCurrentBranch(path)
	if err == nil {
		status.Branch = branch
	}

	// Get current commit
	commit, err := g.GetCurrentCommit(path)
	if err == nil {
		status.Commit = commit
		if len(commit) >= 7 {
			status.CommitShort = commit[:7]
		}
	}

	// Get commit message
	message, err := g.GetCommitMessage(path, "HEAD")
	if err == nil {
		status.CommitMessage = message
	}

	// Check if working directory is clean
	clean, err := g.IsWorkingDirectoryClean(path)
	if err == nil {
		status.IsClean = clean
	}

	// Get file statuses
	modified, err := g.GetModifiedFiles(path)
	if err == nil {
		status.ModifiedFiles = modified
		status.HasModified = len(modified) > 0
	}

	untracked, err := g.GetUntrackedFiles(path)
	if err == nil {
		status.UntrackedFiles = untracked
		status.HasUntracked = len(untracked) > 0
	}

	staged, err := g.GetStagedFiles(path)
	if err == nil {
		status.StagedFiles = staged
		status.HasStaged = len(staged) > 0
	}

	// Get remote information
	remoteURL, err := g.GetRemoteURL(path, "origin")
	if err == nil {
		status.RemoteURL = remoteURL
	}

	// Check upstream tracking
	hasUpstream, err := g.HasUpstream(path)
	if err == nil {
		status.HasUpstream = hasUpstream
	}

	// Get ahead/behind counts
	ahead, behind, err := g.GetAheadBehind(path)
	if err == nil {
		status.AheadBy = ahead
		status.BehindBy = behind
	}

	return status, nil
}

// GetCurrentBranch returns the current branch name
func (g *GitHelper) GetCurrentBranch(path string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCurrentCommit returns the current commit hash
func (g *GitHelper) GetCurrentCommit(path string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current commit: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// GetCommitMessage returns the commit message for a given reference
func (g *GitHelper) GetCommitMessage(path, ref string) (string, error) {
	cmd := exec.Command("git", "log", "-1", "--pretty=format:%s", ref)
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit message: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// IsWorkingDirectoryClean checks if the working directory is clean
func (g *GitHelper) IsWorkingDirectoryClean(path string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to check working directory status: %w", err)
	}

	return len(strings.TrimSpace(string(output))) == 0, nil
}

// GetModifiedFiles returns a list of modified files
func (g *GitHelper) GetModifiedFiles(path string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--name-only")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get modified files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(files) == 1 && files[0] == "" {
		return []string{}, nil
	}

	return files, nil
}

// GetUntrackedFiles returns a list of untracked files
func (g *GitHelper) GetUntrackedFiles(path string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get untracked files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(files) == 1 && files[0] == "" {
		return []string{}, nil
	}

	return files, nil
}

// GetStagedFiles returns a list of staged files
func (g *GitHelper) GetStagedFiles(path string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get staged files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(files) == 1 && files[0] == "" {
		return []string{}, nil
	}

	return files, nil
}

// AddFiles adds files to the Git index
func (g *GitHelper) AddFiles(path string, files []string) error {
	if len(files) == 0 {
		return nil
	}

	args := append([]string{"add"}, files...)
	cmd := exec.Command("git", args...)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add files: %w", err)
	}

	return nil
}

// AddAllFiles adds all files to the Git index
func (g *GitHelper) AddAllFiles(path string) error {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add all files: %w", err)
	}

	return nil
}

// Commit creates a new commit with the given message
func (g *GitHelper) Commit(path, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}

// CommitAll adds all files and creates a commit
func (g *GitHelper) CommitAll(path, message string) error {
	if err := g.AddAllFiles(path); err != nil {
		return err
	}

	return g.Commit(path, message)
}

// CreateBranch creates a new branch
func (g *GitHelper) CreateBranch(path, branchName string) error {
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create branch %s: %w", branchName, err)
	}

	return nil
}

// SwitchBranch switches to an existing branch
func (g *GitHelper) SwitchBranch(path, branchName string) error {
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to switch to branch %s: %w", branchName, err)
	}

	return nil
}

// GetBranches returns a list of all branches
func (g *GitHelper) GetBranches(path string) ([]string, error) {
	cmd := exec.Command("git", "branch")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get branches: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	branches := make([]string, len(lines))

	for i, line := range lines {
		// Remove the "* " prefix from current branch
		branch := strings.TrimSpace(line)
		if strings.HasPrefix(branch, "* ") {
			branch = strings.TrimPrefix(branch, "* ")
		}
		branches[i] = branch
	}

	return branches, nil
}

// GetRemotes returns a list of all remotes
func (g *GitHelper) GetRemotes(path string) ([]*GitRemote, error) {
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get remotes: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	remoteMap := make(map[string]*GitRemote)

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}

		name := parts[0]
		url := parts[1]
		direction := strings.Trim(parts[2], "()")

		if remote, exists := remoteMap[name]; exists {
			if direction == "fetch" {
				remote.FetchURL = url
			} else if direction == "push" {
				remote.PushURL = url
			}
		} else {
			remote := &GitRemote{
				Name: name,
				URL:  url,
			}
			if direction == "fetch" {
				remote.FetchURL = url
			} else if direction == "push" {
				remote.PushURL = url
			}
			remoteMap[name] = remote
		}
	}

	remotes := make([]*GitRemote, 0, len(remoteMap))
	for _, remote := range remoteMap {
		remotes = append(remotes, remote)
	}

	return remotes, nil
}

// GetRemoteURL returns the URL for a specific remote
func (g *GitHelper) GetRemoteURL(path, remoteName string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", remoteName)
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL for %s: %w", remoteName, err)
	}

	return strings.TrimSpace(string(output)), nil
}

// AddRemote adds a new remote
func (g *GitHelper) AddRemote(path, name, url string) error {
	cmd := exec.Command("git", "remote", "add", name, url)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to add remote %s: %w", name, err)
	}

	return nil
}

// HasUpstream checks if the current branch has an upstream
func (g *GitHelper) HasUpstream(path string) (bool, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "@{upstream}")
	cmd.Dir = path

	err := cmd.Run()
	return err == nil, nil
}

// GetAheadBehind returns how many commits ahead and behind the current branch is
func (g *GitHelper) GetAheadBehind(path string) (int, int, error) {
	cmd := exec.Command("git", "rev-list", "--left-right", "--count", "@{upstream}...HEAD")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get ahead/behind count: %w", err)
	}

	parts := strings.Fields(strings.TrimSpace(string(output)))
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected output format")
	}

	behind := 0
	ahead := 0

	fmt.Sscanf(parts[0], "%d", &behind)
	fmt.Sscanf(parts[1], "%d", &ahead)

	return ahead, behind, nil
}

// Pull pulls changes from the remote repository
func (g *GitHelper) Pull(path string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull: %w", err)
	}

	return nil
}

// Push pushes changes to the remote repository
func (g *GitHelper) Push(path string, args ...string) error {
	cmdArgs := append([]string{"push"}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// Fetch fetches changes from the remote repository
func (g *GitHelper) Fetch(path string, args ...string) error {
	cmdArgs := append([]string{"fetch"}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to fetch: %w", err)
	}

	return nil
}

// GetLog returns commit history
func (g *GitHelper) GetLog(path string, limit int) ([]*GitCommit, error) {
	args := []string{"log", "--pretty=format:%H|%h|%s|%an|%ad", "--date=iso"}
	if limit > 0 {
		args = append(args, fmt.Sprintf("-%d", limit))
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	commits := make([]*GitCommit, len(lines))

	for i, line := range lines {
		parts := strings.Split(line, "|")
		if len(parts) >= 5 {
			commits[i] = &GitCommit{
				Hash:      parts[0],
				ShortHash: parts[1],
				Message:   parts[2],
				Author:    parts[3],
				Date:      parts[4],
			}
		}
	}

	return commits, nil
}

// Tag creates a new tag
func (g *GitHelper) Tag(path, tagName, message string) error {
	var cmd *exec.Cmd
	if message != "" {
		cmd = exec.Command("git", "tag", "-a", tagName, "-m", message)
	} else {
		cmd = exec.Command("git", "tag", tagName)
	}
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tag %s: %w", tagName, err)
	}

	return nil
}

// GetTags returns a list of all tags
func (g *GitHelper) GetTags(path string) ([]string, error) {
	cmd := exec.Command("git", "tag", "-l")
	cmd.Dir = path

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get tags: %w", err)
	}

	tags := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(tags) == 1 && tags[0] == "" {
		return []string{}, nil
	}

	return tags, nil
}

// Reset resets the repository to a specific commit
func (g *GitHelper) Reset(path, ref string, hard bool) error {
	args := []string{"reset"}
	if hard {
		args = append(args, "--hard")
	}
	args = append(args, ref)

	cmd := exec.Command("git", args...)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reset to %s: %w", ref, err)
	}

	return nil
}

// Stash stashes current changes
func (g *GitHelper) Stash(path, message string) error {
	args := []string{"stash"}
	if message != "" {
		args = append(args, "save", message)
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stash: %w", err)
	}

	return nil
}

// StashPop pops the most recent stash
func (g *GitHelper) StashPop(path string) error {
	cmd := exec.Command("git", "stash", "pop")
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pop stash: %w", err)
	}

	return nil
}

// Clean removes untracked files
func (g *GitHelper) Clean(path string, force, directories bool) error {
	args := []string{"clean"}
	if force {
		args = append(args, "-f")
	}
	if directories {
		args = append(args, "-d")
	}

	cmd := exec.Command("git", args...)
	cmd.Dir = path

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clean: %w", err)
	}

	return nil
}
