package mount

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync"
	"time"
)

type TransferInput struct {
	SourceAccount string   `json:"sourceAccount"`
	SourceParent  string   `json:"sourceParent"`
	SourceTrail   []string `json:"sourceTrail"`
	IDs           []string `json:"ids"`
	TargetAccount string   `json:"targetAccount"`
	TargetParent  string   `json:"targetParent"`
	Conflict      string   `json:"conflict"`
}
type TransferTask struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	SourceName     string    `json:"sourceName"`
	TargetName     string    `json:"targetName"`
	TargetAccount  string    `json:"targetAccount"`
	TargetParent   string    `json:"targetParent"`
	State          string    `json:"state"`
	Phase          string    `json:"phase"`
	CurrentFile    string    `json:"currentFile"`
	TotalFiles     int       `json:"totalFiles"`
	CompletedFiles int       `json:"completedFiles"`
	SkippedFiles   int       `json:"skippedFiles"`
	TotalBytes     int64     `json:"totalBytes"`
	ProcessedBytes int64     `json:"processedBytes"`
	Speed          int64     `json:"speed"`
	Error          string    `json:"error"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
type transferResult struct {
	Destination string    `json:"destination"`
	SourceID    string    `json:"sourceID"`
	Size        int64     `json:"size"`
	ModifiedAt  time.Time `json:"modifiedAt"`
	Skipped     bool      `json:"skipped"`
}
type transferRecord struct {
	Task  TransferTask              `json:"task"`
	Input TransferInput             `json:"input"`
	Done  map[string]transferResult `json:"done"`
}
type Transfers struct {
	service *Service
	mu      sync.Mutex
	publish sync.Mutex
	tasks   map[string]*transferRecord
	running map[string]context.CancelFunc
	ctx     context.Context
	cancel  context.CancelFunc
	wake    chan struct{}
	wg      sync.WaitGroup
}

func (s *Service) initTransfers() error {
	if _, err := s.store.db.Exec(`CREATE TABLE IF NOT EXISTS transfer_tasks(id TEXT PRIMARY KEY,payload TEXT NOT NULL)`); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Transfers{service: s, tasks: map[string]*transferRecord{}, running: map[string]context.CancelFunc{}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 2)}
	rows, err := s.store.db.Query(`SELECT payload FROM transfer_tasks`)
	if err != nil {
		cancel()
		return err
	}
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			break
		}
		var record transferRecord
		if err = json.Unmarshal([]byte(payload), &record); err != nil {
			break
		}
		if record.Done == nil {
			record.Done = map[string]transferResult{}
		}
		if record.Task.State == "queued" || record.Task.State == "running" {
			record.Task.State = "paused"
			record.Task.Speed = 0
			record.Task.Error = "服务已重启，点击继续；已完成文件会保留，当前文件重新下载"
		}
		m.tasks[record.Task.ID] = &record
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil {
		cancel()
		return err
	}
	if rowErr != nil {
		cancel()
		return rowErr
	}
	s.transfers = m
	for _, record := range m.tasks {
		if cleanErr := m.cleanupPart(record.Task.ID, record.Input); cleanErr != nil {
			record.Task.Error = "未完成的临时文件无法清理，请检查目标目录权限"
		}
		if err = m.save(record); err != nil {
			cancel()
			return err
		}
	}
	for i := 0; i < 2; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	return nil
}
func (m *Transfers) save(r *transferRecord) error {
	r.Task.UpdatedAt = time.Now().UTC()
	payload, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = m.service.store.db.Exec(`INSERT INTO transfer_tasks(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, r.Task.ID, string(payload))
	return err
}
func (m *Transfers) close() {
	m.cancel()
	m.mu.Lock()
	for _, cancel := range m.running {
		cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Transfers) notify() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (s *Service) Transfers() []TransferTask {
	m := s.transfers
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []TransferTask{}
	for _, r := range m.tasks {
		out = append(out, r.Task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}
func (s *Service) CreateTransfer(ctx context.Context, in TransferInput) (TransferTask, error) {
	if len(in.IDs) < 1 || len(in.IDs) > 100 || len(in.SourceTrail) > 128 {
		return TransferTask{}, errors.New("一次请选择 1～100 项，目录层级不能超过 128")
	}
	if in.Conflict != "skip" && in.Conflict != "rename" {
		return TransferTask{}, errors.New("请选择跳过同名文件或自动重命名")
	}
	source, _, err := s.store.Get(ctx, in.SourceAccount)
	if err != nil {
		return TransferTask{}, err
	}
	if source.Provider != "nas" && source.Provider != "quark" {
		return TransferTask{}, errors.New("此网盘尚未接入文件下载，暂不能传输到本地")
	}
	target, _, err := s.store.Get(ctx, in.TargetAccount)
	if err != nil {
		return TransferTask{}, err
	}
	if target.Provider != "nas" {
		return TransferTask{}, errors.New("目标必须是已挂载的本地 NAS / 硬盘目录")
	}
	if _, err = s.validateLocal(target.Config.Root); err != nil {
		return TransferTask{}, err
	}
	listing, err := s.transferSource(ctx, in)
	if err != nil {
		return TransferTask{}, err
	}
	byID := map[string]Entry{}
	for _, e := range listing.Items {
		byID[e.ID] = e
	}
	seen := map[string]bool{}
	for _, id := range in.IDs {
		if _, ok := byID[id]; !ok || seen[id] {
			return TransferTask{}, errors.New("源项目已变化，请刷新后重新选择")
		}
		seen[id] = true
	}
	if _, err = s.List(ctx, in.TargetAccount, in.TargetParent, true); err != nil {
		return TransferTask{}, err
	}
	if err = s.checkTransferOverlap(source, target, in, byID); err != nil {
		return TransferTask{}, err
	}
	name := byID[in.IDs[0]].Name
	if len(in.IDs) > 1 {
		name += " 等 " + strconv.Itoa(len(in.IDs)) + " 项"
	}
	r := &transferRecord{Task: TransferTask{ID: ID(), Name: name, SourceName: source.Name, TargetName: target.Name, TargetAccount: target.ID, TargetParent: in.TargetParent, State: "queued", Phase: "waiting", CreatedAt: time.Now().UTC()}, Input: in, Done: map[string]transferResult{}}
	m := s.transfers
	m.mu.Lock()
	if len(m.tasks) >= 500 {
		m.mu.Unlock()
		return TransferTask{}, errors.New("任务记录已达 500 条，请先清理已结束的任务")
	}
	if err = m.save(r); err == nil {
		m.tasks[r.Task.ID] = r
	}
	snapshot := r.Task
	m.mu.Unlock()
	if err != nil {
		return TransferTask{}, errors.New("保存传输任务失败")
	}
	m.notify()
	return snapshot, nil
}
func (s *Service) TransferAction(id, action string) (TransferTask, error) {
	m := s.transfers
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.tasks[id]
	if !ok {
		return TransferTask{}, errors.New("传输任务不存在")
	}
	active := r.Task.State == "running" || r.Task.State == "queued"
	switch action {
	case "pause", "cancel":
		if !active && !(action == "cancel" && r.Task.State == "paused") {
			return TransferTask{}, errors.New("当前状态不能执行此操作")
		}
		if action == "pause" {
			r.Task.State = "paused"
		} else {
			r.Task.State = "canceled"
		}
		r.Task.Speed = 0
		r.Task.Error = ""
		if cancel := m.running[id]; cancel != nil {
			cancel()
		}
	case "retry":
		if active || r.Task.State == "completed" {
			return TransferTask{}, errors.New("当前任务无需重试")
		}
		if _, running := m.running[id]; running {
			return TransferTask{}, errors.New("任务正在停止，请稍后继续")
		}
		r.Task.State = "queued"
		r.Task.Error = ""
		r.Task.Speed = 0
		r.Task.Phase = "waiting"
	case "remove":
		if active {
			return TransferTask{}, errors.New("请先暂停或取消任务")
		}
		if _, running := m.running[id]; running {
			return TransferTask{}, errors.New("任务正在停止，请稍后移除")
		}
		if err := m.cleanupPart(id, r.Input); err != nil {
			return TransferTask{}, errors.New("无法清理任务临时文件，请检查目标目录权限")
		}
		if _, err := s.store.db.Exec(`DELETE FROM transfer_tasks WHERE id=?`, id); err != nil {
			return TransferTask{}, err
		}
		delete(m.tasks, id)
		return r.Task, nil
	default:
		return TransferTask{}, errors.New("未知任务操作")
	}
	if err := m.save(r); err != nil {
		return TransferTask{}, errors.New("保存任务状态失败")
	}
	if action == "retry" {
		m.notify()
	}
	return r.Task, nil
}
func (m *Transfers) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		}
		for {
			m.mu.Lock()
			if m.ctx.Err() != nil {
				m.mu.Unlock()
				return
			}
			var next *transferRecord
			for _, r := range m.tasks {
				if r.Task.State == "queued" && (next == nil || r.Task.CreatedAt.Before(next.Task.CreatedAt)) {
					next = r
				}
			}
			if next == nil {
				m.mu.Unlock()
				break
			}
			ctx, cancel := context.WithCancel(m.ctx)
			next.Task.State = "running"
			next.Task.Phase = "scanning"
			next.Task.Error = ""
			m.running[next.Task.ID] = cancel
			if err := m.save(next); err != nil {
				next.Task.State = "failed"
				next.Task.Error = "保存任务状态失败"
				delete(m.running, next.Task.ID)
				cancel()
				m.mu.Unlock()
				continue
			}
			id := next.Task.ID
			input := next.Input
			m.mu.Unlock()
			err := m.run(ctx, id, input)
			cancel()
			m.mu.Lock()
			record := m.tasks[id]
			delete(m.running, id)
			record.Task.Speed = 0
			if record.Task.State == "running" {
				if m.ctx.Err() != nil {
					record.Task.State = "paused"
					record.Task.Error = "服务已停止，点击继续"
				} else if err != nil {
					record.Task.State = "failed"
					record.Task.Error = err.Error()
				} else {
					record.Task.State = "completed"
					record.Task.Phase = "done"
					record.Task.CurrentFile = ""
				}
			}
			if saveErr := m.save(record); saveErr != nil {
				record.Task.State = "failed"
				record.Task.Error = "无法保存任务进度，请检查本地磁盘"
			}
			m.mu.Unlock()
			m.service.clearTransferCache(input.TargetAccount)
		}
	}
}
func (m *Transfers) update(id string, fn func(*transferRecord), persist bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.tasks[id]
	fn(r)
	r.Task.UpdatedAt = time.Now().UTC()
	if persist {
		return m.save(r)
	}
	return nil
}
func (s *Service) clearTransferCache(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, c := range s.cache {
		if len(key) > len(id) && key[:len(id)+1] == id+":" {
			s.cacheBytes -= c.bytes
			delete(s.cache, key)
		}
	}
}
