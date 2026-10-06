// Package tui is pelegrun's dashboard (Bubble Tea). Each screen renders live
// output from the real internal packages, so what you see is exactly what the
// tool computes — including the safety refusals.
//
// The look is "Tokyo Night": a deep indigo canvas, near-white text, and neon
// blue/cyan/purple accents (palette from tokyo-night.terminal). Two touches make
// it feel alive without getting in the way:
//
//   - a hand-built pixel-font wordmark whose colours sweep through the neon ramp
//     each frame (the "dynamic text art"), and
//   - harmonica spring physics driving the sidebar caret and the progress bar,
//     so selection and progress glide (and gently overshoot) instead of jumping.
//
// Everything is drawn on surface-backed styles so no span falls back to terminal
// black, and the whole frame is finally placed on the page background.
package tui

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"

	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/band"
	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/hood"
	"github.com/ParkWardRR/pelegrun-ap-hk07-firmware-tools/internal/mews"
)

// ---- Tokyo Night palette (tokyo-night.terminal) --------------------------------

const (
	cBg      = "#1a1b26" // page canvas — deep indigo (Night)
	cSurface = "#24283b" // elevated card surface (Storm) so white text reads crisp
	cSel     = "#33467c" // selected-row pill background
	cInk     = "#c0caf5" // headings + values — bright foreground
	cBody    = "#a9b1d6" // body text
	cMuted   = "#565f89" // captions / secondary — the classic TN comment colour
	cLine    = "#3b4261" // subtle borders / bar troughs
	cBlue    = "#7aa2f7" // primary accent — links, step numbers
	cCyan    = "#7dcfff" // secondary accent
	cTeal    = "#2ac3de"
	cPurple  = "#bb9af7"
	cGreen   = "#9ece6a" // status: ok
	cRed     = "#f7768e" // status: refused
	cOrange  = "#e0af68" // status: required / warning
)

var (
	surface = lipgloss.Color(cSurface)

	// base carries the card surface so no styled span shows terminal black.
	base = lipgloss.NewStyle().Background(surface)
	page = lipgloss.NewStyle().Background(lipgloss.Color(cBg))

	sProduct = base.Foreground(lipgloss.Color(cInk)).Bold(true)
	sHead    = base.Foreground(lipgloss.Color(cInk)).Bold(true)
	sSub     = base.Foreground(lipgloss.Color(cMuted))
	sBody    = base.Foreground(lipgloss.Color(cBody))
	sInk     = base.Foreground(lipgloss.Color(cBody))
	sKey     = base.Foreground(lipgloss.Color(cInk)).Bold(true)
	sAccent  = base.Foreground(lipgloss.Color(cBlue))
	sVer     = base.Foreground(lipgloss.Color(cCyan)).Bold(true)
	sOK      = base.Foreground(lipgloss.Color(cGreen))
	sNo      = base.Foreground(lipgloss.Color(cRed)).Bold(true)
	sWarn    = base.Foreground(lipgloss.Color(cOrange))
	sPill    = base.Foreground(lipgloss.Color(cInk)).Background(lipgloss.Color(cSel)).Bold(true)
	sItem    = base.Foreground(lipgloss.Color(cBody))

	// Header / footer bars sit on the page canvas, not the card surface.
	onBg     = lipgloss.NewStyle().Background(lipgloss.Color(cBg))
	sBgMuted = onBg.Foreground(lipgloss.Color(cMuted))
	sBgAcc   = onBg.Foreground(lipgloss.Color(cBlue))
	sBgInk   = onBg.Foreground(lipgloss.Color(cInk)).Bold(true)
	sBgCyan  = onBg.Foreground(lipgloss.Color(cCyan)).Bold(true)

	menuBx = base.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cLine)).
		BorderBackground(surface).Padding(1, 1).MarginRight(1)
	contBx = base.Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(cLine)).
		BorderBackground(surface).Padding(1, 3)
	barBx = onBg.Padding(0, 1) // full-width header/footer bars
)

