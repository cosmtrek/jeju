package contextmgr

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"jeju/internal/model"
)

const (
	DefaultRecentBlocks        = 4
	DefaultToolResultMaxTokens = 512
	EstimatorName              = "chars_div_3_with_chat_overhead"
)

type Options struct {
	ContextWindow    int
	MaxOutputTokens  int
	Threshold        float64
	CorrectionFactor float64
	RecentBlocks     int
	ToolResultTokens int
}

type SummaryRequest struct {
	PreviousSummary string
	Messages        []model.Message
}

type Result struct {
	Request        model.Request
	StateMessages  []model.Message
	Summary        string
	PendingSummary *SummaryRequest
	Report         Report
	plan           *plan
}

type Report struct {
	Estimator           string   `json:"estimator"`
	CorrectionFactor    float64  `json:"correction_factor"`
	ContextWindow       int      `json:"context_window"`
	MaxOutputTokens     int      `json:"max_output_tokens"`
	EffectiveInputLimit int      `json:"effective_input_limit"`
	Threshold           float64  `json:"threshold"`
	ThresholdTokens     int      `json:"threshold_tokens"`
	BeforeTokens        int      `json:"before_tokens"`
	BeforeRawTokens     int      `json:"before_raw_tokens"`
	AfterTokens         int      `json:"after_tokens"`
	AfterRawTokens      int      `json:"after_raw_tokens"`
	Compressed          bool     `json:"compressed"`
	Strategies          []string `json:"strategies,omitempty"`
	PreservedBlocks     int      `json:"preserved_blocks,omitempty"`
	TruncatedToolResult int      `json:"truncated_tool_results,omitempty"`
	SummaryChanged      bool     `json:"summary_changed,omitempty"`
	MessageCountBefore  int      `json:"message_count_before"`
	MessageCountAfter   int      `json:"message_count_after"`
}

type plan struct {
	Prefix       []model.Message
	Summary      string
	RecentBlocks []block
}

type OverflowError struct {
	AfterTokens         int
	EffectiveInputLimit int
	ContextWindow       int
	MaxOutputTokens     int
}

func (e OverflowError) Error() string {
	if e.EffectiveInputLimit <= 0 {
		return fmt.Sprintf("max output tokens %d leave no input budget in context window %d", e.MaxOutputTokens, e.ContextWindow)
	}
	return fmt.Sprintf("estimated input tokens %d exceed effective input limit %d after compression", e.AfterTokens, e.EffectiveInputLimit)
}

func IsOverflow(err error) bool {
	_, ok := err.(OverflowError)
	return ok
}

