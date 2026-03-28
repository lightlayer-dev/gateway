package score

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ANSI color codes.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// Grade returns a letter grade for the score.
func Grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 50:
		return "D"
	default:
		return "F"
	}
}

// FormatReport returns a colored terminal report of the scan results.
func FormatReport(report *ScoreReport, verbose bool) string {
	var b strings.Builder

	// Header.
	scoreColor := colorGreen
	if report.Score < 80 {
		scoreColor = colorYellow
	}
	if report.Score < 50 {
		scoreColor = colorRed
	}

	fmt.Fprintf(&b, "\n%s🤖 Agent-Readiness Score: %s%d/100 (%s)%s\n",
		colorBold, scoreColor, report.Score, Grade(report.Score), colorReset)
	fmt.Fprintf(&b, "   %s%s — %dms%s\n\n", colorDim, report.URL, report.DurationMs, colorReset)

	// Check results.
	for _, c := range report.Checks {
		icon := "❌"
		color := colorRed
		switch c.Severity {
		case SeverityPass:
			icon = "✅"
			color = colorGreen
		case SeverityWarn:
			icon = "⚠️"
			color = colorYellow
		}

		fmt.Fprintf(&b, "  %s %s%s%s (%d/%d)\n", icon, color, c.Name, colorReset, c.Score, c.MaxScore)
		fmt.Fprintf(&b, "     %s\n", c.Message)

		if verbose && c.Suggestion != "" {
			fmt.Fprintf(&b, "     %s💡 %s%s\n", colorCyan, c.Suggestion, colorReset)
		}
		b.WriteString("\n")
	}

	// Quick wins.
	var quickWins []string
	for _, c := range report.Checks {
		if c.Severity != SeverityPass && c.Suggestion != "" {
			quickWins = append(quickWins, c.Suggestion)
			if len(quickWins) >= 3 {
				break
			}
		}
	}
	if len(quickWins) > 0 {
		fmt.Fprintf(&b, "%s🔧 Quick wins to improve your score:%s\n", colorBold, colorReset)
		for _, w := range quickWins {
			fmt.Fprintf(&b, "   • %s\n", w)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// FormatGatewayEstimate returns a line showing the estimated score with the gateway.
func FormatGatewayEstimate(current, estimated int) string {
	var b strings.Builder
	color := colorGreen
	if estimated < 80 {
		color = colorYellow
	}

	fmt.Fprintf(&b, "%s💡 With LightLayer Gateway, your score would be: %s%d/100 (%s)%s",
		colorBold, color, estimated, Grade(estimated), colorReset)
	if estimated > current {
		fmt.Fprintf(&b, " %s(+%d)%s", colorGreen, estimated-current, colorReset)
	}
	b.WriteString("\n\n")
	return b.String()
}

// FormatJSON returns the report as formatted JSON.
func FormatJSON(report *ScoreReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
