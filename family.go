package bestiary

import "fmt"

// IsKnown reports whether f is a recognized Family.
// The known set is generated from the models.dev API at codegen time
// and is stored in allFamilies (families_gen.go).
func (f Family) IsKnown() bool {
	for _, known := range allFamilies {
		if f == known {
			return true
		}
	}
	return false
}

// String returns the string representation of the family.
func (f Family) String() string {
	return string(f)
}

// MarshalText implements encoding.TextMarshaler.
// It is permissive: any Family value (known or unknown) can be marshaled.
func (f Family) MarshalText() ([]byte, error) {
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It is permissive: any byte slice is accepted; use IsKnown() to validate.
func (f *Family) UnmarshalText(b []byte) error {
	if f == nil {
		return fmt.Errorf("bestiary: Family.UnmarshalText: nil receiver")
	}
	*f = Family(b)
	return nil
}
