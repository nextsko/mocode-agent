package result

// Summarize aggregates CaseResults across multiple runs into a Summary.
//
// PassRate is the fraction of case-results with Status StatusPassed.
// AvgScore is the mean of every MetricResult.Score across all case-results.
// NumRuns is the maximum RunID seen (the caller controls run numbering);
// pass TotalCases as the number of cases in the EvalSet.
func Summarize(results []*CaseResult, totalCases int, numRuns int) *Summary {
	if len(results) == 0 {
		return &Summary{NumRuns: numRuns, TotalCases: totalCases}
	}
	passed := 0
	scoreSum := 0.0
	scoreCount := 0
	for _, cr := range results {
		if cr.Status == StatusPassed {
			passed++
		}
		for _, mr := range cr.MetricResults {
			scoreSum += mr.Score
			scoreCount++
		}
	}
	passRate := float64(passed) / float64(len(results))
	avgScore := 0.0
	if scoreCount > 0 {
		avgScore = scoreSum / float64(scoreCount)
	}
	return &Summary{
		NumRuns:     numRuns,
		TotalCases:  totalCases,
		PassRate:    passRate,
		AvgScore:    avgScore,
	}
}
