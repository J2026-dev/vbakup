package main

import (
	"errors"
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
		expireQueuedCommands(st, now, 10*time.Minute)
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
	if !nodeIsOnline(state, task.NodeID, time.Now().UTC()) {
		return model.Command{}, errors.New("节点离线，备份任务未下发")
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
	nodeName := task.NodeID
	for _, node := range state.Nodes {
		if node.ID == task.NodeID {
			nodeName = nodeDisplayName(node)
			break
		}
	}
	command := model.Command{ID: id.New("cmd"), NodeID: task.NodeID, Type: "backup", Task: task, Payload: map[string]any{"repository_id": task.RepositoryID, "node_name": nodeName}, CreatedAt: time.Now().UTC()}
	operation := model.Operation{ID: id.New("op"), Type: "backup", NodeID: task.NodeID, Status: "queued", Message: task.Name, CreatedAt: command.CreatedAt}
	command.Payload["operation_id"] = operation.ID
	queuedCommand, err := cloneCommand(command)
	if err != nil {
		return model.Command{}, err
	}
	err = s.store.Update(func(st *model.State) error {
		st.Commands = append(st.Commands, queuedCommand)
		st.Operations = append([]model.Operation{operation}, st.Operations...)
		if len(st.Operations) > 200 {
			st.Operations = st.Operations[:200]
		}
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

func nodeIsOnline(state model.State, nodeID string, now time.Time) bool {
	for _, node := range state.Nodes {
		if node.ID == nodeID {
			return !node.LastSeen.IsZero() && now.Sub(node.LastSeen) <= 90*time.Second
		}
	}
	return false
}

func expireQueuedCommands(state *model.State, now time.Time, timeout time.Duration) {
	kept := state.Commands[:0]
	for _, command := range state.Commands {
		if command.ClaimedAt.IsZero() && now.Sub(command.CreatedAt) > timeout {
			operationID, _ := command.Payload["operation_id"].(string)
			for i := range state.Operations {
				if state.Operations[i].ID == operationID {
					state.Operations[i].Status = "failed"
					state.Operations[i].Message = "节点未领取任务，排队超时"
					state.Operations[i].CompletedAt = now
				}
			}
			if command.Task != nil {
				for i := range state.Tasks {
					if state.Tasks[i].ID == command.Task.ID {
						state.Tasks[i].LastStatus = "failed"
						state.Tasks[i].LastMessage = "节点未领取任务，排队超时"
					}
				}
			}
			continue
		}
		kept = append(kept, command)
	}
	state.Commands = kept
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
