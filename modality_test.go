package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

func TestModalityString_ValidValues(t *testing.T) {
	cases := []struct {
		m    bestiary.Modality
		want string
	}{
		{bestiary.ModalityText, "text"},
		{bestiary.ModalityImage, "image"},
		{bestiary.ModalityPDF, "pdf"},
		{bestiary.ModalityAudio, "audio"},
		{bestiary.ModalityVideo, "video"},
	}
	for _, tc := range cases {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("Modality(%d).String() = %q, want %q", int(tc.m), got, tc.want)
		}
	}
}

func TestModalityString_OutOfRange(t *testing.T) {
	cases := []struct {
		m    bestiary.Modality
		want string
	}{
		{bestiary.Modality(99), "Modality(99)"},
		{bestiary.Modality(-1), "Modality(-1)"},
		{bestiary.Modality(5), "Modality(5)"},
	}
	for _, tc := range cases {
		got := tc.m.String()
		if got != tc.want {
			t.Errorf("Modality(%d).String() = %q, want %q", int(tc.m), got, tc.want)
		}
	}
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
