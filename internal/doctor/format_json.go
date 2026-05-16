package doctor

import (
	"encoding/json"
	"io"
)

type jsonResult struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Summary string   `json:"summary"`
	Details []string `json:"details,omitempty"`
	Hint    string   `json:"hint,omitempty"`
	Elapsed string   `json:"elapsed"`
}

type jsonReport struct {
	Results []jsonResult `json:"results"`
	Summary struct {
		OK   int `json:"ok"`
		Warn int `json:"warn"`
		Fail int `json:"fail"`
		Skip int `json:"skip"`
	} `json:"summary"`
	ExitCode int `json:"exit_code"`
}

func FormatJSON(w io.Writer, r Report) error {
	out := jsonReport{ExitCode: r.ExitCode()}
	out.Summary.OK, out.Summary.Warn, out.Summary.Fail, out.Summary.Skip = r.Counts()
	for _, res := range r.Results {
		out.Results = append(out.Results, jsonResult{
			Name:    res.Name,
			Status:  res.Status.String(),
			Summary: res.Summary,
			Details: res.Details,
			Hint:    res.Hint,
			Elapsed: res.Elapsed.String(),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