func Prepare(req model.Request, stateMessages []model.Message, summary string, opts Options) (Result, error) {
	opts = normalizeOptions(opts)
	effectiveLimit := effectiveInputLimit(opts)
	report := Report{
		Estimator:           EstimatorName,
		CorrectionFactor:    opts.CorrectionFactor,
		ContextWindow:       opts.ContextWindow,
		MaxOutputTokens:     opts.MaxOutputTokens,
		EffectiveInputLimit: effectiveLimit,
		Threshold:           opts.Threshold,
		ThresholdTokens:     thresholdTokens(opts),
		MessageCountBefore:  len(stateMessages),
	}

	req = RequestWithSummary(req, stateMessages, summary)
	report.BeforeRawTokens = EstimateRequestTokensRaw(req)
	report.BeforeTokens = applyCorrection(report.BeforeRawTokens, opts.CorrectionFactor)
	if opts.ContextWindow <= 0 || report.BeforeTokens <= report.ThresholdTokens {
		report.AfterTokens = report.BeforeTokens
		report.AfterRawTokens = report.BeforeRawTokens
		report.MessageCountAfter = len(stateMessages)
		result := Result{Request: req, StateMessages: stateMessages, Summary: summary, Report: report}
		if err := Overflow(result, opts); err != nil {
			return result, err
		}
		return result, nil
	}

	prefixLen := len(req.Messages) - len(stateMessages)
	if strings.TrimSpace(summary) != "" {
		prefixLen--
	}
	prefix := append([]model.Message(nil), req.Messages[:prefixLen]...)
	working := cloneMessages(stateMessages)
	blocks := splitBlocks(working)
	recentStart := len(blocks) - opts.RecentBlocks
	if recentStart < 0 {
		recentStart = 0
	}
	recentStart = validRecentStart(blocks, recentStart)
	report.PreservedBlocks = len(blocks) - recentStart

	truncated := truncateOlderToolResults(blocks[:recentStart], opts.ToolResultTokens)
	if truncated > 0 {
		report.Strategies = append(report.Strategies, "tool_result_truncate")
		report.TruncatedToolResult = truncated
	}
	working = flattenBlocks(blocks)
	req.Messages = buildMessages(prefix, summary, working)
	afterToolTruncate := applyCorrection(EstimateRequestTokensRaw(req), opts.CorrectionFactor)

	if afterToolTruncate > report.ThresholdTokens && recentStart > 0 && len(blocks) > opts.RecentBlocks {
		oldBlocks := blocks[:recentStart]
		recentBlocks := blocks[recentStart:]
		return Result{
			Request:       req,
			StateMessages: working,
			Summary:       summary,
			PendingSummary: &SummaryRequest{
				PreviousSummary: summary,
				Messages:        flattenBlocks(oldBlocks),
			},
			Report: report,
			plan: &plan{
				Prefix:       prefix,
				Summary:      summary,
				RecentBlocks: recentBlocks,
			},
		}, nil
	}

	result := finalize(req, prefix, summary, working, report, opts)
	if err := Overflow(result, opts); err != nil {
		return result, err
	}
	return result, nil
}

func CompleteSummary(result Result, summary string, opts Options) Result {
	opts = normalizeOptions(opts)
	if result.plan == nil {
		return result
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		summary = result.plan.Summary
	}
	working := flattenBlocks(result.plan.RecentBlocks)
	req := result.Request
	req.Messages = buildMessages(result.plan.Prefix, summary, working)
	result.Request = req
	result.StateMessages = working
	result.Summary = summary
	result.PendingSummary = nil
	result.plan = nil
	result.Report.Strategies = append(result.Report.Strategies, "summary")
	result.Report.SummaryChanged = true
	return finalizeResult(result, opts)
}

func DegradeSummary(result Result, opts Options) Result {
	opts = normalizeOptions(opts)
	if result.plan == nil {
		return result
	}
	working := flattenBlocks(result.plan.RecentBlocks)
	req := result.Request
	req.Messages = buildMessages(result.plan.Prefix, result.plan.Summary, working)
	result.Request = req
	result.StateMessages = working
	result.Summary = result.plan.Summary
	result.PendingSummary = nil
	result.plan = nil
	result.Report.Strategies = append(result.Report.Strategies, "drop_evicted")
	return finalizeResult(result, opts)
}

func finalize(req model.Request, _ []model.Message, summary string, working []model.Message, report Report, opts Options) Result {
	return finalizeResult(Result{
		Request:       req,
		StateMessages: working,
		Summary:       summary,
		Report:        report,
	}, opts)
}

func finalizeResult(result Result, opts Options) Result {
	truncated := 0
	afterSummary := applyCorrection(EstimateRequestTokensRaw(result.Request), opts.CorrectionFactor)
	if afterSummary > result.Report.ThresholdTokens {
		recentBlocks := splitBlocks(result.StateMessages)
		truncated = truncateOlderToolResults(recentBlocks, opts.ToolResultTokens)
		if truncated > 0 {
			result.Report.Strategies = append(result.Report.Strategies, "recent_tool_result_truncate")
			result.Report.TruncatedToolResult += truncated
			result.StateMessages = flattenBlocks(recentBlocks)
			result.Request.Messages = rebuildMessagesWithState(result.Request.Messages, result.StateMessages, result.Summary)
		}
	}

	result.Report.AfterRawTokens = EstimateRequestTokensRaw(result.Request)
	result.Report.AfterTokens = applyCorrection(result.Report.AfterRawTokens, opts.CorrectionFactor)
	result.Report.MessageCountAfter = len(result.StateMessages)
	result.Report.Compressed = len(result.Report.Strategies) > 0
	return result
}

