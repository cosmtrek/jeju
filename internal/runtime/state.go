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
	RunDir    string
	Agent     string
	Input     string
	Status    RunStatus
	Step      int
	StartedAt time.Time
	EndedAt   *time.Time

	Messages       []model.Message
	PrefixMessages []model.Message
	Summary        string
	Observations   []string
	Errors         []RunError
	Final          string
	FinalRef       string

	ModelCalls                  int
	ToolCalls                   int
	ModelErrors                 int
	ToolErrors                  int
	PermissionDenied            int
	ChildRuns                   int
	ChildRunErrors              int
	ChildModelCalls             int
	ChildToolCalls              int
	ChildModelErrors            int
	ChildToolErrors             int
	ChildPermissionDenied       int
	ChildPromptTokens           int
	ChildPromptCacheHitTokens   int
	ChildCompletionTokens       int
	ChildTotalTokens            int
	PromptTokens                int
	PromptCacheHitTokens        int
	CompletionTokens            int
	TotalTokens                 int
	ConsecutiveErrors           int
	LastTokenEstimate           int
	LastRawTokenEstimate        int
	TokenCorrectionFactor       float64
	ToolBudgetFinalTried        bool
	FinalValidationRetries      int
	FinalValidationRetryPending bool
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

func NewRunStateWithMessages(runID, agent, input string, prefixMessages []model.Message, messages []model.Message) *RunState {
	return &RunState{
		RunID:                 runID,
		Agent:                 agent,
		Input:                 input,
		Status:                StatusRunning,
		StartedAt:             time.Now(),
		PrefixMessages:        append([]model.Message(nil), prefixMessages...),
		Messages:              append([]model.Message(nil), messages...),
		TokenCorrectionFactor: 1,
	}
}

func (s *RunState) RecordUsage(usage model.Usage) {
	s.PromptTokens += usage.InputTokens
	s.PromptCacheHitTokens += usage.CacheHitTokens
	s.CompletionTokens += usage.OutputTokens
	s.TotalTokens += usage.TotalTokens
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
