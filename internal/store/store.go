// Package store 负责 NanoAPI 的持久化：把集合、环境、历史与设置写入单个 JSON 文件。
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nanoapi/internal/model"
)

const (
	// saveDelay 是变更后合并落盘的防抖间隔。
	saveDelay = 300 * time.Millisecond
	// maxHistory 是历史记录保留的最大条数。
	maxHistory = 200
)

// Store 是一个线程安全的 JSON 文件存储。
type Store struct {
	mu    sync.Mutex
	path  string
	data  *model.AppData
	timer *time.Timer
}

// Open 打开（或创建）位于 path 的存储。
// 文件损坏时会备份原文件并以空数据继续运行。
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: &model.AppData{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// newData 返回一份带默认值的空数据。
func newData() *model.AppData {
	return &model.AppData{
		Collections:  []*model.CollectionItem{},
		Environments: []*model.Environment{},
		History:      []*model.HistoryEntry{},
		Settings:     model.DefaultSettings(),
	}
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = newData()
			return nil
		}
		return fmt.Errorf("读取数据文件失败: %w", err)
	}
	var d model.AppData
	if err := json.Unmarshal(b, &d); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%d.json", s.path, time.Now().Unix())
		_ = os.Rename(s.path, backup)
		s.data = newData()
		return nil
	}
	if d.Settings.TimeoutMS <= 0 {
		d.Settings = model.DefaultSettings()
	}
	if d.Collections == nil {
		d.Collections = []*model.CollectionItem{}
	}
	if d.Environments == nil {
		d.Environments = []*model.Environment{}
	}
	if d.History == nil {
		d.History = []*model.HistoryEntry{}
	}
	s.data = &d
	return nil
}

// Path 返回数据文件路径。
func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.path
}

// Snapshot 返回当前状态的副本。
func (s *Store) Snapshot() model.AppData {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := *s.data
	// 这里必须用非 nil 的空切片字面量，append(nil, 空切片...) 的结果是 nil。
	d.Collections = append([]*model.CollectionItem{}, s.data.Collections...)
	d.Environments = append([]*model.Environment{}, s.data.Environments...)
	d.History = append([]*model.HistoryEntry{}, s.data.History...)
	return d
}

// SetCollections 覆盖集合树并触发落盘。
func (s *Store) SetCollections(cols []*model.CollectionItem) {
	s.mu.Lock()
	s.data.Collections = cols
	s.mu.Unlock()
	s.schedule()
}

// SetEnvironments 覆盖环境列表并触发落盘。
func (s *Store) SetEnvironments(envs []*model.Environment) {
	s.mu.Lock()
	s.data.Environments = envs
	s.mu.Unlock()
	s.schedule()
}

// SetSettings 覆盖设置并触发落盘。
func (s *Store) SetSettings(set model.Settings) {
	s.mu.Lock()
	s.data.Settings = set
	s.mu.Unlock()
	s.schedule()
}

// AddHistory 把一条历史插入列表头部，超出上限时丢弃最旧的记录。
func (s *Store) AddHistory(e *model.HistoryEntry) {
	s.mu.Lock()
	s.data.History = append([]*model.HistoryEntry{e}, s.data.History...)
	if len(s.data.History) > maxHistory {
		s.data.History = s.data.History[:maxHistory]
	}
	s.mu.Unlock()
	s.schedule()
}

// ClearHistory 清空历史记录。
func (s *Store) ClearHistory() {
	s.mu.Lock()
	s.data.History = []*model.HistoryEntry{}
	s.mu.Unlock()
	s.schedule()
}

// schedule 在防抖窗口结束后落盘。
func (s *Store) schedule() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(saveDelay, func() {
		_ = s.Flush()
	})
	s.mu.Unlock()
}

// Flush 立即把当前状态写入磁盘。
func (s *Store) Flush() error {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	path := s.path
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("序列化数据失败: %w", err)
	}
	return writeAtomic(path, b)
}

// Close 停止定时器并做最后一次落盘。
func (s *Store) Close() error { return s.Flush() }

// writeAtomic 先写临时文件再重命名，避免写入中断留下半截文件。
func writeAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // 重命名成功后这里是无害的空操作

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("刷盘失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换数据文件失败: %w", err)
	}
	return nil
}
