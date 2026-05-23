package benchmark

import "testing"

func TestGradeResult_Defaults(t *testing.T) {
	var r gradeResult
	if r.Score != 0 || r.Pass {
		t.Fatalf("zero gradeResult should be Score 0, Pass false, got %+v", r)
	}
}
