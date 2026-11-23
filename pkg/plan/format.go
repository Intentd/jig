package plan

// Format is the shape a report is written in.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// String returns the format's name, for messages and flags.
func (f Format) String() string { return string(f) }

// IsValid reports whether f is a known format.
func (f Format) IsValid() bool {
	switch f {
	case "", FormatText, FormatJSON:
		return true
	default:
		return false
	}
}

// IsText reports whether the format is text.
func (f Format) IsText() bool { return f == FormatText }

// IsJSON reports whether the format is JSON.
func (f Format) IsJSON() bool { return f == FormatJSON }

// Text returns the text format string.
func (f Format) Text() string { return string(FormatText) }

// JSON returns the JSON format string.
func (f Format) JSON() string { return string(FormatJSON) }