func Overflow(result Result, opts Options) error {
	opts = normalizeOptions(opts)
	effectiveLimit := effectiveInputLimit(opts)
	if opts.ContextWindow > 0 && result.Report.AfterTokens > effectiveLimit {
		return OverflowError{
			AfterTokens:         result.Report.AfterTokens,
			EffectiveInputLimit: effectiveLimit,
			ContextWindow:       opts.ContextWindow,
			MaxOutputTokens:     opts.MaxOutputTokens,
		}
	}
	return nil
}

func EstimateRequestTokens(req model.Request, correctionFactor float64) int {
	return applyCorrection(EstimateRequestTokensRaw(req), correctionFactor)
}

func EstimateRequestTokensRaw(req model.Request) int {
	tokens := 3
	for _, message := range req.Messages {
		tokens += EstimateMessageTokens(message)
	}
	for _, tool := range req.Tools {
		tokens += estimateJSONTokens(tool) + 8
	}
	if req.ResponseFormat != nil {
		tokens += estimateJSONTokens(req.ResponseFormat) + 8
	}
	return tokens
}

func EstimateMessageTokens(message model.Message) int {
	tokens := 4
	tokens += estimateTextTokens(message.Role)
	tokens += estimateTextTokens(message.Content)
	tokens += estimateTextTokens(message.ReasoningContent)
	tokens += estimateTextTokens(message.ToolCallID)
	for _, call := range message.ToolCalls {
		tokens += estimateTextTokens(call.ID) + estimateTextTokens(call.Name) + estimateTextTokens(string(call.Arguments)) + 8
	}
	return tokens
}

func UpdateCorrectionFactor(previous float64, rawEstimatedInputTokens, actualInputTokens int) float64 {
	if rawEstimatedInputTokens <= 0 || actualInputTokens <= 0 {
		if previous <= 0 {
			return 1
		}
		return previous
	}
	if previous <= 0 {
		previous = 1
	}
	ratio := float64(actualInputTokens) / float64(rawEstimatedInputTokens)
	if ratio < 0.5 {
		ratio = 0.5
	}
	if ratio > 2 {
		ratio = 2
	}
	return previous*0.8 + ratio*0.2
}

func normalizeOptions(opts Options) Options {
	if opts.Threshold <= 0 || opts.Threshold > 1 {
		opts.Threshold = 0.8
	}
	if opts.CorrectionFactor <= 0 {
		opts.CorrectionFactor = 1
	}
	if opts.MaxOutputTokens < 0 {
		opts.MaxOutputTokens = 0
	}
	if opts.RecentBlocks <= 0 {
		opts.RecentBlocks = DefaultRecentBlocks
	}
	if opts.ToolResultTokens <= 0 {
		opts.ToolResultTokens = DefaultToolResultMaxTokens
	}
	return opts
}

func thresholdTokens(opts Options) int {
	limit := effectiveInputLimit(opts)
	if limit <= 0 {
		return 0
	}
	return int(math.Floor(float64(limit) * opts.Threshold))
}

func effectiveInputLimit(opts Options) int {
	if opts.ContextWindow <= 0 {
		return 0
	}
	limit := opts.ContextWindow - opts.MaxOutputTokens
	if limit < 0 {
		return 0
	}
	return limit
}

func applyCorrection(rawTokens int, correctionFactor float64) int {
	if correctionFactor <= 0 {
		correctionFactor = 1
	}
	return int(math.Ceil(float64(rawTokens) * correctionFactor))
}

func RequestWithSummary(req model.Request, stateMessages []model.Message, summary string) model.Request {
	prefixLen := len(req.Messages) - len(stateMessages)
	if prefixLen < 0 {
		prefixLen = 0
	}
	if prefixLen > len(req.Messages) {
		prefixLen = len(req.Messages)
	}
	req.Messages = buildMessages(req.Messages[:prefixLen], summary, stateMessages)
	return req
}

