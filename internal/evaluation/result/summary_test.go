package result

import "testing"

func absFloat(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestSummarize(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := Summarize(nil, 0, 3)
		if s == nil || s.NumRuns != 3 || s.PassRate != 0 || s.AvgScore != 0 {
			t.Fatalf("empty summary wrong: %+v", s)
		}
	})
	t.Run("pass rate and avg score", func(t *testing.T) {
		results := []*CaseResult{
			{Status: StatusPassed, MetricResults: []*MetricResult{{Score: 1.0}, {Score: 0.8}}},
			{Status: StatusFailed, MetricResults: []*MetricResult{{Score: 0.4}}},
			{Status: StatusError},
		}
		s := Summarize(results, 3, 2)
		// 1 of 3 passed => 1/3
		if s.PassRate != 1.0/3.0 {
			t.Fatalf("PassRate = %v, want %v", s.PassRate, 1.0/3.0)
		}
		// scores: 1.0 + 0.8 + 0.4 = 2.2 over 3 metrics
		wantAvg := 2.2 / 3.0
		if absFloat(s.AvgScore-wantAvg) > 1e-9 {
			t.Fatalf("AvgScore = %v, want %v", s.AvgScore, wantAvg)
		}
		if s.TotalCases != 3 || s.NumRuns != 2 {
			t.Fatalf("counts wrong: %+v", s)
		}
	})
	t.Run("error case contributes no metric scores", func(t *testing.T) {
		results := []*CaseResult{
			{Status: StatusPassed, MetricResults: []*MetricResult{{Score: 1.0}}},
			{Status: StatusError},
		}
		s := Summarize(results, 2, 1)
		if s.PassRate != 0.5 {
			t.Fatalf("PassRate = %v, want 0.5", s.PassRate)
		}
		if s.AvgScore != 1.0 {
			t.Fatalf("AvgScore = %v, want 1.0", s.AvgScore)
		}
	})
}
