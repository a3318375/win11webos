package mount

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type transferFile struct {
	Entry            Entry
	Parent, Relative string
}

func (m *Transfers) cleanupPart(id string, in TransferInput) error {
	target, _, err := m.service.store.Get(context.Background(), in.TargetAccount)
	if err != nil {
		return nil
	}
	if target.Provider != "nas" {
		return nil
	}
	if _, err = m.service.validateLocal(target.Config.Root); err != nil {
		return err
	}
	root, err := os.OpenRoot(target.Config.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.Remove(path.Join(in.TargetParent, ".cloud-part-"+id))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Service) transferSource(ctx context.Context, in TransferInput) (Listing, error) {
	a, _, err := s.store.Get(ctx, in.SourceAccount)
	if err != nil {
		return Listing{}, err
	}
	if a.Provider == "nas" {
		return s.List(ctx, a.ID, in.SourceParent, true)
	}
	current, err := s.List(ctx, a.ID, "", true)
	if err != nil {
		return Listing{}, err
	}
	for _, next := range in.SourceTrail {
		found := false
		for _, entry := range current.Items {
			if entry.ID == next && entry.Directory {
				found = true
				break
			}
		}
		if !found {
			return Listing{}, errors.New("源目录路径已变化，请重新选择源文件")
		}
		current, err = s.List(ctx, a.ID, next, true)
		if err != nil {
			return Listing{}, err
		}
	}
	expected := ""
	if len(in.SourceTrail) > 0 {
		expected = in.SourceTrail[len(in.SourceTrail)-1]
	}
	if in.SourceParent != expected && !(expected == "" && in.SourceParent == a.Config.Root) {
		return Listing{}, errors.New("请从挂载根目录进入源文件夹后传输")
	}
	return current, nil
}
func (s *Service) checkTransferOverlap(source, target Account, in TransferInput, entries map[string]Entry) error {
	if source.Provider != "nas" {
		return nil
	}
	targetPath, err := filepath.EvalSymlinks(filepath.Join(target.Config.Root, filepath.FromSlash(in.TargetParent)))
	if err != nil {
		return errors.New("目标目录不可访问")
	}
	if !inside(target.Config.Root, targetPath) {
		return errors.New("目标目录超出挂载范围")
	}
	for _, id := range in.IDs {
		e := entries[id]
		from, err := filepath.EvalSymlinks(filepath.Join(source.Config.Root, filepath.FromSlash(e.ID)))
		if err != nil {
			return errors.New("源文件不可访问")
		}
		if e.Directory && inside(from, targetPath) {
			return errors.New("不能传输到源文件夹自身或它的子目录，包括重叠的挂载")
		}
		if strings.EqualFold(from, filepath.Join(targetPath, e.Name)) {
			return errors.New("源文件已在目标目录中")
		}
	}
	return nil
}
func (m *Transfers) scan(ctx context.Context, in TransferInput) ([]transferFile, error) {
	listing, err := m.service.transferSource(ctx, in)
	if err != nil {
		return nil, err
	}
	roots := map[string]Entry{}
	for _, entry := range listing.Items {
		roots[entry.ID] = entry
	}
	out := []transferFile{}
	seen := map[string]bool{}
	var walk func(Entry, string, string, int) error
	walk = func(entry Entry, parent, relative string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 || len(out) >= 20000 {
			return errors.New("单个任务最多 20000 项、128 层目录，请分批传输")
		}
		if !validFileName(entry.Name) {
			return fmt.Errorf("%s：名称不能用于本地文件，请先重命名", entry.Name)
		}
		if seen[entry.ID] {
			return errors.New("源目录存在循环或重复项目")
		}
		seen[entry.ID] = true
		out = append(out, transferFile{Entry: entry, Parent: parent, Relative: relative})
		if entry.Directory {
			children, err := m.service.List(ctx, in.SourceAccount, entry.ID, false)
			if err != nil {
				return err
			}
			for _, child := range children.Items {
				if err = walk(child, entry.ID, path.Join(relative, child.Name), depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, id := range in.IDs {
		entry, ok := roots[id]
		if !ok {
			return nil, errors.New("源文件已移动或删除")
		}
		if err = walk(entry, in.SourceParent, entry.Name, 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (m *Transfers) run(ctx context.Context, id string, in TransferInput) error {
	files, err := m.scan(ctx, in)
	if err != nil {
		return err
	}
	source, _, err := m.service.store.Get(ctx, in.SourceAccount)
	if err != nil {
		return err
	}
	target, _, err := m.service.store.Get(ctx, in.TargetAccount)
	if err != nil {
		return err
	}
	if target.Provider != "nas" {
		return errors.New("目标不再是本地存储")
	}
	if _, err = m.service.validateLocal(target.Config.Root); err != nil {
		return err
	}
	root, err := os.OpenRoot(target.Config.Root)
	if err != nil {
		return errors.New("目标目录不可写")
	}
	defer root.Close()
	dir, err := root.Open(in.TargetParentOrDot())
	if err != nil {
		return errors.New("目标文件夹已移动或删除")
	}
	info, err := dir.Stat()
	dir.Close()
	if err != nil || !info.IsDir() {
		return errors.New("目标必须是文件夹")
	}
	entries := map[string]Entry{}
	for _, f := range files {
		if f.Parent == in.SourceParent {
			entries[f.Entry.ID] = f.Entry
		}
	}
	if err = m.service.checkTransferOverlap(source, target, in, entries); err != nil {
		return err
	}
	totalFiles := 0
	var totalBytes int64
	for _, file := range files {
		if !file.Entry.Directory {
			totalFiles++
			totalBytes += file.Entry.Size
		}
	}
	if err = m.update(id, func(r *transferRecord) {
		r.Task.Phase = "downloading"
		r.Task.TotalFiles = totalFiles
		r.Task.TotalBytes = totalBytes
		r.Task.ProcessedBytes = 0
		r.Task.CompletedFiles = 0
		r.Task.SkippedFiles = 0
	}, true); err != nil {
		return errors.New("保存扫描结果失败")
	}
	part := path.Join(in.TargetParent, ".cloud-part-"+id)
	// A previous process may have stopped while writing this task's partial file.
	if err = root.Remove(part); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("无法清理未完成的临时文件")
	}
	defer root.Remove(part)
	var processed int64
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return err
		}
		destination := path.Join(in.TargetParent, file.Relative)
		if file.Entry.Directory {
			if err = root.MkdirAll(destination, 0755); err != nil {
				return fmt.Errorf("%s：无法创建目录", file.Relative)
			}
			continue
		}
		if err = root.MkdirAll(path.Dir(destination), 0755); err != nil {
			return fmt.Errorf("%s：目标目录不可写", file.Relative)
		}
		m.mu.Lock()
		previous, done := m.tasks[id].Done[file.Relative]
		m.mu.Unlock()
		validDone := done && previous.SourceID == file.Entry.ID && previous.Size == file.Entry.Size && previous.ModifiedAt.Equal(file.Entry.ModifiedAt)
		if validDone {
			stat, e := root.Lstat(previous.Destination)
			validDone = e == nil && stat.Mode().IsRegular() && (previous.Skipped || stat.Size() == previous.Size)
		}
		if validDone {
			processed += file.Entry.Size
			if err = m.markFile(id, file, previous, processed); err != nil {
				return err
			}
			continue
		}
		destination, skip, err := transferDestination(root, destination, in.Conflict)
		if err != nil {
			return fmt.Errorf("%s：%s", file.Relative, err)
		}
		result := transferResult{Destination: destination, SourceID: file.Entry.ID, Size: file.Entry.Size, ModifiedAt: file.Entry.ModifiedAt, Skipped: skip}
		if skip {
			processed += file.Entry.Size
			if err = m.markFile(id, file, result, processed); err != nil {
				return err
			}
			continue
		}
		if err = m.update(id, func(r *transferRecord) { r.Task.CurrentFile = file.Relative; r.Task.Speed = 0 }, false); err != nil {
			return err
		}
		size, err := m.downloadFile(ctx, id, in, file, root, part, processed)
		if err != nil {
			return fmt.Errorf("%s：%s", file.Relative, err)
		}
		if file.Entry.Size > 0 && size != file.Entry.Size {
			return fmt.Errorf("%s：文件大小发生变化或下载不完整，请重试", file.Relative)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		// Resolve conflicts again at publication: another queued task may have
		// completed the same name while this file was downloading.
		m.publish.Lock()
		destination, skip, err = transferDestination(root, path.Join(in.TargetParent, file.Relative), in.Conflict)
		if err == nil && !skip {
			err = renameExclusive(root, part, destination)
		}
		m.publish.Unlock()
		if err != nil {
			return fmt.Errorf("%s：目标同名文件出现或写入失败，未覆盖原文件", file.Relative)
		}
		result.Destination = destination
		result.Skipped = skip
		if skip {
			if err = root.Remove(part); err != nil {
				return errors.New("无法清理已跳过文件的临时数据")
			}
		}
		processed += file.Entry.Size
		if err = m.markFile(id, file, result, processed); err != nil {
			return err
		}
	}
	return nil
}
func (in TransferInput) TargetParentOrDot() string {
	if in.TargetParent == "" {
		return "."
	}
	return in.TargetParent
}
func transferDestination(root *os.Root, destination, conflict string) (string, bool, error) {
	info, err := root.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return destination, false, nil
	}
	if err != nil {
		return "", false, errors.New("无法检查目标文件")
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false, errors.New("目标存在同名文件夹或符号链接")
	}
	if conflict == "skip" {
		return destination, true, nil
	}
	ext := path.Ext(destination)
	stem := strings.TrimSuffix(destination, ext)
	for i := 1; i <= 10000; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err = root.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate, false, nil
		} else if err != nil {
			return "", false, err
		}
	}
	return "", false, errors.New("同名副本过多")
}
func (m *Transfers) markFile(id string, file transferFile, result transferResult, processed int64) error {
	err := m.update(id, func(r *transferRecord) {
		r.Done[file.Relative] = result
		r.Task.ProcessedBytes = processed
		if result.Skipped {
			r.Task.SkippedFiles++
		} else {
			r.Task.CompletedFiles++
		}
	}, true)
	if err != nil {
		return errors.New("保存文件进度失败；已下载的文件仍在目标目录")
	}
	return nil
}
func (m *Transfers) downloadFile(ctx context.Context, id string, in TransferInput, file transferFile, root *os.Root, part string, base int64) (int64, error) {
	var from io.ReadCloser
	source, _, err := m.service.store.Get(ctx, in.SourceAccount)
	if err != nil {
		return 0, err
	}
	if source.Provider == "nas" {
		from, err = m.service.OpenContent(ctx, in.SourceAccount, file.Entry.ID)
	} else {
		response, e := m.service.DownloadQuark(ctx, in.SourceAccount, file.Parent, file.Entry.ID, "")
		if e != nil {
			return 0, e
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			return 0, errors.New("网盘未返回完整文件")
		}
		from = response.Body
	}
	if err != nil {
		return 0, err
	}
	defer from.Close()
	to, err := root.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, errors.New("目标目录不可写或磁盘空间不足")
	}
	// Close the remote body promptly on pause/cancel; local reads check per chunk.
	stopClose := context.AfterFunc(ctx, func() { from.Close() })
	defer stopClose()
	buffer := make([]byte, 256*1024)
	var written, lastBytes int64
	last := time.Now()
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = from.Read(buffer)
		if n > 0 {
			count, writeErr := to.Write(buffer[:n])
			written += int64(count)
			if writeErr != nil {
				err = errors.New("写入失败，请检查目标磁盘空间和权限")
				break
			}
			if count != n {
				err = io.ErrShortWrite
				break
			}
			if elapsed := time.Since(last); elapsed >= 500*time.Millisecond {
				speed := int64(float64(written-lastBytes) / elapsed.Seconds())
				_ = m.update(id, func(r *transferRecord) { r.Task.ProcessedBytes = base + written; r.Task.Speed = speed }, false)
				last = time.Now()
				lastBytes = written
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			break
		}
	}
	if err == nil {
		err = to.Sync()
	}
	closeErr := to.Close()
	if err != nil {
		return written, err
	}
	return written, closeErr
}