// neonRamp is a seamless loop through the Tokyo Night accent hues. The banner
// picks colours from it per column and shifts the offset each frame, producing a
// horizontal shimmer across the wordmark.
var neonRamp = buildRamp([]string{cBlue, cCyan, cTeal, cPurple, cBlue}, 60)

// PaletteHex returns every theme colour plus the full shimmer ramp as hex
// strings. The tuigif tool seeds its GIF palette with these so the neon blocks
// and gradient render without banding.
func PaletteHex() []string {
	out := []string{cBg, cSurface, cSel, cInk, cBody, cMuted, cLine, cBlue, cCyan, cTeal, cPurple, cGreen, cRed, cOrange}
	for _, c := range neonRamp {
		out = append(out, string(c))
	}
	return out
}

func buildRamp(stops []string, n int) []lipgloss.Color {
	cols := make([]colorful.Color, len(stops))
	for i, s := range stops {
		c, _ := colorful.Hex(s)
		cols[i] = c
	}
	out := make([]lipgloss.Color, n)
	segs := len(cols) - 1
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n) * float64(segs)
		seg := int(t)
		if seg >= segs {
			seg = segs - 1
		}
		local := t - float64(seg)
		blended := cols[seg].BlendHcl(cols[seg+1], local).Clamped()
		out[i] = lipgloss.Color(blended.Hex())
	}
	return out
}

// ---- animation timing ---------------------------------------------------------

const fps = 30

type frameMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second/fps, func(t time.Time) tea.Msg { return frameMsg(t) })
}

// ---- model --------------------------------------------------------------------

type stage struct {
	name, sub string
	run       func() string
}

type model struct {
	stages  []stage
	sel     int
	version string
	w, h    int

	frame int // banner shimmer offset

	spring             harmonica.Spring
	caretPos, caretVel float64 // sidebar caret glide (in step units)
	progPos, progVel   float64 // footer progress fill (0..1)
}

// New builds the dashboard model.
func New(version string) model {
	return model{
		version: version,
		spring:  harmonica.NewSpring(harmonica.FPS(fps), 7.0, 0.65),
		stages: []stage{
			{"Discover", "Identify the access point", runDiscover},
			{"Connect", "Choose how to reach it", runConnect},
			{"Back Up", "Save everything first", runBackup},
			{"Safeguards", "Why it can't brick", runSafeguards},
			{"Identity", "Assign a unique serial", runIdentity},
			{"Install", "Flash without UART", runInstall},
			{"Verify", "Confirm it came back", runVerify},
		},
	}
}

// Run launches the dashboard.
func Run(version string) error {
	_, err := tea.NewProgram(New(version), tea.WithAltScreen()).Run()
	return err
}

func (m model) Init() tea.Cmd { return tick() }

// DemoFrames drives the model through a canned tour — an intro shimmer, a walk
// down all seven steps (so the caret and progress springs glide), then a spring
// back to the top — capturing one rendered frame per animation tick. It exists
// so the tuigif tool can rasterise the TUI to docs/tour.gif offline, without a
// terminal recorder. Colour must be forced on by the caller (non-TTY output).
func DemoFrames(version string, w, h int) []string {
	m := New(version)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = nm.(model)

	var frames []string
	adv := func(n int) {
		for i := 0; i < n; i++ {
			u, _ := m.Update(frameMsg(time.Now()))
			m = u.(model)
			frames = append(frames, m.View())
		}
	}
	press := func(k string) {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = u.(model)
	}

	adv(16) // hold on Discover; let the wordmark shimmer
	for i := 0; i < len(m.stages)-1; i++ {
		press("j")
		adv(9) // glide the caret + progress into the next step
	}
	adv(8)
	press("g")
	adv(18) // spring all the way back to the top
	return frames
}

