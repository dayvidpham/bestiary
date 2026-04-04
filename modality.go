package bestiary

import "fmt"

// Modality represents a type of input or output an AI model can process.
type Modality int

const (
	ModalityText  Modality = iota // "text"
	ModalityImage                 // "image"
	ModalityPDF                   // "pdf"
	ModalityAudio                 // "audio"
	ModalityVideo                 // "video"
)

var modalityNames = [...]string{"text", "image", "pdf", "audio", "video"}

// String returns the human-readable name of the modality.
// For out-of-range values it returns "Modality(<n>)" rather than panicking.
func (m Modality) String() string {
	if m < 0 || int(m) >= len(modalityNames) {
		return fmt.Sprintf("Modality(%d)", int(m))
	}
	return modalityNames[m]
}

// MarshalText implements encoding.TextMarshaler.
func (m Modality) MarshalText() ([]byte, error) {
	if m < 0 || int(m) >= len(modalityNames) {
		return nil, fmt.Errorf("bestiary: Modality.MarshalText: unknown value %d", int(m))
	}
	return []byte(modalityNames[m]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (m *Modality) UnmarshalText(b []byte) error {
	if m == nil {
		return fmt.Errorf("bestiary: Modality.UnmarshalText: nil receiver")
	}
	s := string(b)
	for i, name := range modalityNames {
		if s == name {
			*m = Modality(i)
			return nil
		}
	}
	return fmt.Errorf("bestiary: Modality.UnmarshalText: unknown modality %q", s)
}

// Modalities groups the input and output modalities supported by a model.
type Modalities struct {
	Input  []Modality
	Output []Modality
}
