package bestiary_test

import (
	_ "embed"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

//go:embed testdata/enum/harness_required_names.json
var harnessRequiredNamesJSON []byte

var harnessRequiredNames = func() []string {
	var names []string
	if err := json.Unmarshal(harnessRequiredNamesJSON, &names); err != nil {
		panic("decode testdata/enum/harness_required_names.json: " + err.Error())
	}
	return names
}()

//go:embed testdata/enum/harness_lifecycle.json
var harnessLifecycleJSON []byte

//go:embed testdata/enum/harness_lifecycle_required_names.json
var harnessLifecycleRequiredNamesJSON []byte

//go:embed testdata/enum/harness_archived_required_names.json
var harnessArchivedRequiredNamesJSON []byte

func TestHarness_IsKnown(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	for _, c := range corpus.Cases {
		if !bestiary.Harness(c.Input).IsKnown() {
			t.Errorf("Harness(%q).IsKnown() = false, want true", c.Input)
		}
	}

	unknown := []bestiary.Harness{
		"",
		"CLAUDE-CODE",
		"unknown-harness",
	}
	for _, h := range unknown {
		if h.IsKnown() {
			t.Errorf("Harness(%q).IsKnown() = true, want false", h)
		}
	}
}

func TestNewHarness(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	for _, c := range corpus.Cases {
		got, err := bestiary.NewHarness(c.Input)
		if err != nil {
			t.Errorf("NewHarness(%q) error = %v", c.Input, err)
			continue
		}
		if got.String() != c.Expected {
			t.Errorf("NewHarness(%q) = %q, want %q", c.Input, got, c.Expected)
		}
	}

	if _, err := bestiary.NewHarness("Pi"); err == nil {
		t.Fatal("NewHarness(Pi) error = nil, want unknown harness rejection")
	}
}

// TestHarness_String drives Harness.String() over every well-known constant, loaded
// from testdata/enum/harness_string_corpus.json. requireHarnessKnown additionally
// pins that each corpus token is still a recognized Harness.
func TestHarness_String(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	requireHarnessKnown(t, corpus)
	runEnumStringCorpus(t, corpus, func(_ *testing.T, in string) string {
		return bestiary.Harness(in).String()
	})
}

func TestHarness_MarshalUnmarshalText(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	for _, c := range corpus.Cases {
		h := bestiary.Harness(c.Input)
		b, err := h.MarshalText()
		if err != nil {
			t.Errorf("Harness(%q).MarshalText() error = %v", h, err)
			continue
		}
		var got bestiary.Harness
		if err := got.UnmarshalText(b); err != nil {
			t.Errorf("Harness.UnmarshalText(%q) error = %v", b, err)
			continue
		}
		if got != h {
			t.Errorf("round-trip: got %q, want %q", got, h)
		}
	}
}

func TestHarness_UnmarshalText_Permissive(t *testing.T) {
	var h bestiary.Harness
	if err := h.UnmarshalText([]byte("some-unknown-harness")); err != nil {
		t.Fatalf("UnmarshalText(unknown) error = %v, want nil", err)
	}
	if h.IsKnown() {
		t.Errorf("Harness(%q).IsKnown() = true, want false for unknown value", h)
	}
}

func TestHarness_UnmarshalText_NilReceiver(t *testing.T) {
	var h *bestiary.Harness
	err := h.UnmarshalText([]byte("claude-code"))
	if err == nil {
		t.Fatal("UnmarshalText on nil receiver: got nil error, want error")
	}
}

func TestHarnesses_AllKnown(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	want := make(map[bestiary.Harness]bool, len(corpus.Cases))
	var wantOrder []bestiary.Harness
	for _, c := range corpus.Cases {
		want[bestiary.Harness(c.Input)] = true
		wantOrder = append(wantOrder, bestiary.Harness(c.Input))
	}
	got := bestiary.Harnesses()
	if !slices.Equal(got, wantOrder) {
		t.Errorf("Harnesses() = %v, want canonical corpus order %v", got, wantOrder)
	}
	seen := make(map[bestiary.Harness]bool, len(got))
	for _, h := range got {
		if seen[h] {
			t.Errorf("Harnesses() returned duplicate %q", h)
		}
		seen[h] = true
		if !h.IsKnown() {
			t.Errorf("Harnesses() returned %q which IsKnown() = false", h)
		}
		if !want[h] {
			t.Errorf("Harnesses() returned unexpected %q", h)
		}
		delete(want, h)
	}
	for missing := range want {
		t.Errorf("Harnesses() omitted %q", missing)
	}
}

type harnessLifecycleInput struct {
	Constant string `json:"constant"`
	Value    string `json:"value"`
}

type harnessLifecycleExpected struct {
	Deprecated         bool     `json:"deprecated"`
	DocContains        []string `json:"doc_contains"`
	DeprecatedContains []string `json:"deprecated_contains"`
}

type harnessDeclaration struct {
	value bestiary.Harness
	doc   string
}

func TestHarnessLifecycle(t *testing.T) {
	corpus, err := testcase.LoadCorpus[harnessLifecycleInput, harnessLifecycleExpected](harnessLifecycleJSON)
	if err != nil {
		t.Fatalf("load harness lifecycle corpus: %v", err)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("validate harness lifecycle corpus: %v", err)
	}
	requiredCases := loadHarnessNameManifest(t, harnessLifecycleRequiredNamesJSON, "harness_lifecycle_required_names.json")
	requiredArchived := loadHarnessNameManifest(t, harnessArchivedRequiredNamesJSON, "harness_archived_required_names.json")

	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate harness.go for harness lifecycle: runtime.Caller failed; expected a source file path")
	}
	path := filepath.Join(filepath.Dir(testFile), "harness.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s for harness lifecycle: %v", path, err)
	}
	declarations := make(map[string]harnessDeclaration)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			doc := value.Doc
			if doc == nil && len(gen.Specs) == 1 {
				doc = gen.Doc
			}
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "Harness") {
					continue
				}
				if i >= len(value.Values) {
					t.Fatalf("%s: %s must have an explicit string value for lifecycle validation", path, name.Name)
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s: %s must have an explicit string value for lifecycle validation", path, name.Name)
				}
				wire, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s: parse value of %s: %v", path, name.Name, err)
				}
				text := ""
				if doc != nil {
					text = doc.Text()
				}
				declarations[name.Name] = harnessDeclaration{value: bestiary.Harness(wire), doc: text}
			}
		}
	}

	caseNames := make(map[string]bool, len(corpus.Cases))
	caseConstants := make(map[string]bool, len(corpus.Cases))
	caseValues := make(map[bestiary.Harness]bool, len(corpus.Cases))
	for _, c := range corpus.Cases {
		if caseNames[c.Name] || caseConstants[c.Input.Constant] {
			t.Errorf("harness lifecycle has duplicate case name or constant: %q, %q", c.Name, c.Input.Constant)
		}
		caseNames[c.Name] = true
		caseConstants[c.Input.Constant] = true
		if c.Classification != testcase.MustPass || c.Input.Constant == "" || c.Input.Value == "" || len(c.Expected.DocContains) == 0 {
			t.Errorf("harness lifecycle case %q must name a constant, value, required doc text, and must-pass classification", c.Name)
		}
		t.Run(c.Name, func(t *testing.T) {
			decl, ok := declarations[c.Input.Constant]
			if !ok {
				t.Fatalf("%s has no %s declaration", path, c.Input.Constant)
			}
			if decl.value != bestiary.Harness(c.Input.Value) {
				t.Errorf("%s = %q, want %q", c.Input.Constant, decl.value, c.Input.Value)
			}
			for _, text := range c.Expected.DocContains {
				if text == "" || !strings.Contains(decl.doc, text) {
					t.Errorf("%s documentation must contain %q; got %q", c.Input.Constant, text, decl.doc)
				}
			}
			paragraphs := deprecationParagraphs(decl.doc)
			if c.Expected.Deprecated {
				if len(paragraphs) != 1 || paragraphs[0] == "" {
					t.Errorf("%s must have one non-empty Deprecated: paragraph; got %q", c.Input.Constant, paragraphs)
				} else {
					for _, text := range c.Expected.DeprecatedContains {
						if text == "" || !strings.Contains(paragraphs[0], text) {
							t.Errorf("%s deprecation paragraph must contain %q; got %q", c.Input.Constant, text, paragraphs[0])
						}
					}
				}
			} else if len(paragraphs) != 0 {
				t.Errorf("%s must not carry a Deprecated: marker", c.Input.Constant)
			}
		})
		if decl, ok := declarations[c.Input.Constant]; ok {
			if caseValues[decl.value] {
				t.Errorf("harness lifecycle repeats archived value %q", decl.value)
			}
			caseValues[decl.value] = true
		}
	}
	for _, name := range requiredCases {
		if !caseNames[name] {
			t.Errorf("harness lifecycle corpus is missing required case %q", name)
		}
	}
	for name, decl := range declarations {
		if !caseConstants[name] && len(deprecationParagraphs(decl.doc)) != 0 {
			t.Errorf("%s: unexpected Deprecated: marker on %s", path, name)
		}
	}

	archived := bestiary.ArchivedHarnesses()
	wantArchived := make([]bestiary.Harness, 0, len(requiredArchived))
	for _, name := range requiredArchived {
		wantArchived = append(wantArchived, bestiary.Harness(name))
	}
	if !slices.Equal(archived, wantArchived) {
		t.Errorf("ArchivedHarnesses() = %v, want manifest order and membership %v", archived, wantArchived)
	}
	seen := make(map[bestiary.Harness]bool, len(archived))
	for _, h := range archived {
		if seen[h] {
			t.Errorf("ArchivedHarnesses() repeats %q", h)
		}
		seen[h] = true
		if !h.IsKnown() {
			t.Errorf("ArchivedHarnesses() includes unknown harness %q", h)
		}
		if !caseValues[h] {
			t.Errorf("archived harness %q has no lifecycle case", h)
		}
	}
	for h := range caseValues {
		if !seen[h] {
			t.Errorf("lifecycle case for %q has no archived harness", h)
		}
	}
	if len(archived) > 0 {
		before := slices.Clone(archived)
		archived[0] = "tampered"
		if !slices.Equal(bestiary.ArchivedHarnesses(), before) {
			t.Error("ArchivedHarnesses() returned a non-defensive copy")
		}
	}
}

