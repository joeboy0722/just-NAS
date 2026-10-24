package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"just-nas/internal/config"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Resource string     `json:"resource"`
	State    string     `json:"state"`
	Progress string     `json:"progress"`
	Error    string     `json:"error,omitempty"`
	Created  time.Time  `json:"created"`
	Finished *time.Time `json:"finished,omitempty"`
}
type Schedule struct {
	Retain       int       `json:"retain,omitempty"`
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Resource     string    `json:"resource"`
	EverySeconds int64     `json:"every_seconds"`
	Enabled      bool      `json:"enabled"`
	LastRun      time.Time `json:"last_run"`
}
type Handler func(context.Context, string, func(string)) error
type Engine struct {
	paused    bool
	mu        sync.Mutex
	jobs      []Job
	schedules []Schedule
	handlers  map[string]Handler
	running   map[string]bool
	cancel    map[string]context.CancelFunc
}

func New() *Engine {
	e := &Engine{jobs: []Job{}, schedules: []Schedule{}, handlers: map[string]Handler{}, running: map[string]bool{}, cancel: map[string]context.CancelFunc{}}
	if err := config.ReadJSON("jobs.json", &e.jobs); err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	if err := config.ReadJSON("schedules.json", &e.schedules); err != nil && !os.IsNotExist(err) {
		panic(err)
	}
	for i := range e.jobs {
		if e.jobs[i].State == "running" || e.jobs[i].State == "queued" {
			e.jobs[i].State = "interrupted"
			e.jobs[i].Error = "NAS restarted before task completed"
		}
	}
	return e
}
func (e *Engine) Register(kind string, h Handler) { e.handlers[kind] = h }
func (e *Engine) save() error {
	if err := config.WriteJSON("jobs.json", e.jobs); err != nil {
		return err
	}
	if err := config.WriteJSON("schedules.json", e.schedules); err != nil {
		return err
	}
	return config.NewPersistenceManager().SaveConfigToZfs()
}
func (e *Engine) Jobs() []Job { e.mu.Lock(); defer e.mu.Unlock(); return append([]Job{}, e.jobs...) }
func (e *Engine) Schedules() []Schedule {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Schedule{}, e.schedules...)
}
func (e *Engine) SetSchedule(s Schedule) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.handlers[s.Kind]; !ok {
		return fmt.Errorf("unsupported schedule")
	}
	if strings.HasPrefix(s.Kind, "snapshot-") && (s.Retain < 0 || s.Retain > 10000) {
		return fmt.Errorf("快照保留數須 1–10000，0 使用預設")
	}
	if s.EverySeconds < 60 || s.EverySeconds > 366*86400 {
		return fmt.Errorf("排程間隔須 60 秒到 366 天")
	}
	if s.ID == "" {
		s.ID = fmt.Sprint(time.Now().UnixNano())
	}
	old := append([]Schedule{}, e.schedules...)
	found := false
	for i, v := range e.schedules {
		if v.ID == s.ID {
			s.LastRun = v.LastRun
			e.schedules[i] = s
			found = true
		}
	}
	if !found {
		if s.LastRun.IsZero() {
			s.LastRun = time.Now()
		}
		e.schedules = append(e.schedules, s)
	}
	if err := e.save(); err != nil {
		e.schedules = old
		return err
	}
	return nil
}
func (e *Engine) Start(kind, resource string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.paused {
		return "", fmt.Errorf("maintenance paused for configuration restore")
	}
	h, ok := e.handlers[kind]
	if !ok {
		return "", fmt.Errorf("unsupported task")
	}
	if !config.Persistent() {
		return "", fmt.Errorf("請先建立設定池")
	}
	if len(e.running) > 0 {
		return "", fmt.Errorf("此資源已有進行中的任務")
	}
	id := fmt.Sprint(time.Now().UnixNano())
	j := Job{ID: id, Kind: kind, Resource: resource, State: "running", Created: time.Now()}
	e.jobs = append(e.jobs, j)
	if len(e.jobs) > 200 {
		e.jobs = e.jobs[len(e.jobs)-200:]
	}
	if err := e.save(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	e.running[resource] = true
	e.cancel[id] = cancel
	go func() {
		defer cancel()
		err := h(ctx, resource, func(p string) {
			e.mu.Lock()
			defer e.mu.Unlock()
			for i := range e.jobs {
				if e.jobs[i].ID == id {
					if len(p) > 2048 {
						p = p[len(p)-2048:]
					}
					e.jobs[i].Progress = p
				}
			}
		})
		e.mu.Lock()
		defer e.mu.Unlock()
		delete(e.running, resource)
		delete(e.cancel, id)
		now := time.Now()
		for i := range e.jobs {
			if e.jobs[i].ID == id {
				e.jobs[i].Finished = &now
				e.jobs[i].State = "succeeded"
				if err != nil {
					e.jobs[i].State = "failed"
					e.jobs[i].Error = err.Error()
				}
			}
		}
		if err := e.save(); err != nil {
			for i := range e.jobs {
				if e.jobs[i].ID == id {
					e.jobs[i].State = "failed"
					e.jobs[i].Error = "task state persistence failed: " + err.Error()
				}
			}
		}
	}()
	return id, nil
}
func (e *Engine) Loop() {
	go func() {
		for {
			time.Sleep(time.Minute)
			if !config.Persistent() {
				continue
			}
			due := e.Schedules()
			sort.SliceStable(due, func(i, j int) bool { return due[i].LastRun.Before(due[j].LastRun) })
			for _, s := range due {
				if s.Enabled && time.Since(s.LastRun) >= time.Duration(s.EverySeconds)*time.Second {
					if _, err := e.Start(s.Kind, s.Resource); err == nil {
						e.mu.Lock()
						for i := range e.schedules {
							if e.schedules[i].ID == s.ID {
								e.schedules[i].LastRun = time.Now()
							}
						}
						_ = e.save()
						e.mu.Unlock()
					}
				}
			}
		}
	}()
}
func Decode[T any](raw json.RawMessage) (T, error) {
	var t T
	e := json.Unmarshal(raw, &t)
	return t, e
}

func (e *Engine) Pause(paused bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if paused && len(e.running) > 0 {
		return fmt.Errorf("請等待目前任務完成後再恢復設定")
	}
	e.paused = paused
	return nil
}

func (e *Engine) Cancel(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.cancel[id]
	if !ok {
		return fmt.Errorf("找不到執行中的任務")
	}
	for _, job := range e.jobs {
		if job.ID == id && (job.Kind == "permissions" || job.Kind == "replace" || job.Kind == "smart-short") {
			return fmt.Errorf("此系統操作開始後不可透過任務取消；請等待結果")
		}
	}
	c()
	return nil
}