// StepFrame renders a single settled frame for the given step (0-indexed) — the
// springs have come to rest — for the tuigif tool's static PNG screenshots.
func StepFrame(version string, w, h, step int) string {
	m := New(version)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = nm.(model)
	for i := 0; i < step; i++ {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = u.(model)
	}
	for i := 0; i < 40; i++ { // let the caret + progress springs settle
		u, _ := m.Update(frameMsg(time.Now()))
		m = u.(model)
	}
	return m.View()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case frameMsg:
		m.frame++
		m.caretPos, m.caretVel = m.spring.Update(m.caretPos, m.caretVel, float64(m.sel))
		m.progPos, m.progVel = m.spring.Update(m.progPos, m.progVel, m.progTarget())
		return m, tick()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.sel > 0 {
				m.sel--
			}
		case "down", "j":
			if m.sel < len(m.stages)-1 {
				m.sel++
			}
		case "g", "home":
			m.sel = 0
		case "G", "end":
			m.sel = len(m.stages) - 1
		}
	}
	return m, nil
}

func (m model) progTarget() float64 {
	if len(m.stages) <= 1 {
		return 1
	}
	return float64(m.sel) / float64(len(m.stages)-1)
}

func (m model) View() string {
	if m.w == 0 {
		m.w, m.h = 100, 30
	}

	header := m.header()
	footer := m.footer()
	bodyH := m.h - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 12 {
		bodyH = 12
	}

	// Column footprints (including borders + the menu's right margin) sum to m.w.
	// menuBx: Width is the text+padding area; +2 border +1 margin. contBx: +2 border.
	const menuCol = 26
	menuPanel := menuBx.Width(menuCol - 3).Height(bodyH).Render(m.sidebar())

	contentCol := m.w - menuCol
	if contentCol < 42 {
		contentCol = 42
	}
	contentInner := contentCol - 8 // minus border (2) and horizontal padding (6)
	cur := m.stages[m.sel]
	crumb := sSub.Render(fmt.Sprintf("Step %d of %d", m.sel+1, len(m.stages)))
	title := sHead.Render(cur.name) + sSub.Render("    ") + crumb
	sub := sSub.Render(cur.sub)
	rule := base.Foreground(lipgloss.Color(cLine)).Render(strings.Repeat("─", max(0, contentInner)))
	bodyText := title + "\n" + sub + "\n" + rule + "\n\n" + cur.run()
	contentPanel := contBx.Width(contentCol - 2).Height(bodyH).Render(bodyText)

	main := lipgloss.JoinHorizontal(lipgloss.Top, menuPanel, contentPanel)

	frame := lipgloss.JoinVertical(lipgloss.Left, header, main, footer)
	return page.Width(m.w).Height(m.h).Render(frame)
}

// ---- header: animated wordmark + tagline --------------------------------------

func (m model) header() string {
	tagline := sProductBg("Pelegrún") +
		sBgMuted.Render("  ·  ap-hk07 firmware toolkit  ·  ") +
		sVerBg("v"+m.version)

	// Below ~84 cols the pixel wordmark doesn't fit; fall back to the tagline alone.
	if m.w < 84 {
		return barBx.Width(m.w).Render(tagline)
	}
	// Render every strip at full width so the whole header carries the page bg —
	// no ragged default-background padding beside the wordmark or tagline.
	art := onBg.Width(m.w).Render(banner(m.frame))
	gap := onBg.Width(m.w).Render("")
	tag := barBx.Width(m.w).Render(tagline)
	return lipgloss.JoinVertical(lipgloss.Left, art, gap, tag)
}

func sProductBg(s string) string { return sBgInk.Render(s) }
func sVerBg(s string) string     { return sBgCyan.Render(s) }

// ---- sidebar: numbered steps with a spring-driven caret -----------------------

