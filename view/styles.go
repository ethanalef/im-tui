package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// btop-inspired color palette: dark bg, green/yellow/red gradients, muted borders
var (
	// Base colors - btop dark theme
	ColorBg      = lipgloss.Color("#0a0a0f") // near-black background
	ColorSurface = lipgloss.Color("#232336") // panel interior
	ColorBorder  = lipgloss.Color("#3b3b5c") // muted border lines
	ColorText    = lipgloss.Color("#ccccdc") // primary text
	ColorSubtext = lipgloss.Color("#7a7a8c") // dimmed labels
	ColorTitle   = lipgloss.Color("#60a0dc") // panel titles in border (btop blue)

	// Accent colors
	ColorGreen  = lipgloss.Color("#30d158") // healthy / low usage
	ColorYellow = lipgloss.Color("#ffd60a") // warning / medium usage
	ColorOrange = lipgloss.Color("#ff9f0a") // elevated
	ColorRed    = lipgloss.Color("#ff453a") // critical / high usage
	ColorCyan   = lipgloss.Color("#64d2ff") // info highlights
	ColorPurple = lipgloss.Color("#bf5af2") // special accents

	// Gradient stops for progress bars (btop-style green→yellow→red)
	barGradient = []lipgloss.Color{
		lipgloss.Color("#30d158"), // 0-15%
		lipgloss.Color("#30d158"), // 15-30%
		lipgloss.Color("#7cd160"), // 30-45%
		lipgloss.Color("#b8d148"), // 45-55%
		lipgloss.Color("#ffd60a"), // 55-65%
		lipgloss.Color("#ffb30a"), // 65-75%
		lipgloss.Color("#ff9f0a"), // 75-85%
		lipgloss.Color("#ff6b3a"), // 85-92%
		lipgloss.Color("#ff453a"), // 92-100%
		lipgloss.Color("#ff453a"), // overflow
	}

	// Tab bar - btop style: active tab has bright text, inactive is dim
	TabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#0a0a0f")).
			Background(ColorTitle).
			Padding(0, 1)

	TabInactive = lipgloss.NewStyle().
			Foreground(ColorSubtext).
			Padding(0, 1)

	// Status bar
	StatusBar = lipgloss.NewStyle().
			Foreground(ColorSubtext)

	StatusOK = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	StatusErr = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)

	StatusNA = lipgloss.NewStyle().
			Foreground(ColorSubtext)

	// Panel - rounded borders like btop
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder)

	PanelTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTitle)

	// Values
	ValueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText)

	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorSubtext)

	// Alert styles
	AlertCritical = lipgloss.NewStyle().
			Foreground(ColorRed).
			Bold(true)

	AlertWarning = lipgloss.NewStyle().
			Foreground(ColorYellow).
			Bold(true)

	// Help overlay
	HelpStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorTitle).
			Padding(1, 2).
			Width(50)

	// Table header
	TableHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(ColorBorder)

	// Table row
	TableRow = lipgloss.NewStyle().
			Foreground(ColorText)

	TableRowDim = lipgloss.NewStyle().
			Foreground(ColorSubtext)
)

// Panel renders a btop-style panel with title embedded in the top border.
// Like: ╭─┤ title ├──────────────╮
func Panel(title string, content string, width, height int) string {
	titleStr := ""
	if title != "" {
		titleStr = lipgloss.NewStyle().Foreground(ColorBorder).Render("┤") +
			PanelTitle.Render(" "+title+" ") +
			lipgloss.NewStyle().Foreground(ColorBorder).Render("├")
	}

	// Calculate inner width
	innerWidth := width - 2 // borders
	if innerWidth < 1 {
		innerWidth = 1
	}

	// Build top border with embedded title
	topLeft := lipgloss.NewStyle().Foreground(ColorBorder).Render("╭")
	topRight := lipgloss.NewStyle().Foreground(ColorBorder).Render("╮")
	botLeft := lipgloss.NewStyle().Foreground(ColorBorder).Render("╰")
	botRight := lipgloss.NewStyle().Foreground(ColorBorder).Render("╯")
	hLine := lipgloss.NewStyle().Foreground(ColorBorder).Render("─")
	vLine := lipgloss.NewStyle().Foreground(ColorBorder).Render("│")

	// Top border
	titleVisualLen := 0
	if title != "" {
		titleVisualLen = lipgloss.Width(titleStr)
	}
	remainingWidth := innerWidth - titleVisualLen
	if remainingWidth < 0 {
		remainingWidth = 0
	}
	leftPad := 1
	rightPad := remainingWidth - leftPad
	if rightPad < 0 {
		rightPad = 0
		leftPad = remainingWidth
	}
	topBorder := topLeft + strings.Repeat("─", leftPad) + titleStr + repeatStr(hLine, rightPad) + topRight

	// Bottom border
	bottomBorder := botLeft + strings.Repeat("─", innerWidth) + botRight

	// Content lines
	contentLines := strings.Split(content, "\n")

	// Ensure we fill to height
	innerHeight := height - 2 // top + bottom borders
	if innerHeight < 0 {
		innerHeight = 0
	}
	for len(contentLines) < innerHeight {
		contentLines = append(contentLines, "")
	}
	if len(contentLines) > innerHeight && innerHeight > 0 {
		contentLines = contentLines[:innerHeight]
	}

	// Render body lines
	var lines []string
	lines = append(lines, topBorder)
	for _, cl := range contentLines {
		lineWidth := lipgloss.Width(cl)
		padding := innerWidth - lineWidth
		if padding < 0 {
			padding = 0
		}
		lines = append(lines, vLine+cl+strings.Repeat(" ", padding)+vLine)
	}
	lines = append(lines, bottomBorder)

	return strings.Join(lines, "\n")
}

