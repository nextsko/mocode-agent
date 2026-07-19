package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/package-register/mocode/internal/domain/history"
	sessionpkg "github.com/package-register/mocode/internal/domain/session"
	"github.com/package-register/mocode/internal/domain/session/message"
	"github.com/package-register/mocode/internal/ui/util"
	"github.com/package-register/mocode/internal/util/infra"
)

type rollbackTarget struct {
	Message message.Message
	Index   int
}

func (m *UI) rollbackSession(node string) tea.Cmd {
	if !m.hasSession() {
		return util.ReportWarn("No active session to rollback")
	}
	node = strings.TrimSpace(node)
	if node == "" {
		return util.ReportWarn("Usage: /rollback <message-index|message-id-prefix>")
	}

	sess := *m.session
	workingDir := m.com.Workspace.WorkingDir()
	return func() tea.Msg {
		ctx := context.Background()
		messages, err := m.com.Workspace.ListMessages(ctx, sess.ID)
		if err != nil {
			return util.NewErrorMsg(fmt.Errorf("list session messages: %w", err))
		}
		target, err := resolveRollbackTarget(messages, node)
		if err != nil {
			return util.InfoMsg{Type: util.InfoTypeWarn, Msg: err.Error()}
		}

		files, err := m.com.Workspace.ListSessionHistory(ctx, sess.ID)
		if err != nil {
			return util.NewErrorMsg(fmt.Errorf("list session file history: %w", err))
		}
		if len(files) == 0 {
			return util.InfoMsg{Type: util.InfoTypeWarn, Msg: "No file history found for this session"}
		}

		repoDir, err := rollbackRepoDir(sess, workingDir)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if err := commitRollbackState(repoDir, workingDir, currentTrackedFileState(files, workingDir), "pre-rollback to "+shortNode(target)); err != nil {
			return util.NewErrorMsg(fmt.Errorf("snapshot before rollback: %w", err))
		}

		restored, removed, err := restoreFilesAt(files, workingDir, target.Message.UpdatedAt)
		if err != nil {
			return util.NewErrorMsg(err)
		}
		if err := commitRollbackState(repoDir, workingDir, currentTrackedFileState(files, workingDir), "rollback target "+shortNode(target)); err != nil {
			return util.NewErrorMsg(fmt.Errorf("snapshot after rollback: %w", err))
		}

		return util.InfoMsg{
			Type: util.InfoTypeInfo,
			Msg:  fmt.Sprintf("Rolled back to node #%d (%s): restored %d file(s), removed %d. Snapshot: %s", target.Index, shortID(target.Message.ID), restored, removed, repoDir),
		}
	}
}

func resolveRollbackTarget(messages []message.Message, node string) (rollbackTarget, error) {
	filtered := make([]message.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.IsSummaryMessage {
			continue
		}
		filtered = append(filtered, msg)
	}
	if len(filtered) == 0 {
		return rollbackTarget{}, errors.New("No session nodes found")
	}

	if strings.HasPrefix(node, "#") {
		node = strings.TrimPrefix(node, "#")
	}
	if idx, err := strconv.Atoi(node); err == nil {
		if idx < 1 || idx > len(filtered) {
			return rollbackTarget{}, fmt.Errorf("Rollback node index must be between 1 and %d", len(filtered))
		}
		return rollbackTarget{Message: filtered[idx-1], Index: idx}, nil
	}

	for i, msg := range filtered {
		if msg.ID == node || strings.HasPrefix(msg.ID, node) {
			return rollbackTarget{Message: msg, Index: i + 1}, nil
		}
	}
	return rollbackTarget{}, fmt.Errorf("Rollback node not found: %s", node)
}

func restoreFilesAt(files []history.File, workingDir string, cutoff int64) (restored, removed int, err error) {
	byPath := map[string][]history.File{}
	for _, file := range files {
		byPath[file.Path] = append(byPath[file.Path], file)
	}

	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	for _, path := range paths {
		versions := byPath[path]
		slices.SortStableFunc(versions, func(a, b history.File) int {
			if a.CreatedAt == b.CreatedAt {
				return int(a.Version - b.Version)
			}
			return int(a.CreatedAt - b.CreatedAt)
		})

		var target *history.File
		for i := range versions {
			if versions[i].CreatedAt <= cutoff {
				target = &versions[i]
			}
		}

		absPath := absoluteHistoryPath(workingDir, path)
		if target == nil {
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				return restored, removed, fmt.Errorf("remove %s: %w", path, err)
			}
			removed++
			continue
		}

		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return restored, removed, fmt.Errorf("create parent for %s: %w", path, err)
		}
		if err := os.WriteFile(absPath, []byte(target.Content), 0o644); err != nil {
			return restored, removed, fmt.Errorf("restore %s: %w", path, err)
		}
		restored++
	}
	return restored, removed, nil
}

func currentTrackedFileState(files []history.File, workingDir string) map[string]string {
	paths := map[string]struct{}{}
	for _, file := range files {
		paths[file.Path] = struct{}{}
	}
	state := make(map[string]string, len(paths))
	for path := range paths {
		content, err := os.ReadFile(absoluteHistoryPath(workingDir, path))
		if err != nil {
			continue
		}
		state[path] = string(content)
	}
	return state
}

func rollbackRepoDir(sess sessionpkg.Session, workingDir string) (string, error) {
	project := filepath.Base(workingDir)
	if project == "." || project == string(filepath.Separator) || project == "" {
		project = "project"
	}
	root := infra.ScreenshotsDir()
	sessionDir := filepath.Base(sessionpkg.StoreDir(root, sess))
	return filepath.Join(root, sessionDir, safePathName(project)), nil
}

func commitRollbackState(repoDir, workingDir string, state map[string]string, msg string) error {
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return err
	}
	repo, err := git.PlainOpen(repoDir)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(repoDir, false)
	}
	if err != nil {
		return err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := clearRollbackRepoWorktree(repoDir); err != nil {
		return err
	}

	for path, content := range state {
		rel, ok := rollbackRepoRelPath(workingDir, path)
		if !ok {
			continue
		}
		dst := filepath.Join(repoDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return err
		}
		if _, err := wt.Add(rel); err != nil {
			return err
		}
	}

	_, err = wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "mocode", Email: "mocode@local", When: time.Now()},
		All:    true,
	})
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "clean") {
		return err
	}
	return nil
}

func clearRollbackRepoWorktree(repoDir string) error {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(repoDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func rollbackRepoRelPath(workingDir, path string) (string, bool) {
	abs := absoluteHistoryPath(workingDir, path)
	rel, err := filepath.Rel(workingDir, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func absoluteHistoryPath(workingDir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workingDir, path)
}

func safePathName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, "-._")
	if name == "" {
		return "project"
	}
	return name
}

func shortNode(target rollbackTarget) string {
	return fmt.Sprintf("#%d %s", target.Index, shortID(target.Message.ID))
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
