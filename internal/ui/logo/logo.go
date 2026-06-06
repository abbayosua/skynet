package logo

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/abbayosua/skynet/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/common-nighthawk/go-figure"
)

const diag = `╱`

type Opts struct {
	FieldColor   color.Color
	TitleColorA  color.Color
	TitleColorB  color.Color
	CharmColor   color.Color
	VersionColor color.Color
	Width        int
	Hyper        bool
	Unstable     bool
}

func Render(base lipgloss.Style, version string, compact bool, o Opts) string {
	fg := func(c color.Color, s string) string {
		return lipgloss.NewStyle().Foreground(c).Render(s)
	}

	name := "SKYNET"
	if o.Hyper {
		name = "HYPERCRUSH"
	}

	font := "big"
	if compact {
		font = "small"
	}

	art := figure.NewFigure(name, font, true).String()
	artWidth := lipgloss.Width(strings.Split(art, "\n")[0])

	b := new(strings.Builder)
	for r := range strings.SplitSeq(art, "\n") {
		fmt.Fprintln(b, styles.ApplyForegroundGrad(base, r, o.TitleColorA, o.TitleColorB))
	}
	art = b.String()

	metaRowGap := 1
	maxVersionWidth := artWidth - metaRowGap
	version = ansi.Truncate(version, maxVersionWidth, "…")
	gap := max(0, artWidth-lipgloss.Width(version))
	metaRow := strings.Repeat(" ", gap) + fg(o.VersionColor, version)

	art = strings.TrimSpace(metaRow + "\n" + art)

	if compact {
		field := fg(o.FieldColor, strings.Repeat(diag, artWidth))
		return strings.Join([]string{field, field, art, field, ""}, "\n")
	}

	fieldHeight := lipgloss.Height(art)

	const leftWidth = 6
	leftFieldRow := fg(o.FieldColor, strings.Repeat(diag, leftWidth))
	leftField := new(strings.Builder)
	for range fieldHeight {
		fmt.Fprintln(leftField, leftFieldRow)
	}

	rightWidth := max(15, o.Width-artWidth-leftWidth-2)
	const stepDownAt = 0
	rightField := new(strings.Builder)
	for i := range fieldHeight {
		width := rightWidth
		if i >= stepDownAt {
			width = rightWidth - (i - stepDownAt)
		}
		fmt.Fprint(rightField, fg(o.FieldColor, strings.Repeat(diag, width)), "\n")
	}

	const hGap = " "
	logo := lipgloss.JoinHorizontal(lipgloss.Top, leftField.String(), hGap, art, hGap, rightField.String())
	if o.Width > 0 {
		lines := strings.Split(logo, "\n")
		for i, line := range lines {
			lines[i] = ansi.Truncate(line, o.Width, "")
		}
		logo = strings.Join(lines, "\n")
	}
	return logo
}

func SmallRender(t *styles.Styles, width int, o Opts) string {
	name := "SkyNet"
	if o.Hyper {
		name = "HYPERSKYNET"
	}
	title := styles.ApplyBoldForegroundGrad(t.Logo.GradCanvas, name, t.Logo.SmallGradFromColor, t.Logo.SmallGradToColor)
	remainingWidth := width - lipgloss.Width(title) - 1
	if remainingWidth > 0 {
		lines := strings.Repeat("╱", remainingWidth)
		title = fmt.Sprintf("%s %s", title, t.Logo.SmallDiagonals.Render(lines))
	}
	return title
}