// ProgressBar renders a btop-style gradient progress bar.
func ProgressBar(percent float64, width int) string {
	if width < 2 {
		width = 2
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	filled := int(percent / 100 * float64(width))
	if filled > width {
		filled = width
	}

	var bar string
	for i := 0; i < filled; i++ {
		// Each character gets a color based on its position
		pos := float64(i) / float64(width) * float64(len(barGradient)-1)
		idx := int(pos)
		if idx >= len(barGradient) {
			idx = len(barGradient) - 1
		}
		bar += lipgloss.NewStyle().Foreground(barGradient[idx]).Render("█")
	}

	// Empty portion
	empty := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("░", width-filled))

	return bar + empty
}

// ProgressBarLabeled renders "Label: [████░░░░] XX%"
func ProgressBarLabeled(label string, percent float64, barWidth int, totalWidth int) string {
	pctStr := fmt.Sprintf("%5.1f%%", percent)

	// Color the percentage
	var pctColor lipgloss.Color
	switch {
	case percent >= 80:
		pctColor = ColorRed
	case percent >= 60:
		pctColor = ColorYellow
	default:
		pctColor = ColorGreen
	}
	pctStyled := lipgloss.NewStyle().Foreground(pctColor).Bold(true).Render(pctStr)
	labelStyled := LabelStyle.Render(label)

	return fmt.Sprintf("%s %s %s", labelStyled, ProgressBar(percent, barWidth), pctStyled)
}

// SparklineStr renders a text sparkline with btop-like green coloring.
func SparklineStr(values []float64, width int) string {
	if len(values) == 0 {
		return lipgloss.NewStyle().Foreground(ColorSubtext).Render("─")
	}

	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Use last `width` values
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	vals := values[start:]

	// Find min/max
	min, max := vals[0], vals[0]
	for _, v := range vals {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	result := ""
	rng := max - min
	for _, v := range vals {
		idx := 0
		if rng > 0 {
			idx = int((v - min) / rng * float64(len(blocks)-1))
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		if idx < 0 {
			idx = 0
		}

		// Color based on relative height (btop uses green for graphs)
		var color lipgloss.Color
		ratio := float64(idx) / float64(len(blocks)-1)
		switch {
		case ratio > 0.75:
			color = ColorGreen
		case ratio > 0.5:
			color = lipgloss.Color("#7cd160")
		case ratio > 0.25:
			color = lipgloss.Color("#5ca04c")
		default:
			color = lipgloss.Color("#3a6b38")
		}
		result += lipgloss.NewStyle().Foreground(color).Render(string(blocks[idx]))
	}

	return result
}

// StatusDot renders a colored dot indicator.
func StatusDot(ok bool) string {
	if ok {
		return StatusOK.Render("●")
	}
	return StatusErr.Render("●")
}

// FormatBytes formats bytes into human-readable form.
func FormatBytes(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", b/(1<<10))
	default:
		return fmt.Sprintf("%.0f B", b)
	}
}

// FormatNum formats a number with comma separators.
func FormatNum(n float64) string {
	if n < 0 {
		return "-" + FormatNum(-n)
	}
	s := fmt.Sprintf("%.0f", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// FormatLatency formats seconds into human-readable latency.
func FormatLatency(seconds float64) string {
	switch {
	case seconds <= 0:
		return "--"
	case seconds >= 1:
		return fmt.Sprintf("%.1fs", seconds)
	case seconds >= 0.001:
		return fmt.Sprintf("%.0fms", seconds*1000)
	default:
		return "<1ms"
	}
}

// FormatRate formats a rate value.
func FormatRate(n float64) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk/s", n/1000)
	}
	if n > 0 && n < 1 {
		return fmt.Sprintf("%.1f/s", n)
	}
	return fmt.Sprintf("%.0f/s", n)
}

func repeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

// PadRight pads a string to the given width.
func PadRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// Truncate truncates a string to max width with ellipsis.
func Truncate(s string, max int) string {
	if max <= 3 {
		return s
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	// Simple truncation
	runes := []rune(s)
	if len(runes) > max-1 {
		runes = runes[:max-1]
	}
	return string(runes) + "…"
}
