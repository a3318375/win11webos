package mount

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) OpenContent(ctx context.Context, accountID, id string) (*os.File, error) {
	for _, part := range strings.Split(strings.ReplaceAll(id, "\\", "/"), "/") {
		if strings.EqualFold(part, ".cloud-trash") {
			return nil, errors.New("请从系统文件管理器恢复本地回收站中的文件")
		}
	}
	a, _, err := s.store.Get(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if a.Provider != "nas" {
		return nil, errors.New("此网盘的文件传输尚未接入")
	}
	if _, err = s.validateLocal(a.Config.Root); err != nil {
		return nil, err
	}
	if id == "" || filepath.IsAbs(id) || strings.Contains(id, "\\") || id == ".." || strings.HasPrefix(id, "../") {
		return nil, errors.New("文件路径无效")
	}
	root, err := os.OpenRoot(a.Config.Root)
	if err != nil {
		return nil, errors.New("本地根目录无法访问")
	}
	defer root.Close()
	file, err := root.Open(id)
	if err != nil {
		return nil, errors.New("文件不存在或不在挂载范围内")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("只能读取普通文件")
	}
	return file, nil
}
