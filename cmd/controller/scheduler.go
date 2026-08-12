package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/J2026-dev/vbakup/internal/id"
	"github.com/J2026-dev/vbakup/internal/model"
)

func (s *server) runScheduler(interval time.Duration) {
	s.tick()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		s.tick()
	}
}

func (s *server) tick() {
	now := time.Now().UTC()
	state := s.store.Snapshot()
	_ = s.store.Update(func(st *model.State) error {
		for i := range st.Nodes {
			if now.Sub(st.Nodes[i].LastSeen) > 90*time.Second {
				st.Nodes[i].Status = "offline"
			}
		}
		return nil
	})
	for _, task := range state.Tasks {
		duration, ok := scheduleInterval(task.Schedule)
		if task.Enabled && ok && (task.LastRun.IsZero() || now.Sub(task.LastRun) >= duration) {
			_, _ = s.queueBackup(task.ID, false)
		}
	}
}

func (s *server) queueBackup(taskID string, force bool) (model.Command, error) {
	state := s.store.Snapshot()
	var task *model.Task
	for i := range state.Tasks {
		if state.Tasks[i].ID == taskID {
			copy := state.Tasks[i]
			task = &copy
			break
		}
	}
	if task == nil {
		return model.Command{}, errNotFound
	}
	for _, command := range state.Commands {
		if command.Type == "backup" && command.Task != nil && command.Task.ID == taskID {
			return model.Command{}, errors.New("该任务已有备份正在排队")
		}
	}
	if !force {
		duration, _ := scheduleInterval(task.Schedule)
		if !task.LastRun.IsZero() && time.Since(task.LastRun) < duration {
			return model.Command{}, errors.New("任务尚未到执行时间")
		}
	}
	repo, err := s.repositoryCredentials(task.RepositoryID)
	if err != nil {
		return model.Command{}, fmt.Errorf("读取备份库失败: %w", err)
	}
	command := model.Command{ID: id.New("cmd"), NodeID: task.NodeID, Type: "backup", Task: task, Payload: map[string]any{"repository": repo}, CreatedAt: time.Now().UTC()}
	err = s.store.Update(func(st *model.State) error {
		st.Commands = append(st.Commands, command)
		for i := range st.Tasks {
			if st.Tasks[i].ID == taskID {
				st.Tasks[i].LastRun = command.CreatedAt
				st.Tasks[i].LastStatus = "queued"
			}
		}
		return nil
	})
	return command, err
}

func scheduleInterval(value string) (time.Duration, bool) {
	switch value {
	case "@hourly":
		return time.Hour, true
	case "@6hours":
		return 6 * time.Hour, true
	case "@12hours":
		return 12 * time.Hour, true
	case "@daily", "":
		return 24 * time.Hour, true
	case "@weekly":
		return 7 * 24 * time.Hour, true
	default:
		d, err := time.ParseDuration(value)
		return d, err == nil && d >= 15*time.Minute
	}
}
