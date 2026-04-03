package clipboard

import "errors"

var (
	// ErrNoImage is returned when the clipboard contains no image data.
	ErrNoImage = errors.New("no image in clipboard")

	// ErrOversized is returned when the clipboard payload exceeds the size cap.
	ErrOversized = errors.New("clipboard payload exceeds configured size cap")

	// ErrInvalidToken is returned when a clipboard request has no or invalid auth.
	ErrInvalidToken = errors.New("invalid clipboard token")

	// ErrUnsupportedFormat is returned when the clipboard image format cannot
	// be converted to PNG by either in-process or external conversion.
	ErrUnsupportedFormat = errors.New("unsupported image format")

	// ErrNoClipboardTool is returned when the required clipboard tool
	// (wl-paste or xclip) is not installed on the host.
	ErrNoClipboardTool = errors.New("clipboard tool not found")

	// ErrNoConversionTool is returned when magick is not installed for
	// uncommon image format conversion.
	ErrNoConversionTool = errors.New("image conversion tool not found")
)
