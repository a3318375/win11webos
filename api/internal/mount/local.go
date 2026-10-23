package mount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Entry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Directory  bool      `json:"directory"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modifiedAt"`
	Kind       string    `json:"kind"`
}

func kind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif":
		return "image"
	case ".txt", ".md", ".json", ".csv", ".log":
		return "text"
	case ".mp4", ".mkv", ".webm":
		return "video"
	case ".mp3", ".flac", ".wav":
		return "audio"
	}
	return "file"
}
func inside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// os.Root enforces confinement during filesystem operations, including symlinks.
// Names in APIs are relative and never server absolute paths.
func localList(ctx context.Context, root, id string) ([]Entry, error) {
	for _, part := range strings.Split(strings.ReplaceAll(id, "\\", "/"), "/") {
		if strings.EqualFold(part, ".cloud-trash") {
			return nil, errors.New("请从系统文件管理器恢复本地回收站中的文件")
		}
	}
	if id == "" {
		id = "."
	}
	if strings.Contains(id, "\\") || filepath.IsAbs(id) || id == ".." || strings.HasPrefix(id, "../") {
		return nil, errors.New("目录路径超出挂载范围")
	}
	handle, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("本地根目录不可访问")
	}
	defer handle.Close()
	dir, err := handle.Open(id)
	if err != nil {
		return nil, errors.New("目录不可读取或超出挂载范围")
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, errors.New("无法读取此目录")
	}
	out := []Entry{}
	for _, e := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if e.Type()&os.ModeSymlink != 0 || e.Name() == ".cloud-trash" || strings.HasPrefix(e.Name(), ".cloud-part-") {
			continue
		}
		info, eerr := e.Info()
		if eerr != nil {
			continue
		}
		entryID := filepath.ToSlash(filepath.Join(id, e.Name()))
		k := kind(e.Name())
		if info.IsDir() {
			k = "folder"
		}
		out = append(out, Entry{ID: entryID, Name: e.Name(), Directory: info.IsDir(), Size: info.Size(), ModifiedAt: info.ModTime(), Kind: k})
	}
	return out, nil
}
func (s *Service) validateLocal(root string) (string, error) {
	// Explicit development alias; normal deployments leave this unset.
	if target := os.Getenv("CLOUD_DEV_DATA_DIR"); target != "" && root == "/data" {
		root = target
	}
	absolute, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return "", errors.New("本地路径无效")
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", errors.New("目录不存在或不可访问")
	}
	for _, allowed := range s.roots {
		if inside(allowed, real) {
			info, e := os.Stat(real)
			if e == nil && info.IsDir() {
				return real, nil
			}
		}
	}
	return "", errors.New("此目录不在允许挂载的范围内，请配置 CLOUD_LOCAL_ROOTS")
}

type Directory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Service) Directories(ctx context.Context, path string) ([]Directory, error) {
	out := []Directory{}
	if path == "" {
		for _, root := range s.roots {
			out = append(out, Directory{Name: filepath.Base(root), Path: root})
		}
		return out, nil
	}
	root, err := s.validateLocal(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("目录无法读取")
	}
	for _, e := range entries {
		if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
			out = append(out, Directory{Name: e.Name(), Path: filepath.Join(root, e.Name())})
		}
	}
	return out, nil
}