func rebuildMessagesWithState(messages []model.Message, stateMessages []model.Message, summary string) []model.Message {
	prefixLen := len(messages) - len(stateMessages)
	if strings.TrimSpace(summary) != "" {
		prefixLen--
	}
	if prefixLen < 0 {
		prefixLen = 0
	}
	if prefixLen > len(messages) {
		prefixLen = len(messages)
	}
	return buildMessages(messages[:prefixLen], summary, stateMessages)
}

func buildMessages(prefix []model.Message, summary string, stateMessages []model.Message) []model.Message {
	messages := append([]model.Message(nil), prefix...)
	if strings.TrimSpace(summary) != "" {
		messages = append(messages, model.Message{Role: "user", Content: "# Conversation Summary\n" + strings.TrimSpace(summary)})
	}
	messages = append(messages, stateMessages...)
	return messages
}

func cloneMessages(messages []model.Message) []model.Message {
	out := make([]model.Message, len(messages))
	copy(out, messages)
	for i := range out {
		if len(out[i].ToolCalls) > 0 {
			out[i].ToolCalls = append([]model.ToolCall(nil), out[i].ToolCalls...)
		}
	}
	return out
}

type block struct {
	Messages []model.Message
}

func splitBlocks(messages []model.Message) []block {
	var blocks []block
	for i := 0; i < len(messages); i++ {
		current := block{Messages: []model.Message{messages[i]}}
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			need := map[string]bool{}
			for _, call := range messages[i].ToolCalls {
				if call.ID != "" {
					need[call.ID] = true
				}
			}
			for i+1 < len(messages) && messages[i+1].Role == "tool" {
				i++
				current.Messages = append(current.Messages, messages[i])
				delete(need, messages[i].ToolCallID)
				if len(need) == 0 {
					break
				}
			}
		}
		blocks = append(blocks, current)
	}
	return blocks
}

func validRecentStart(blocks []block, start int) int {
	if start < 0 {
		start = 0
	}
	if start >= len(blocks) {
		return len(blocks)
	}
	// Prefer a user boundary, but native tool-calling loops may have recent
	// windows made only of assistant/tool blocks. Keep that full window; the
	// summary message supplies the user boundary when older blocks are evicted.
	for i := start; i < len(blocks); i++ {
		if len(blocks[i].Messages) > 0 && blocks[i].Messages[0].Role == "user" {
			return i
		}
	}
	return start
}

func flattenBlocks(blocks []block) []model.Message {
	var messages []model.Message
	for _, block := range blocks {
		messages = append(messages, block.Messages...)
	}
	return messages
}

func truncateOlderToolResults(blocks []block, maxTokens int) int {
	count := 0
	for bi := range blocks {
		for mi := range blocks[bi].Messages {
			message := &blocks[bi].Messages[mi]
			if !isToolResult(message) || EstimateMessageTokens(*message) <= maxTokens {
				continue
			}
			message.Content = truncateText(message.Content, maxTokens)
			count++
		}
	}
	return count
}

func isToolResult(message *model.Message) bool {
	if message.Role == "tool" {
		return true
	}
	return message.Role == "user" && strings.HasPrefix(message.Content, "Observation: Tool ")
}

func truncateText(text string, maxTokens int) string {
	maxChars := maxTokens * 3
	if maxChars < 128 {
		maxChars = 128
	}
	if utf8.RuneCountInString(text) <= maxChars {
		return text
	}
	runes := []rune(text)
	head := maxChars * 2 / 3
	tail := maxChars / 3
	if head+tail >= len(runes) {
		return text
	}
	return string(runes[:head]) + fmt.Sprintf("\n\n[Jeju truncated tool result: omitted approximately %d characters]\n\n", len(runes)-head-tail) + string(runes[len(runes)-tail:])
}

func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	return int(math.Ceil(float64(len([]rune(text))) / 3.0))
}

func estimateJSONTokens(value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return estimateTextTokens(string(data))
}
