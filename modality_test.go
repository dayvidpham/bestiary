package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestModalityString_ValidValues drives Modality.String() over every member of the
// closed enum, loaded from testdata/enum/modality_string_valid_corpus.json.
func TestModalityString_ValidValues(t *testing.T) {
	corpus := loadEnumIntCorpus(t, enumModalityStringValidCorpusJSON, 5)
	requireInputCoverage(t, corpus, map[int]string{
		int(bestiary.ModalityText):  "text",
		int(bestiary.ModalityVideo): "video",
	})
	runEnumIntStringCorpus(t, corpus, func(v int) string { return bestiary.Modality(v).String() })
}

// TestModalityString_OutOfRange drives Modality.String() over values outside the
// closed enum: the fallback must render a diagnosable Modality(N) form.
func TestModalityString_OutOfRange(t *testing.T) {
	corpus := loadEnumIntCorpus(t, enumModalityStringOutOfRangeCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[int]string{
		-1: "Modality(-1)",
		5:  "Modality(5)",
	})
	runEnumIntStringCorpus(t, corpus, func(v int) string { return bestiary.Modality(v).String() })
}

func TestModalityMarshalUnmarshalRoundTrip(t *testing.T) {
	modalities := []bestiary.Modality{
		bestiary.ModalityText,
		bestiary.ModalityImage,
		bestiary.ModalityPDF,
		bestiary.ModalityAudio,
		bestiary.ModalityVideo,
	}
	for _, m := range modalities {
		b, err := m.MarshalText()
		if err != nil {
			t.Errorf("Modality(%d).MarshalText() error = %v", int(m), err)
			continue
		}
		var got bestiary.Modality
		if err := got.UnmarshalText(b); err != nil {
			t.Errorf("Modality.UnmarshalText(%q) error = %v", b, err)
			continue
		}
		if got != m {
			t.Errorf("round-trip: got %d, want %d", int(got), int(m))
		}
	}
}

func TestModalityMarshalText_OutOfRange(t *testing.T) {
	_, err := bestiary.Modality(99).MarshalText()
	if err == nil {
		t.Error("Modality(99).MarshalText() expected error, got nil")
	}
}

func TestModalityUnmarshalText_Unknown(t *testing.T) {
	var m bestiary.Modality
	err := m.UnmarshalText([]byte("hologram"))
	if err == nil {
		t.Error("Modality.UnmarshalText(\"hologram\") expected error, got nil")
	}
}
