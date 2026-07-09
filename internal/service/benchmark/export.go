package benchmark

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// ExportRun writes a run to <dir>/<id>.json (the raw run) and <dir>/<id>.csv
// (one row per problem) for external analysis, returning the two paths. A CSV
// write failure removes the JSON so no half-written export is left behind.
func ExportRun(run Run, dir string) (jsonPath, csvPath string, err error) {
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create export dir: %w", err)
	}
	jsonPath = filepath.Join(dir, run.ID+".json")
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal run: %w", err)
	}
	if err = os.WriteFile(jsonPath, raw, 0o644); err != nil {
		return "", "", fmt.Errorf("write json: %w", err)
	}

	csvPath = filepath.Join(dir, run.ID+".csv")
	if err = writeRunCSV(csvPath, run); err != nil {
		os.Remove(jsonPath) // don't leave a half-written export behind
		return "", "", err
	}
	return jsonPath, csvPath, nil
}

// writeRunCSV writes one row per problem to path. On any error after the file is
// created it removes the partial file, so no half-written CSV survives a failed
// export (BR13).
func writeRunCSV(path string, r Run) (err error) {
	f, cerr := os.Create(path)
	if cerr != nil {
		return fmt.Errorf("create csv: %w", cerr)
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(path)
		}
	}()
	w := csv.NewWriter(f)
	header := []string{
		"problemId", "problemName", "resolved", "score", "ttftMs", "totalMs",
		"tokensPerSecond", "promptProcessingTps", "decodeTps", "promptTokens",
		"completionTokens", "detail", "err",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for _, pr := range r.Problems {
		row := []string{
			pr.ProblemID, pr.ProblemName, strconv.FormatBool(pr.Resolved),
			strconv.FormatFloat(pr.Score, 'f', 4, 64),
			strconv.FormatInt(pr.TTFTms, 10), strconv.FormatInt(pr.TotalMs, 10),
			strconv.FormatFloat(pr.TokensPerSecond, 'f', 2, 64),
			strconv.FormatFloat(pr.PromptProcessingTPS, 'f', 2, 64),
			strconv.FormatFloat(pr.DecodeTPS, 'f', 2, 64),
			strconv.Itoa(pr.PromptTokens), strconv.Itoa(pr.CompletionTokens),
			pr.Detail, pr.Err,
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}
