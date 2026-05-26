package agent

// AddCheckpoint records a new timeline entry on the Run, stamping it with the
// current time and trimming step/message to their UI-safe bounds. The
// checkpoint slice is capped at 50 entries to keep persisted runs bounded.
func (run *Run) AddCheckpoint(status RunStatus, step string, message string) {
	run.Checkpoints = append(run.Checkpoints, Checkpoint{
		At:      nowRFC3339Nano(),
		Status:  status,
		Step:    truncateString(step, 120),
		Message: truncateString(message, 1000),
	})
	if len(run.Checkpoints) > 50 {
		run.Checkpoints = append([]Checkpoint(nil), run.Checkpoints[len(run.Checkpoints)-50:]...)
	}
}

// View returns the client-safe RunView projection of the Run, copying slices
// so downstream mutations cannot reach back into the persisted Run.
func (run Run) View() RunView {
	var waitingOn *RunQuestionRef
	if run.WaitingOn != nil {
		waitingOnCopy := *run.WaitingOn
		waitingOn = &waitingOnCopy
	}
	return RunView{
		RunID:               run.RunID,
		CardID:              run.CardID,
		JiraIssueKey:        run.JiraIssueKey,
		CardTitle:           run.CardTitle,
		Objective:           run.Objective,
		RequestedBy:         run.RequestedBy,
		RetryOf:             run.RetryOf,
		AgentProfile:        run.AgentProfile,
		RequestType:         run.RequestType,
		Specialist:          run.Specialist,
		Status:              run.Status,
		CurrentStep:         run.CurrentStep,
		Repo:                run.Repo,
		Branch:              run.Branch,
		PullRequestNumber:   run.PullRequestNumber,
		PullRequestURL:      run.PullRequestURL,
		PMModel:             run.PMModel,
		ReviewModel:         run.ReviewModel,
		Classification:      run.Classification,
		ReviewLens:          run.ReviewLens,
		FindingCount:        len(run.Findings),
		Findings:            append([]CodeReviewFinding(nil), run.Findings...),
		Summary:             run.Summary,
		PublishWarnings:     append([]string(nil), run.PublishWarnings...),
		CostBudgetCents:     run.CostBudgetCents,
		EstimatedCostCents:  run.EstimatedCostCents,
		ModelCalls:          run.ModelCalls,
		JiraCommentPosted:   run.JiraCommentPosted,
		PRReviewPosted:      run.PRReviewPosted,
		Error:               run.Error,
		Checkpoints:         append([]Checkpoint(nil), run.Checkpoints...),
		Plan:                append([]PlanStep(nil), run.Plan...),
		Cost:                run.Cost.Clone(),
		WaitingOn:           waitingOn,
		SequenceNumberStart: run.SequenceNumberStart,
		SequenceNumberEnd:   run.SequenceNumberEnd,
		CreatedAt:           run.CreatedAt,
		UpdatedAt:           run.UpdatedAt,
		StartedAt:           run.StartedAt,
		CompletedAt:         run.CompletedAt,
	}
}

// Clone returns a deep copy of the cost breakdown so callers can mutate it
// without bleeding back into the persisted Run.
func (cost CostBreakdown) Clone() CostBreakdown {
	out := cost
	if len(cost.ByModel) > 0 {
		out.ByModel = make(map[string]int, len(cost.ByModel))
		for k, v := range cost.ByModel {
			out.ByModel[k] = v
		}
	}
	return out
}