func loadHarnessNameManifest(t *testing.T, data []byte, name string) []string {
	t.Helper()
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		t.Fatalf("decode testdata/enum/%s: %v", name, err)
	}
	if len(names) == 0 {
		t.Fatalf("testdata/enum/%s must not be empty", name)
	}
	seen := make(map[string]bool, len(names))
	for _, entry := range names {
		if entry == "" || seen[entry] {
			t.Errorf("testdata/enum/%s contains an empty or duplicate name %q", name, entry)
		}
		seen[entry] = true
	}
	return names
}

// deprecationParagraphs records each marker, including malformed markers not at
// a paragraph boundary, so an unexpected marker can never disappear from checks.
func deprecationParagraphs(doc string) []string {
	lines := strings.Split(doc, "\n")
	var paragraphs []string
	for i, line := range lines {
		reason, marked := strings.CutPrefix(line, "Deprecated:")
		if !marked {
			continue
		}
		if i > 0 && strings.TrimSpace(lines[i-1]) != "" {
			paragraphs = append(paragraphs, "")
			continue
		}
		parts := []string{strings.TrimSpace(reason)}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				break
			}
			parts = append(parts, strings.TrimSpace(next))
		}
		paragraphs = append(paragraphs, strings.TrimSpace(strings.Join(parts, " ")))
	}
	return paragraphs
}

func TestHarnesses_DefensiveCopy(t *testing.T) {
	first := bestiary.Harnesses()
	first[0] = "tampered"
	second := bestiary.Harnesses()
	if second[0] == "tampered" {
		t.Error("Harnesses() returned a non-defensive copy: mutation affected subsequent call")
	}
}
