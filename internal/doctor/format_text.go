package doctor

import (
	"fmt"
	"io"
	"strings"
)

func FormatText(w io.Writer, r Report) {
	for _, res := range r.Results {
		fmt.Fprintf(w, "%s %-14s %s\n", badge(res.Status), res.Name, res.Summary)
		for _, d := range res.Details {
			fmt.Fprintf(w, "                  %s\n", d)
		}
		if res.Hint != "" && res.Status != StatusOK {
			fmt.Fprintf(w, "                  hint: %s\n", res.Hint)
		}
	}
	ok, warn, fail, skip := r.Counts()
	fmt.Fprintln(w, strings.Repeat("-", 60))
	fmt.Fprintf(w, "summary: %d ok, %d warn, %d fail, %d skip\n", ok, warn, fail, skip)
}

func badge(s Status) string {
	switch s {
	case StatusOK:
		return "[ OK ]"
	case StatusWarn:
		return "[WARN]"
	case StatusFail:
		return "[FAIL]"
	case StatusSkip:
		return "[SKIP]"
	}
	return "[ ?? ]"
}
