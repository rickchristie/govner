package theme

import "github.com/charmbracelet/lipgloss"

// === Color Constants ===

// Base colors -- The darkness of charred oak.
var (
	ColorCharcoal = lipgloss.Color("#0c0e12")
	ColorOakDark  = lipgloss.Color("#151920")
	ColorOakMid   = lipgloss.Color("#1e2530")
	ColorOakLight = lipgloss.Color("#283040")
	ColorStave    = lipgloss.Color("#3a4556")
)

// Text colors.
var (
	ColorParchment = lipgloss.Color("#e8e0d4")
	ColorLinen     = lipgloss.Color("#c4b8a8")
	ColorDusty     = lipgloss.Color("#8a7e70")
	ColorFaded     = lipgloss.Color("#5c5248")
	ColorVoid      = lipgloss.Color("#0c0e12")
)

// Accent colors -- Warm.
var (
	ColorAmber  = lipgloss.Color("#f0a830")
	ColorCopper = lipgloss.Color("#d4783c")
	ColorBarrel = lipgloss.Color("#b85c28")
	ColorFlame  = lipgloss.Color("#e84820")
	ColorWheat  = lipgloss.Color("#e8d8a0")
)

// Accent colors -- Cool.
var (
	ColorProof     = lipgloss.Color("#58c878")
	ColorVerdigris = lipgloss.Color("#48a89c")
	ColorSlateBlue = lipgloss.Color("#6888b8")
	ColorMist      = lipgloss.Color("#889cb8")
)

// Semantic aliases.
var (
	ColorBg            = ColorCharcoal
	ColorSurface       = ColorOakDark
	ColorSurfaceHi     = ColorOakMid
	ColorBorder        = ColorOakLight
	ColorBorderDim     = ColorStave
	ColorTextPrimary   = ColorParchment
	ColorTextSecondary = ColorLinen
	ColorTextTertiary  = ColorDusty
	ColorTextDisabled  = ColorFaded
	ColorAccent        = ColorAmber
	ColorSuccess       = ColorProof
	ColorDanger        = ColorFlame
	ColorWarning       = ColorCopper
	ColorInfo          = ColorSlateBlue
)

// === Pre-computed Styles ===

// Brand and header styles.
var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorAmber).
			Bold(true)

	BrandStyle = lipgloss.NewStyle().
			Foreground(ColorAmber).
			Bold(true)

	TaglineStyle = lipgloss.NewStyle().
			Foreground(ColorDusty).
			Italic(true)

	HeaderBarStyle = lipgloss.NewStyle()

	HeaderDividerStyle = lipgloss.NewStyle().
				Foreground(ColorOakLight)
)

// Tab bar styles.
var (
	TabActiveStyle = lipgloss.NewStyle().
			Foreground(ColorAmber).
			Bold(true)

	TabInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorDusty)

	TabHoverStyle = lipgloss.NewStyle().
			Foreground(ColorLinen)

	TabUnderlineActiveStyle = lipgloss.NewStyle().
				Foreground(ColorAmber)

	TabUnderlineInactiveStyle = lipgloss.NewStyle().
					Foreground(ColorOakLight)
)

// Row styles for lists and tables.
var (
	RowNormalStyle = lipgloss.NewStyle().
			Foreground(ColorLinen)

	RowSelectedStyle = lipgloss.NewStyle().
				Background(ColorOakMid).
				Foreground(ColorParchment).
				Bold(true)

	SelectionArrowStyle = lipgloss.NewStyle().
				Foreground(ColorAmber).
				Bold(true)

	ColumnHeaderStyle = lipgloss.NewStyle().
				Foreground(ColorDusty).
				Bold(true)
)

// Container and status styles.
var (
	StatusRunningStyle = lipgloss.NewStyle().
				Foreground(ColorProof)

	StatusStoppedStyle = lipgloss.NewStyle().
				Foreground(ColorFlame)

	StatusPendingStyle = lipgloss.NewStyle().
				Foreground(ColorAmber)

	StatusTransitionStyle = lipgloss.NewStyle().
				Foreground(ColorAmber)

	ContainerNameStyle = lipgloss.NewStyle().
				Foreground(ColorParchment)

	TimestampStyle = lipgloss.NewStyle().
			Foreground(ColorDusty)

	SourceStyle = lipgloss.NewStyle().
			Foreground(ColorMist)

	DomainStyle = lipgloss.NewStyle().
			Foreground(ColorParchment)
)

// HTTP method badge styles.
var (
	MethodGetStyle = lipgloss.NewStyle().
			Background(ColorSlateBlue).
			Foreground(ColorVoid).
			Padding(0, 1)

	MethodPostStyle = lipgloss.NewStyle().
			Background(ColorAmber).
			Foreground(ColorVoid).
			Padding(0, 1)

	MethodPutStyle = lipgloss.NewStyle().
			Background(ColorCopper).
			Foreground(ColorVoid).
			Padding(0, 1)

	MethodDeleteStyle = lipgloss.NewStyle().
				Background(ColorFlame).
				Foreground(ColorParchment).
				Padding(0, 1)

	MethodPatchStyle = lipgloss.NewStyle().
				Background(ColorVerdigris).
				Foreground(ColorVoid).
				Padding(0, 1)
)

