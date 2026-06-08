package bestiary

import "fmt"

// DerivationKind classifies HOW a model was derived from one or more parent
// models — the edge label on a lineage relationship (see LineageEdge). It is a
// closed enum: the set of derivation techniques is small and well-understood, so
// (unlike Host/Provider) a strongly-typed int enum is the right fit.
//
// The zero value is DerivationNone (no derivation / a base model).
type DerivationKind int

const (
	// DerivationNone is the zero value: no derivation relationship (a base model).
	DerivationNone DerivationKind = iota
	// DerivationFinetune: parameters further trained on additional data
	// (e.g. dracarys finetuned from llama).
	DerivationFinetune
	// DerivationMerge: weights combined from two or more parent models. A merge
	// edge set carries >= 2 parents.
	DerivationMerge
	// DerivationDistillation: a smaller/student model trained to mimic a larger
	// teacher model.
	DerivationDistillation
	// DerivationQuantized: a lower-precision copy of a parent model.
	DerivationQuantized
	// DerivationAdapter: a parameter-efficient adapter (e.g. LoRA) applied over
	// a base model.
	DerivationAdapter
)

// derivationKindNames is the canonical text wire value for each kind, indexed by
// the enum value. It is the single source of truth for String/MarshalText.
var derivationKindNames = [...]string{
	DerivationNone:         "none",
	DerivationFinetune:     "finetune",
	DerivationMerge:        "merge",
	DerivationDistillation: "distillation",
	DerivationQuantized:    "quantized",
	DerivationAdapter:      "adapter",
}

// String returns the lowercase wire name of the derivation kind. An
// out-of-range value renders as "derivationkind(<n>)" so logs never silently
// drop an unexpected value.
func (k DerivationKind) String() string {
	if int(k) < 0 || int(k) >= len(derivationKindNames) {
		return fmt.Sprintf("derivationkind(%d)", int(k))
	}
	return derivationKindNames[k]
}

// MarshalText implements encoding.TextMarshaler, emitting the canonical wire
// name (so DerivationKind serializes as a JSON string, not an integer). An
// out-of-range value is a programming error and yields an actionable error.
func (k DerivationKind) MarshalText() ([]byte, error) {
	if int(k) < 0 || int(k) >= len(derivationKindNames) {
		return nil, fmt.Errorf(
			"bestiary: cannot marshal DerivationKind: value %d is out of range [0,%d);"+
				" why: an invalid enum value was constructed (only the DerivationNone..DerivationAdapter constants are valid);"+
				" where: DerivationKind.MarshalText;"+
				" how to fix: assign one of the exported DerivationKind constants",
			int(k), len(derivationKindNames),
		)
	}
	return []byte(derivationKindNames[k]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing a canonical wire
// name back into a DerivationKind. Parsing is case-sensitive against the wire
// names produced by MarshalText, guaranteeing a lossless round-trip. An
// unrecognized token yields an actionable error listing the valid values.
func (k *DerivationKind) UnmarshalText(text []byte) error {
	s := string(text)
	for i, name := range derivationKindNames {
		if name == s {
			*k = DerivationKind(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal DerivationKind from %q;"+
			" why: the token does not match any known derivation kind;"+
			" where: DerivationKind.UnmarshalText;"+
			" how to fix: use one of %v",
		s, derivationKindNames,
	)
}
