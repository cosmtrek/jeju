package runtime

import (
	"time"

	"github.com/cosmtrek/jeju/internal/model"
)

type RunStatus string

const (
	StatusRunning         RunStatus = "running"
	StatusWaitingUser     RunStatus = "waiting_user"
	StatusWaitingApproval RunStatus = "waiting_approval"
	StatusPaused          RunStatus = "paused"
	StatusCompleted       RunStatus = "completed"
	StatusFailed          RunStatus = "failed"
	StatusCancelled       RunStatus = "cancelled"
)

type RunState struct {
	RunID     string
	Agent     string
	Input     string
	Status    RunStatus
	Step      int
	StartedAt time.Time
	EndedAt   *time.Time

	Messages     []model.Message
	Summary      string
	Observations []string
	Errors       []RunError
	Final        string

	ModelCalls            int
	ToolCalls             int
	ModelErrors           int
	ToolErrors            int
	PermissionDenied      int
	ConsecutiveErrors     int
	LastTokenEstimate     int
	LastRawTokenEstimate  int
	TokenCorrectionFactor float64
}

type RunError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type RunResult struct {
	RunID  string
	Status RunStatus
	Final  string
}

func NewRunState(runID, agent, input string) *RunState {
	return &RunState{
		RunID:     runID,
		Agent:     agent,
		Input:     input,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		Messages: []model.Message{
			{Role: "user", Content: input},
		},
		TokenCorrectionFactor: 1,
	}
}

func (s *RunState) AddObservation(text string) {
	s.Observations = append(s.Observations, text)
	s.Messages = append(s.Messages, model.Message{Role: "user", Content: "Observation: " + text})
}

func (s *RunState) AddError(kind string, err error) {
	s.Errors = append(s.Errors, RunError{Kind: kind, Message: err.Error()})
	s.ConsecutiveErrors++
}

func (s *RunState) ResetErrors() {
	s.ConsecutiveErrors = 0
}

func (s *RunState) IsTerminal() bool {
	return s.Status == StatusCompleted || s.Status == StatusFailed || s.Status == StatusCancelled
}