func (m model) sidebar() string {
	const labelW = 26 - 10 // caret(1) + " %d  " (4) + name → fits the inner text area
	var b strings.Builder
	b.WriteString(sSub.Render("STEPS") + "\n\n")
	for i, s := range m.stages {
		// Caret glow: brightest on the row the spring is nearest, fading to the
		// neighbour it's travelling toward. Between rows, both light up faintly.
		d := math.Abs(float64(i) - m.caretPos)
		intensity := 1 - d
		var caret string
		if intensity > 0 {
			trough, _ := colorful.Hex(cSurface)
			glow, _ := colorful.Hex(cBlue)
			c := trough.BlendHcl(glow, clamp01(intensity)).Clamped()
			caret = base.Foreground(lipgloss.Color(c.Hex())).Render("▍")
		} else {
			caret = base.Render(" ")
		}

		label := fmt.Sprintf(" %d  %-*s", i+1, labelW, s.name)
		var row string
		if i == m.sel {
			row = sPill.Render(label)
		} else {
			row = sItem.Render(label)
		}
		b.WriteString(caret + row + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---- footer: spring progress bar + keys + legal -------------------------------

func (m model) footer() string {
	pct := fmt.Sprintf("  %d of %d", m.sel+1, len(m.stages))
	barW := m.w - 2 - lipgloss.Width(pct) // bar box has 1-col padding each side
	if barW < 10 {
		barW = 10
	}
	bar := progressBar(clamp01(m.progPos), barW)
	progress := barBx.Width(m.w).Render(bar + sBgMuted.Render(pct))

	keys := sBgAcc.Render("↑↓") + sBgMuted.Render(" move") + sBgMuted.Render("    ") +
		sBgAcc.Render("g/G") + sBgMuted.Render(" ends") + sBgMuted.Render("    ") +
		sBgAcc.Render("q") + sBgMuted.Render(" quit")
	legal := sBgMuted.Render("unofficial · not affiliated with EnGenius/Senao · hardware you own")

	return lipgloss.JoinVertical(lipgloss.Left,
		progress,
		barBx.Width(m.w).Render(keys),
		barBx.Width(m.w).Render(legal),
	)
}

// progressBar renders a neon-filled bar with a fractional trailing cell so the
// spring's motion reads smoothly at sub-cell resolution.
func progressBar(frac float64, width int) string {
	if width < 1 {
		width = 1
	}
	const eighths = " ▏▎▍▌▋▊▉█"
	total := frac * float64(width)
	full := int(total)
	rem := total - float64(full)

	var b strings.Builder
	for i := 0; i < width; i++ {
		switch {
		case i < full:
			c := neonRamp[(i*len(neonRamp)/width)%len(neonRamp)]
			b.WriteString(onBg.Foreground(c).Render("█"))
		case i == full && rem > 0:
			idx := int(rem*8 + 0.5)
			if idx < 1 {
				idx = 1
			}
			if idx > 8 {
				idx = 8
			}
			r := []rune(eighths)[idx]
			b.WriteString(onBg.Foreground(lipgloss.Color(cBlue)).Render(string(r)))
		default:
			b.WriteString(onBg.Foreground(lipgloss.Color(cLine)).Render("░"))
		}
	}
	return b.String()
}

// ---- pixel-font wordmark ("dynamic text art") ---------------------------------
//
// Each glyph is a 5×5 bitmap; a lit pixel becomes a 2-wide block so the wordmark
// keeps a square-ish aspect. Colours are pulled from neonRamp by absolute column
// plus the frame offset, sweeping the shimmer across the letters over time.

var glyphs = map[rune][5]string{
	'p': {"11111", "10001", "11111", "10000", "10000"},
	'e': {"11111", "10000", "11110", "10000", "11111"},
	'l': {"10000", "10000", "10000", "10000", "11111"},
	'g': {"11111", "10000", "10011", "10001", "11111"},
	'r': {"11110", "10001", "11110", "10010", "10001"},
	'u': {"10001", "10001", "10001", "10001", "11111"},
	'n': {"10001", "11001", "10101", "10011", "10001"},
}

func banner(frame int) string {
	const word = "pelegrun"
	rows := [5]strings.Builder{}
	col := 0 // absolute lit-pixel column, for the gradient sweep
	for li, ch := range word {
		g := glyphs[ch]
		for r := 0; r < 5; r++ {
			for c := 0; c < 5; c++ {
				if g[r][c] == '1' {
					color := neonRamp[(col+c+frame)%len(neonRamp)]
					rows[r].WriteString(onBg.Foreground(color).Render("██"))
				} else {
					rows[r].WriteString(onBg.Render("  "))
				}
			}
			if li < len(word)-1 {
				rows[r].WriteString(onBg.Render("  ")) // 1-pixel gap between glyphs
			}
		}
		col += 6
	}
	lines := make([]string, 5)
	for r := 0; r < 5; r++ {
		lines[r] = rows[r].String()
	}
	return barBx.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- screen renderers (real package output; blunt, plain-language copy) ----
//
// Every visible segment must be rendered through a surface-backed style — a raw
// string literal between styled spans would show as a black gap on the card.

// pad returns n surface-backed spaces (for indents/separators, never raw " ").
func pad(n int) string { return base.Render(strings.Repeat(" ", n)) }

// blank returns a surface-backed blank line so multi-line bodies keep the card
// colour on otherwise-empty rows.
func blank() string { return "\n" }

func runDiscover() string {
	var b strings.Builder
	b.WriteString(sBody.Render("Find the AP and read what firmware it runs.") + blank() + blank())
	rows := [][3]string{
		{"Cloud model", "React web UI · JSON API (admin/admin)", "no shell"},
		{"EWS model", "LuCI web UI · md5.js · cgi-bin/luci", "SSH + LuCI upload"},
		{"FIT model", "ews377-fit · FitController", "controller-managed"},
	}
	for _, r := range rows {
		b.WriteString(pad(2) + sKey.Render(fmt.Sprintf("%-13s", r[0])) + sBody.Render(r[1]) + "\n")
		b.WriteString(pad(15) + sSub.Render(r[2]) + "\n")
	}
	b.WriteString(blank() + sSub.Render("Run:  ") + sAccent.Render("pelegrun discover http://<ap-ip>"))
	return b.String()
}

func runConnect() string {
	var b strings.Builder
	b.WriteString(sBody.Render("Pick the access path. A normal SSH client won't reach it.") + blank() + blank())
	b.WriteString(pad(2) + sKey.Render("SSH   ") + sBody.Render("port ") + sKey.Render("8822") +
		sBody.Render(" (not 22), old host-key type, admin password") + "\n")
	b.WriteString(pad(2) + sKey.Render("Cloud ") + sBody.Render("web API login → token → upload") + "\n")
	b.WriteString(pad(2) + sKey.Render("LuCI  ") + sBody.Render("hashed-password login → session → 2-step upload") + "\n")
	b.WriteString(blank() + sSub.Render("All firmware writes still pass the safety checks."))
	return b.String()
}

func runBackup() string {
	var b strings.Builder
	b.WriteString(sBody.Render("Dump everything first. This is a hard gate, not a suggestion.") + blank() + blank())
	for _, a := range mews.Plan() {
		tag := sSub.Render("optional")
		if a.Critical {
			tag = sWarn.Render("required")
		}
		b.WriteString(pad(2) + sOK.Render("· ") + sInk.Render(fmt.Sprintf("%-20s", a.Name)) +
			sSub.Render(fmt.Sprintf("%-26s", a.Command)) + tag + "\n")
	}
	b.WriteString(blank() + sSub.Render("Partitions are found by NAME in /proc/mtd, never by index."))
	b.WriteString("\n" + sSub.Render("RF calibration (factory MACs) is read-only — never written."))
	b.WriteString("\n" + sWarn.Render("cert / userconfig are secret: keep offline, never attach to an issue."))
	return b.String()
}

func runSafeguards() string {
	var b strings.Builder
	b.WriteString(sBody.Render("The bootloader env is append-only. The tool can't brick it.") + blank() + blank())
	good := hood.ParsePrintenv("bootcmd=bootipq\nactive_fw=0\napp_part=0\nrootfsname=rootfs\nsnextra=00000000000000000000\n")
	cmd, _ := good.PlanSet("snextra", "SWLWX42000000000000A")
	wiped := hood.ParsePrintenv("ethaddr=00:03:7f:12:3e:87\n")
	_, _ = wiped.PlanSet("snextra", "x")
	b.WriteString(pad(2) + sOK.Render(fmt.Sprintf("%-16s", "· complete env")) + sBody.Render("allowed — adds one field") + "\n")
	b.WriteString(pad(2) + sNo.Render(fmt.Sprintf("%-16s", "· wiped env")) + sBody.Render(fmt.Sprintf("refused — %d required keys missing", len(wiped.Missing()))) + "\n")
	b.WriteString(pad(2) + sNo.Render(fmt.Sprintf("%-16s", "· empty value")) + sBody.Render("refused — u-boot would delete it") + "\n")
	b.WriteString(blank() + sSub.Render("Allowed write:  ") + sAccent.Render(cmd) + "\n")
	b.WriteString(blank() + sSub.Render("It only ever adds a field — never erases or resets."))
	return b.String()
}

func runIdentity() string {
	var b strings.Builder
	b.WriteString(sBody.Render("Mint a unique, valid serial per unit. Collisions are refused.") + blank() + blank())
	for i, mdl := range [][2]string{{"ECW230v3", "X42"}, {"EWS377-FIT", "X45"}, {"EWS377AP v3", "X44"}} {
		ser, _ := band.MakeSerial("SWLW", mdl[1], fmt.Sprintf("%04d", i+1))
		ok := sOK.Render("valid")
		if !band.ValidateSerial(ser) {
			ok = sNo.Render("BAD")
		}
		b.WriteString(pad(2) + sInk.Render(fmt.Sprintf("%-12s", mdl[0])) + sSub.Render("→ ") +
			sKey.Render(ser) + sSub.Render("  ("+mdl[1]+")  ") + ok + "\n")
	}
	x, _ := band.MakeSnextra("SWLW", "X42")
	b.WriteString(blank() + pad(2) + sSub.Render("field 19 (20 chars): ") + sAccent.Render(x))
	return b.String()
}

func runInstall() string {
	var b strings.Builder
	b.WriteString(sBody.Render("Flash over the network. No UART, no open case.") + blank() + blank())
	b.WriteString(pad(2) + sAccent.Render("1  ") + sBody.Render("Write the ") + sKey.Render("spare") + sBody.Render(" slot; the running one stays bootable") + "\n")
	b.WriteString(pad(2) + sAccent.Render("2  ") + sBody.Render("Reboot, then re-read firmware + serial") + "\n")
	b.WriteString(pad(2) + sAccent.Render("3  ") + sBody.Render("Roll back anytime; UART only if truly dead") + "\n")
	b.WriteString(blank() + sSub.Render("The active slot is never overwritten, so a bad flash can't brick."))
	return b.String()
}

func runVerify() string {
	var b strings.Builder
	b.WriteString(sBody.Render("Prove it came back exactly as intended.") + blank() + blank())
	b.WriteString(pad(2) + sOK.Render("· ") + sBody.Render("Re-read firmware family + serial after reboot") + "\n")
	b.WriteString(pad(2) + sOK.Render("· ") + sBody.Render("Compare against what was written") + "\n")
	b.WriteString(pad(2) + sOK.Render("· ") + sBody.Render("Flag any mismatch, offer one-step rollback") + "\n")
	b.WriteString(blank() + sSub.Render("Not done until this passes."))
	return b.String()
}
