package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"
)

type FileAction struct {
	Action       string   `json:"action"`
	Parent       string   `json:"parent"`
	SourceParent string   `json:"sourceParent"`
	IDs          []string `json:"ids"`
	Name         string   `json:"name"`
}

func validFileName(name string) bool {
	return name != "" && name != "." && name != ".." && len([]rune(name)) <= 255 && !strings.ContainsAny(name, "\\/:*?\"<>|\x00") && !strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ") && !strings.HasPrefix(strings.ToLower(name), ".cloud-trash") && !strings.HasPrefix(strings.ToLower(name), ".cloud-part-")
}

// FileActions validates sources against a directory inside the mounted root.
// Batches report how many completed when an error interrupts local operations.
func (s *Service) FileActions(ctx context.Context, id string, in FileAction) (int, error) {
	if in.Action != "mkdir" && in.Action != "rename" && in.Action != "copy" && in.Action != "move" && in.Action != "trash" {
		return 0, errors.New("不支持的文件操作")
	}
	if (in.Action == "mkdir" || in.Action == "rename") && !validFileName(in.Name) {
		return 0, errors.New("名称不能为空，或包含不允许的字符")
	}
	if in.Action != "mkdir" && (len(in.IDs) == 0 || len(in.IDs) > 100 || (in.Action == "rename" && len(in.IDs) != 1)) {
		return 0, errors.New("每次请选择 1～100 项；重命名仅支持单项")
	}
	source, err := s.List(ctx, id, in.SourceParent, true)
	if err != nil {
		return 0, err
	}
	target := source
	if in.Parent != in.SourceParent {
		target, err = s.List(ctx, id, in.Parent, true)
		if err != nil {
			return 0, err
		}
	}
	found := map[string]Entry{}
	for _, e := range source.Items {
		found[e.ID] = e
	}
	seen := map[string]bool{}
	entries := []Entry{}
	for _, fid := range in.IDs {
		e, ok := found[fid]
		if !ok || seen[fid] {
			return 0, errors.New("所选文件已变化，请刷新后重试")
		}
		seen[fid] = true
		entries = append(entries, e)
	}
	names := map[string]bool{}
	for _, e := range target.Items {
		names[strings.ToLower(e.Name)] = true
	}
	if (in.Action == "rename" || in.Action == "mkdir") && names[strings.ToLower(in.Name)] {
		return 0, errors.New("目标目录已有同名项目，请换一个名称")
	}
	if in.Action == "move" || in.Action == "copy" {
		for _, e := range entries {
			if names[strings.ToLower(e.Name)] {
				return 0, errors.New("目标目录已有同名项目，未覆盖任何文件")
			}
		}
	}
	lock, _ := s.accountLocks.LoadOrStore(id, &sync.Mutex{})
	m := lock.(*sync.Mutex)
	m.Lock()
	defer m.Unlock()
	a, creds, err := s.store.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	// Invalidate directory contents while preserving the discovered root boundary.
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for key, c := range s.cache {
			if strings.HasPrefix(key, id+":") {
				s.cacheBytes -= c.bytes
				delete(s.cache, key)
			}
		}
	}()
	if a.Provider == "quark" {
		return s.quarkAction(ctx, a, creds, in)
	}
	if a.Provider != "nas" {
		return 0, errors.New("此网盘暂未接入文件写入操作")
	}
	if _, err = s.validateLocal(a.Config.Root); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(a.Config.Root)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	if in.Action == "mkdir" {
		err = root.Mkdir(path.Join(in.Parent, in.Name), 0755)
		if err != nil {
			return 0, errors.New("创建失败：名称已存在或目录不可写")
		}
		return 1, nil
	}
	done := 0
	for _, e := range entries {
		if err = ctx.Err(); err != nil {
			return done, err
		}
		switch in.Action {
		case "rename":
			err = renameExclusive(root, e.ID, path.Join(in.Parent, in.Name))
		case "move", "copy":
			dest := path.Join(in.Parent, e.Name)
			if e.Directory && (dest == e.ID || strings.HasPrefix(dest, e.ID+"/")) {
				return done, errors.New("不能把文件夹放入自身或它的子文件夹")
			}
			if in.Action == "move" {
				err = renameExclusive(root, e.ID, dest)
			} else {
				err = copyLocal(ctx, root, e.ID, dest)
			}
		case "trash":
			// Keep recoverable files within the mounted root; never permanently delete.
			err = root.Mkdir(".cloud-trash", 0700)
			if errors.Is(err, os.ErrExist) {
				err = nil
			}
			if err == nil {
				err = renameExclusive(root, e.ID, path.Join(".cloud-trash", time.Now().Format("20060102-150405")+"-"+ID()+"-"+e.Name))
			}
		}
		if err != nil {
			return done, errors.New("操作失败：项目已存在、已变化或目录不可写；已完成的项目不会回滚")
		}
		done++
	}
	return done, nil
}
func renameExclusive(root *os.Root, src, dst string) error {
	if _, err := root.Lstat(dst); !errors.Is(err, os.ErrNotExist) {
		return os.ErrExist
	}
	return root.Rename(src, dst)
}
func copyLocal(ctx context.Context, root *os.Root, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := root.Lstat(src)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("不复制符号链接")
	}
	if info.IsDir() {
		if err = root.Mkdir(dst, 0755); err != nil {
			return err
		}
		f, err := root.Open(src)
		if err != nil {
			return err
		}
		children, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return err
		}
		for _, child := range children {
			if child.Name() == ".cloud-trash" {
				continue
			}
			if err = copyLocal(ctx, root, path.Join(src, child.Name()), path.Join(dst, child.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return errors.New("仅支持普通文件")
	}
	from, err := root.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := root.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(to, from)
	closeErr := to.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (s *Service) quarkJSON(ctx context.Context, c Credentials, method, endpoint string, body any) (map[string]any, error) {
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	var out map[string]any
	err := s.jsonRequest(ctx, method, "https://drive-pc.quark.cn/1/clouddrive"+endpoint, bytes.NewReader(payload), map[string]string{"Content-Type": "application/json", "Cookie": c.Cookie, "Referer": "https://pan.quark.cn/", "User-Agent": quarkDesktopUA}, &out)
	if err != nil {
		return nil, err
	}
	if number(out["status"]) != 200 {
		return nil, remoteError("夸克", number(out["code"]))
	}
	return object(out["data"]), nil
}
func (s *Service) quarkAction(ctx context.Context, a Account, c Credentials, in FileAction) (int, error) {
	parent := in.Parent
	if parent == "" {
		parent = a.Config.Root
	}
	endpoint := ""
	var body any
	switch in.Action {
	case "mkdir":
		endpoint = "/file"
		body = map[string]any{"pdir_fid": parent, "file_name": in.Name, "dir_init_lock": false, "dir_path": ""}
	case "rename":
		endpoint = "/file/rename"
		body = map[string]any{"fid": in.IDs[0], "file_name": in.Name}
	case "trash":
		endpoint = "/file/delete"
		body = map[string]any{"filelist": in.IDs, "action_type": 1}
	case "copy":
		endpoint = "/file/copy"
		body = map[string]any{"filelist": in.IDs, "to_pdir_fid": parent}
	case "move":
		endpoint = "/file/move"
		body = map[string]any{"filelist": in.IDs, "to_pdir_fid": parent, "action_type": 1, "exclude_fids": []string{}}
	}
	data, err := s.quarkJSON(ctx, c, "POST", endpoint+"?pr=ucpro&fr=pc", body)
	if err != nil {
		return 0, err
	}
	if in.Action == "copy" {
		task := str(data["task_id"])
		if task == "" {
			return 0, errors.New("复制已提交但未取得任务编号，请刷新目标目录确认")
		}
		for i := 0; i < 30; i++ {
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(time.Second):
			}
			state, e := s.quarkJSON(ctx, c, "GET", "/task?pr=ucpro&fr=pc&task_id="+url.QueryEscape(task)+"&retry_index=0", nil)
			if e != nil {
				return 0, e
			}
			if number(state["status"]) == 2 {
				return len(in.IDs), nil
			}
		}
		return 0, errors.New("复制任务尚未完成，请稍后刷新目录，避免重复提交")
	}
	if in.Action == "mkdir" {
		return 1, nil
	}
	return len(in.IDs), nil
}