// HTTP status code styles.
var (
	StatusCode2xxStyle = lipgloss.NewStyle().Foreground(ColorProof)
	StatusCode3xxStyle = lipgloss.NewStyle().Foreground(ColorSlateBlue)
	StatusCode4xxStyle = lipgloss.NewStyle().Foreground(ColorCopper)
	StatusCode5xxStyle = lipgloss.NewStyle().Foreground(ColorFlame)
)

// Modal styles.
var (
	ModalOverlayStyle = lipgloss.NewStyle().
				Foreground(ColorFaded)

	ModalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorOakLight).
			Background(ColorOakDark).
			Padding(1, 2)

	ModalBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.DoubleBorder()).
				BorderForeground(ColorAmber).
				Padding(1, 3).
				Width(50)

	ModalTitleStyle = lipgloss.NewStyle().
			Foreground(ColorParchment).
			Bold(true).
			Align(lipgloss.Center).
			Width(44)

	ModalDividerStyle = lipgloss.NewStyle().
				Foreground(ColorOakLight).
				Align(lipgloss.Center).
				Width(44)

	ModalBodyStyle = lipgloss.NewStyle().
			Foreground(ColorLinen).
			Align(lipgloss.Center).
			Width(44)

	ModalConfirmStyle = lipgloss.NewStyle().
				Foreground(ColorProof).
				Bold(true)

	ModalCancelStyle = lipgloss.NewStyle().
				Foreground(ColorDusty)

	DimBackdropStyle = lipgloss.NewStyle().
				Foreground(ColorFaded)
)

// Input field styles.
var (
	InputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorOakLight).
			Foreground(ColorParchment).
			Padding(0, 1)

	InputFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(ColorAmber).
				Foreground(ColorParchment).
				Padding(0, 1)

	InputActiveStyle = lipgloss.NewStyle().
				Foreground(ColorAmber)

	InputInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorOakLight)

	InputTextStyle = lipgloss.NewStyle().
			Foreground(ColorParchment)

	InputPlaceholderStyle = lipgloss.NewStyle().
				Foreground(ColorFaded).
				Italic(true)
)

// Help bar styles.
var (
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorAmber)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorDusty)

	HelpBarStyle = lipgloss.NewStyle().
			Foreground(ColorDusty)
)

// Timer bar styles for countdown gradient thresholds.
var (
	TimerBarProofStyle  = lipgloss.NewStyle().Foreground(ColorProof)
	TimerBarAmberStyle  = lipgloss.NewStyle().Foreground(ColorAmber)
	TimerBarCopperStyle = lipgloss.NewStyle().Foreground(ColorCopper)
	TimerBarBarrelStyle = lipgloss.NewStyle().Foreground(ColorBarrel)
	TimerBarFlameStyle  = lipgloss.NewStyle().Foreground(ColorFlame)
	TimerBarEmptyStyle  = lipgloss.NewStyle().Foreground(ColorOakLight)
)

// Divider and layout styles.
var (
	DividerStyle = lipgloss.NewStyle().
			Foreground(ColorOakLight)

	PaneLabelStyle = lipgloss.NewStyle().
			Foreground(ColorDusty).
			Bold(true)
)

// Badge and detail styles.
var (
	CountBadgeStyle = lipgloss.NewStyle().
			Foreground(ColorVoid).
			Background(ColorAmber).
			PaddingLeft(1).
			PaddingRight(1)

	DetailLabelStyle = lipgloss.NewStyle().
				Foreground(ColorDusty).
				Bold(true).
				Width(10).
				Align(lipgloss.Right)

	DetailValueStyle = lipgloss.NewStyle().
				Foreground(ColorParchment)

	DetailSectionStyle = lipgloss.NewStyle().
				Foreground(ColorDusty)

	DetailPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorOakLight).
			Padding(0, 1)
)

// Info and emphasis styles.
var (
	InfoBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorOakLight).
			Padding(0, 1)

	InfoTextStyle = lipgloss.NewStyle().
			Foreground(ColorDusty)

	InfoEmphasisStyle = lipgloss.NewStyle().
				Foreground(ColorCopper).
				Bold(true)
)

// Status and semantic convenience styles.
var (
	ProofStyle = lipgloss.NewStyle().
			Foreground(ColorProof)

	FlameStyle = lipgloss.NewStyle().
			Foreground(ColorFlame)

	CopperStyle = lipgloss.NewStyle().
			Foreground(ColorCopper)

	EmptyStateStyle = lipgloss.NewStyle().
			Foreground(ColorDusty).
			Italic(true).
			Align(lipgloss.Center)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorFlame).
			Bold(true)

	DimStyle = lipgloss.NewStyle().
			Foreground(ColorDusty)
)

// TimerColor returns the appropriate color for a countdown timer based on
// the fraction of time remaining (1.0 = full, 0.0 = expired).
func TimerColor(progress float64) lipgloss.Color {
	switch {
	case progress > 0.80:
		return ColorProof
	case progress > 0.60:
		return ColorAmber
	case progress > 0.40:
		return ColorCopper
	case progress > 0.20:
		return ColorBarrel
	default:
		return ColorFlame
	}
}
