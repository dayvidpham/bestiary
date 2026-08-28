package bestiary

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The 2026-08-28 catalog refresh forced twenty-nine exact-ID pins. Each corrects a
// SERVING or LABELLING fact that the pipeline would otherwise read as identity, and
// each is invisible to the generic stale-bake guard: TestCodegen_UpToDate_RealInput
// notices that the bake MOVED, but its printed remedy is "run bestiary-gen and commit
// the regenerated files", which — after an accidental pin loss — produces a GREEN TREE
// WITH THE DEFECT RESTORED. It is a determinism guard doing duty as a correctness
// guard, and it cannot tell an accidental regression from an intended curation change.
//
// This file is the behavioural guard the pins were missing. Every pin carries its
// BEFORE tuple (what the unpinned pipeline produces), its AFTER tuple (what the pin
// asserts), the entity key its rows must land on, the Creator attribution at stake,
// and the user-visible defect that returns if the pin is dropped. Dropping any pin
// fails HERE, by name, with the broken behaviour stated — not as an opaque bake diff.
//
// The before/after tuples follow the model set by the eight fish-audio rows in
// cmd/bestiary-gen's justifiedExceptions ledger, which is where the same transitions
// are recorded on the decomposition side. The two records are deliberately redundant:
// the ledger justifies a diff against a frozen baseline and goes dead when the
// baseline is re-captured, while this file asserts the LIVE bake and never goes dead.
//
// Scope note: `Creator` is asserted because three of these pins exist to repair a
// MISATTRIBUTION, and the creator field is where the user sees it. An empty Creator is
// the honest answer for a family creators.json does not map; it is asserted exactly,
// so a pin loss that re-attaches the row to another lab's family bucket (which DOES
// map) fails on this field even when the entity key happens to survive.

// curatedPinCase is one exact-ID pin and the user-visible behaviour it defends.
type curatedPinCase struct {
	// ID is the idFamilyOverrides key: the model id, lower-cased.
	ID string
	// Before is the tuple the UNPINNED pipeline produces, in the same notation the
	// path-unification ledger uses.
	Before string
	// After is the tuple the pin asserts, and the one every row with this id must carry.
	WantFamily   string
	WantVariant  string
	WantVersion  string
	WantModifier []string
	// WantEntityKey is the entity every row with this id must land on.
	WantEntityKey string
	// WantCreator is the exact Creator attribution these rows must report. Empty is a
	// real, asserted value: it means creators.json maps no lab to this family.
	WantCreator Creator
	// Defect states, in user terms, what returns if the pin is dropped.
	Defect string
}

// curatedPinCases is the CLOSED table of the exact-ID pins the 2026-08-28 refresh
// forced. A row here that names an id no longer present in idFamilyOverrides is dead
// curation and fails TestCuratedIDPins_TableNamesLivePins.
var curatedPinCases = []curatedPinCase{
	// ---- misattribution repairs: an upstream family label credits the wrong lab ----
	{
		ID:            "gemma-4-31b-claude-4.6-opus-reasoning-distilled",
		Before:        `(family="claude",variant="opus",version="",modifier="")`,
		WantFamily:    "gemma",
		WantVersion:   "4",
		WantEntityKey: "gemma@4#31b",
		WantCreator:   CreatorGoogle,
		Defect: "nano-gpt's Gemma 4 31B distill keys claude/opus#31b and reports Creator anthropic — " +
			"a Google open-weights model credited to Anthropic on the flagship Opus line, at a 31B size " +
			"Anthropic has never published, and shown as a sibling of Opus 4.5/4.6/4.7 by `show claude/opus`",
	},
	{
		ID:            "trendyol-asure-12b",
		Before:        `(family="gemma",variant="",version="",modifier="")`,
		WantFamily:    "asure",
		WantEntityKey: "asure#12b",
		WantCreator:   "",
		Defect: "Trendyol's Asure 12B keys gemma#12b and is credited to Google — the entity holds that one " +
			"row and nothing else, so the whole key is a misattribution",
	},
	{
		ID:            "fish-audio/s1",
		Before:        `(family="o",variant="",version="",modifier="")`,
		WantFamily:    "fish-audio",
		WantVariant:   "s",
		WantVersion:   "1",
		WantEntityKey: "fish-audio/s@1",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/s1-free",
		Before:        `(family="o",variant="",version="",modifier="free")`,
		WantFamily:    "fish-audio",
		WantVariant:   "s",
		WantVersion:   "1",
		WantEntityKey: "fish-audio/s@1",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/s2-pro",
		Before:        `(family="o",variant="pro",version="",modifier="")`,
		WantFamily:    "fish-audio",
		WantVariant:   "s",
		WantVersion:   "2",
		WantModifier:  []string{"pro"},
		WantEntityKey: "fish-audio/s@2{pro}",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/s2-pro-free",
		Before:        `(family="o",variant="pro",version="",modifier="free")`,
		WantFamily:    "fish-audio",
		WantVariant:   "s",
		WantVersion:   "2",
		WantModifier:  []string{"pro"},
		WantEntityKey: "fish-audio/s@2{pro}",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/s2.1-pro",
		Before:        `(family="o",variant="pro",version="",modifier="")`,
		WantFamily:    "fish-audio",
		WantVariant:   "s",
		WantVersion:   "2.1",
		WantModifier:  []string{"pro"},
		WantEntityKey: "fish-audio/s@2.1{pro}",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/s2.1-pro-free",
		Before:        `(family="o",variant="pro",version="",modifier="free")`,
		WantFamily:    "fish-audio",
		WantVariant:   "s",
		WantVersion:   "2.1",
		WantModifier:  []string{"pro"},
		WantEntityKey: "fish-audio/s@2.1{pro}",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/transcribe-1",
		Before:        `(family="o",variant="",version="",modifier="")`,
		WantFamily:    "fish-audio",
		WantVariant:   "transcribe",
		WantVersion:   "1",
		WantEntityKey: "fish-audio/transcribe@1",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},
	{
		ID:            "fish-audio/transcribe-1-free",
		Before:        `(family="o",variant="",version="",modifier="free")`,
		WantFamily:    "fish-audio",
		WantVariant:   "transcribe",
		WantVersion:   "1",
		WantEntityKey: "fish-audio/transcribe@1",
		WantCreator:   "",
		Defect:        fishAudioDefect,
	},

	// ---- serving facts read as identity: a phantom key the vendor never published ----
	{
		ID:            "openai/gpt-5.6-sol-discounted",
		Before:        `(family="gpt",variant="",version="5.6",modifier="")`,
		WantFamily:    "gpt",
		WantVariant:   "sol",
		WantVersion:   "5.6",
		WantEntityKey: "gpt/sol@5.6",
		WantCreator:   CreatorOpenAI,
		Defect: "kilo's reseller PRICING label \"-discounted\" defeats the tier scan, minting a bare gpt@5.6 " +
			"that holds that one listing — and it HIJACKS resolution: `bestiary show gpt/5.6` stops spanning " +
			"the six real GPT-5.6 tiers and silently answers with one reseller's discounted rehost",
	},
	{
		ID:            "inkling-256k",
		Before:        `(family="inkling",variant="",version="256k",modifier="")`,
		WantFamily:    "inkling",
		WantEntityKey: "inkling",
		WantCreator:   CreatorThinkingMachines,
		Defect: "requesty's SERVED CONTEXT LENGTH becomes a version, minting inkling@256k — a release Thinking " +
			"Machines never published, and the keyspace's only context-length-shaped version. `bestiary show " +
			"inkling` then offers a phantom release as a peer of the real ones",
	},
	{
		ID:            "mimo-v25",
		Before:        `(family="mimo",variant="",version="25",modifier="")`,
		WantFamily:    "mimo",
		WantVersion:   "2.5",
		WantEntityKey: "mimo@2.5",
		WantCreator:   CreatorXiaomi,
		Defect: "inferx's dot-lost \"v25\" spelling mints mimo@25 — a version Xiaomi never published — holding " +
			"one instance beside the ~40 rows on the real mimo@2.5",
	},
	{
		ID:            "xiaomi/mimo-v2.5-pro-crof",
		Before:        `(family="mimo",variant="",version="",modifier="pro")`,
		WantFamily:    "mimo",
		WantVersion:   "2.5",
		WantModifier:  []string{"pro"},
		WantEntityKey: "mimo@2.5{pro}",
		WantCreator:   CreatorXiaomi,
		Defect:        crofDefect,
	},
	{
		ID:            "xiaomi/mimo-v2.5-pro-crof:thinking",
		Before:        `(family="mimo",variant="",version="",modifier="pro")`,
		WantFamily:    "mimo",
		WantVersion:   "2.5",
		WantModifier:  []string{"pro"},
		WantEntityKey: "mimo@2.5{pro}",
		WantCreator:   CreatorXiaomi,
		Defect:        crofDefect,
	},
	{
		ID:            "thinkingmachines/inkling:free",
		Before:        `(family="ling",variant="",version="",modifier="free")`,
		WantFamily:    "inkling",
		WantEntityKey: "inkling",
		WantCreator:   CreatorThinkingMachines,
		Defect:        inklingSuffixDefect,
	},
	{
		ID:            "thinkingmachines/inkling:thinking",
		Before:        `(family="ling",variant="",version="",modifier="")`,
		WantFamily:    "inkling",
		WantEntityKey: "inkling",
		WantCreator:   CreatorThinkingMachines,
		Defect:        inklingSuffixDefect,
	},
	{
		ID:            "thinkingmachines/inkling:peft:262144",
		Before:        `(family="ling",variant="",version="",modifier="")`,
		WantFamily:    "inkling",
		WantEntityKey: "inkling",
		WantCreator:   CreatorThinkingMachines,
		Defect:        inklingSuffixDefect,
	},

	// ---- version losses: a tier or backend token halts the version scan ----
	{
		ID:            "accounts/fireworks/models/nemotron-lightning-3p5-30b-a3b",
		Before:        `(family="nemotron",variant="",version="",modifier="")`,
		WantFamily:    "nemotron",
		WantVersion:   "3.5",
		WantEntityKey: "nemotron@3.5#30b-a3b",
		WantCreator:   CreatorNvidia,
		Defect:        nemotronDefect,
	},
	{
		ID:            "nemotron-lightning-3.5-30b-a3b",
		Before:        `(family="nemotron",variant="",version="",modifier="")`,
		WantFamily:    "nemotron",
		WantVersion:   "3.5",
		WantEntityKey: "nemotron@3.5#30b-a3b",
		WantCreator:   CreatorNvidia,
		Defect:        nemotronDefect,
	},
	{
		ID:            "nvidia/nvidia-nemotron-3.5-lightning-30b-a3b-bf16",
		Before:        `(family="nemotron",variant="",version="",modifier="")`,
		WantFamily:    "nemotron",
		WantVersion:   "3.5",
		WantEntityKey: "nemotron@3.5#30b-a3b",
		WantCreator:   CreatorNvidia,
		Defect:        nemotronDefect,
	},
	{
		ID:            "nvidia/nvidia-nemotron-3.5-lightning-30b-a3b",
		Before:        `(family="nemotron",variant="",version="",modifier="")`,
		WantFamily:    "nemotron",
		WantVersion:   "3.5",
		WantEntityKey: "nemotron@3.5#30b-a3b",
		WantCreator:   CreatorNvidia,
		Defect:        nemotronDefect,
	},
	{
		ID:            "eva-unit-01/eva-qwen2.5-32b-v0.2",
		Before:        `(family="qwen",variant="",version="",modifier="")`,
		WantFamily:    "eva",
		WantVersion:   "0.2",
		WantEntityKey: "eva@0.2#32b",
		WantCreator:   "",
		Defect: "upstream dropped the base-leading spelling and now serves only the org-prefixed id with " +
			"raw_family \"qwen\", so the EVA finetune falls into the BARE qwen bucket with no version and no " +
			"size — strictly worse than the compound family the original pin corrected",
	},

	// ---- beta is a release STAGE, never an identity ----
	{
		ID:            "spacexai/grok-4.20-multi-agent-beta",
		Before:        `(family="grok",variant="beta",version="4.20",modifier="")`,
		WantFamily:    "grok",
		WantVersion:   "4.20",
		WantEntityKey: "grok@4.20",
		WantCreator:   CreatorXAI,
		Defect:        grokBetaDefect,
	},
	{
		ID:            "spacexai/grok-4.20-non-reasoning-beta",
		Before:        `(family="grok",variant="beta",version="4.20",modifier="non-reasoning")`,
		WantFamily:    "grok",
		WantVersion:   "4.20",
		WantModifier:  []string{"non-reasoning"},
		WantEntityKey: "grok@4.20{non-reasoning}",
		WantCreator:   CreatorXAI,
		Defect:        grokBetaDefect,
	},
	{
		ID:            "spacexai/grok-4.20-reasoning-beta",
		Before:        `(family="grok",variant="beta",version="4.20",modifier="reasoning")`,
		WantFamily:    "grok",
		WantVersion:   "4.20",
		WantModifier:  []string{"reasoning"},
		WantEntityKey: "grok@4.20{reasoning}",
		WantCreator:   CreatorXAI,
		Defect:        grokBetaDefect,
	},
	{
		ID:            "xai/grok-4-20-beta-0309-non-reasoning",
		Before:        `(family="grok",variant="beta",version="4.20",modifier="non-reasoning")`,
		WantFamily:    "grok",
		WantVersion:   "4.20",
		WantModifier:  []string{"non-reasoning"},
		WantEntityKey: "grok@4.20{non-reasoning}",
		WantCreator:   CreatorXAI,
		Defect:        grokBetaDefect,
	},
	{
		ID:            "xai/grok-4-20-beta-0309-reasoning",
		Before:        `(family="grok",variant="beta",version="4.20",modifier="reasoning")`,
		WantFamily:    "grok",
		WantVersion:   "4.20",
		WantModifier:  []string{"reasoning"},
		WantEntityKey: "grok@4.20{reasoning}",
		WantCreator:   CreatorXAI,
		Defect:        grokBetaDefect,
	},
	{
		ID:            "grok-4.2-beta",
		Before:        `(family="grok",variant="beta",version="4.2",modifier="")`,
		WantFamily:    "grok",
		WantVersion:   "4.2",
		WantEntityKey: "grok@4.2",
		WantCreator:   CreatorXAI,
		Defect:        grokBetaDefect,
	},
}

// Shared defect statements, so the sibling rows of one lever cannot drift apart.
const (
	fishAudioDefect = "vercel files Fish Audio's speech models under raw_family \"o\" — OpenAI's o-series " +
		"bucket — so they key `o` / `o/pro`, which hold these eight rows and NOTHING else, and creators.json " +
		"credits OpenAI with Fish Audio's models"
	crofDefect = "nano-gpt spells the `crof` BACKEND into the id; the trailing \"-crof\" halts the version " +
		"scan, so the row loses its 2.5 and re-mints the undated mimo/pro key a prior lever had retired"
	inklingSuffixDefect = "the ':'-suffixed serving spellings defeat the ID-driven read, so the row falls " +
		"back on upstream's raw_family \"ling\" and re-mints the bare `ling` entity the collision split " +
		"retired — filing a Thinking Machines model under inclusionAI's family"
	nemotronDefect = "the tier token precedes the version (\"nemotron-lightning-3.5-…\", fireworks' " +
		"p-notation \"…-3p5-…\"), so the version scan halts on \"lightning\" and the row keys the UNDATED " +
		"nemotron#30b-a3b — a key a prior lever had retired, resurrected by version LOSS"
	grokBetaDefect = "\"beta\" sits MID-id, before the date/mode tokens, so it is promoted to the VARIANT " +
		"slot while the same row also carries Stage=beta — beta as both identity and stage, the degenerate " +
		"coexistence the release-stage axis exists to prevent (ValidateNoBetaInIdentity aborts the bake)"
)

// curatedPinDefectKeys are the entity keys the pins above prevent from being minted.
// Each is the phantom, junk-bucket or resurrected-undated key a dropped pin produces.
var curatedPinDefectKeys = map[string]string{
	"claude/opus#31b":  "the Gemma 4 31B distill, credited to Anthropic on the Opus line",
	"gemma#12b":        "Trendyol's Asure 12B, credited to Google",
	"o":                "OpenAI's o-series junk bucket, holding Fish Audio's speech models",
	"o/pro":            "as `o`, for the S2 Pro tiers",
	"gpt@5.6":          "the phantom that hijacks every `gpt/5.6` lookup to one reseller's discounted listing",
	"inkling@256k":     "a served context length read as a release Thinking Machines never published",
	"mimo@25":          "a dot-lost version Xiaomi never published",
	"mimo/pro":         "the undated mimo key the normalization retired, resurrected by the crof backend label",
	"ling":             "inclusionAI's family, holding Thinking Machines' Inkling rows",
	"nemotron#30b-a3b": "the undated nemotron key a prior lever retired, resurrected by version loss",
	"grok/beta@4.20":   "beta as an identity rather than a release stage",
	"grok/beta@4.2":    "as grok/beta@4.20, on the 4.2 spelling",
}

// TestCuratedIDPins_TableNamesLivePins fails on DEAD curation: a row naming an id that
// is no longer pinned. Without it a deleted pin could leave its row here asserting a
// tuple the pipeline happens to produce for another reason, and the table would read as
// live coverage while guarding nothing.
func TestCuratedIDPins_TableNamesLivePins(t *testing.T) {
	for _, c := range curatedPinCases {
		if _, ok := idFamilyOverrides[c.ID]; !ok {
			t.Errorf("curatedPinCases names %q, which is NOT in idFamilyOverrides\n"+
				"  What: this table row asserts a pin that no longer exists\n"+
				"  Why it matters: the row still passes whenever the pipeline reaches the same tuple by\n"+
				"    accident, so it reads as coverage while defending nothing\n"+
				"  How to fix: if the pin was deliberately removed, delete this row and re-pin the entity\n"+
				"    census in the same commit; if it was removed by accident, restore it in parse.go", c.ID)
		}
	}
}

// TestCuratedIDPins_TuplesAndKeysHold is the behavioural guard. For every pinned id it
// asserts, over EVERY baked row carrying that id (an id served by several providers is
// covered on all of them), the exact decomposition tuple, the Creator attribution, and
// the entity key the rows land on. Dropping a pin fails here by name.
func TestCuratedIDPins_TuplesAndKeysHold(t *testing.T) {
	byID := map[string][]ModelInfo{}
	for _, m := range staticModels {
		lid := strings.ToLower(string(m.ID))
		byID[lid] = append(byID[lid], m)
	}
	keyOfInstance := map[string]string{}
	for _, e := range Entities() {
		for _, in := range e.Instances {
			keyOfInstance[strings.ToLower(string(in.ID))] = e.Ref.String()
		}
	}

	for _, c := range curatedPinCases {
		rows := byID[c.ID]
		if len(rows) == 0 {
			t.Errorf("pinned id %q has NO row in the bake\n"+
				"  What: the pin is live but upstream no longer serves this id\n"+
				"  Why it matters: an unreachable pin is dead curation — it cannot defend anything, and\n"+
				"    it hides the fact that the artifact left the catalog\n"+
				"  How to fix: confirm against parse/data/modelsdev/catalog.json; if the id is genuinely\n"+
				"    gone, retire the pin and this row together and record it in the CHANGELOG", c.ID)
			continue
		}
		for _, m := range rows {
			got := fmt.Sprintf("(family=%q,variant=%q,version=%q,modifier=%q)",
				m.Family, m.Variant, m.Version, strings.Join(m.Modifier, ","))
			want := fmt.Sprintf("(family=%q,variant=%q,version=%q,modifier=%q)",
				c.WantFamily, c.WantVariant, c.WantVersion, strings.Join(c.WantModifier, ","))
			if got != want {
				t.Errorf("pin %q lost on provider %s\n"+
					"  What: the decomposition tuple is %s, want %s\n"+
					"  Why (the tuple this pin corrects): before=%s\n"+
					"  What it means for the caller: %s\n"+
					"  How to fix: restore the exact-id entry in idFamilyOverrides (parse.go), regenerate,\n"+
					"    and re-pin the entity census. Do NOT resolve this by regenerating alone — the\n"+
					"    stale-bake guard will go green with the defect baked in.",
					c.ID, m.Provider, got, want, c.Before, c.Defect)
			}
			if m.Creator != c.WantCreator {
				t.Errorf("pin %q: Creator is %q on provider %s, want %q\n"+
					"  What it means for the caller: %s\n"+
					"  How to fix: restore the exact-id entry in idFamilyOverrides (parse.go) and regenerate",
					c.ID, m.Creator, m.Provider, c.WantCreator, c.Defect)
			}
		}
		if got := keyOfInstance[c.ID]; got != c.WantEntityKey {
			t.Errorf("pin %q keys entity %q, want %q\n"+
				"  What it means for the caller: %s\n"+
				"  How to fix: restore the exact-id entry in idFamilyOverrides (parse.go) and regenerate",
				c.ID, got, c.WantEntityKey, c.Defect)
		}
	}
}

// TestCuratedIDPins_LiveParseHonoursEveryPin runs the pinned ids back through the LIVE
// decomposition entry point instead of reading the baked output. This is the arm that
// makes the guard useful: TestCuratedIDPins_TuplesAndKeysHold reads models_static_gen.go,
// so on its own it stays GREEN after a pin is deleted and only goes red once someone
// regenerates — by which time the stale-bake guard has already told the engineer to
// regenerate and commit, which is the remedy that restores the defect. This test goes
// red the moment the pin leaves parse.go, before any regeneration.
//
// The (RawFamily, ID, Provider) triple is read off the bake because it is the INPUT
// upstream supplies; the assertion is entirely on ParseFamilyDetailed's output.
func TestCuratedIDPins_LiveParseHonoursEveryPin(t *testing.T) {
	byID := map[string][]ModelInfo{}
	for _, m := range staticModels {
		byID[strings.ToLower(string(m.ID))] = append(byID[strings.ToLower(string(m.ID))], m)
	}
	for _, c := range curatedPinCases {
		for _, m := range byID[c.ID] {
			fam, variant, version, mods, pf := ParseFamilyDetailed(Family(m.RawFamily), m.ID, m.Provider)
			if pf != nil {
				t.Errorf("pin %q: ParseFamilyDetailed reports a parse failure on provider %s: %+v",
					c.ID, m.Provider, pf)
				continue
			}
			got := fmt.Sprintf("(family=%q,variant=%q,version=%q,modifier=%q)",
				fam, variant, version, strings.Join(mods, ","))
			want := fmt.Sprintf("(family=%q,variant=%q,version=%q,modifier=%q)",
				c.WantFamily, c.WantVariant, c.WantVersion, strings.Join(c.WantModifier, ","))
			if got != want {
				t.Errorf("pin %q is NOT honoured by the live pipeline (provider %s)\n"+
					"  What: ParseFamilyDetailed returns %s, want %s\n"+
					"  Why (the tuple this pin corrects): before=%s\n"+
					"  Where: parse.go, idFamilyOverrides\n"+
					"  What it means for the caller: %s\n"+
					"  How to fix: restore the exact-id entry in idFamilyOverrides. Regenerating alone is\n"+
					"    NOT the fix — it bakes the defect in and turns the stale-bake guard green.",
					c.ID, m.Provider, got, want, c.Before, c.Defect)
			}
		}
	}
}

// TestCuratedIDPins_DefectKeysNeverMinted asserts the other direction: the phantom and
// junk-bucket keys the pins prevent are ABSENT from the live keyspace. A pin can be
// lost in a way that leaves the tuple assertion above intact on one provider while a
// sibling spelling re-mints the bad key, so both directions are pinned.
func TestCuratedIDPins_DefectKeysNeverMinted(t *testing.T) {
	keys := make([]string, 0, len(curatedPinDefectKeys))
	for k := range curatedPinDefectKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := EntityByKey(k); ok {
			t.Errorf("entity key %q is LIVE; the curated pins exist to prevent it\n"+
				"  What it holds when the pin is lost: %s\n"+
				"  Where: parse.go, idFamilyOverrides\n"+
				"  How to fix: find the pinned id whose rows re-minted this key (see curatedPinCases for\n"+
				"    the mapping), restore its entry, regenerate, and re-pin the entity census",
				k, curatedPinDefectKeys[k])
		}
	}
}

// TestCuratedIDPins_GPT56SpansItsTiers is the headline claim of the "-discounted" pin,
// asserted at the seam a user actually hits rather than at the key. Unpinned, kilo's
// single discounted rehost mints a bare gpt@5.6 that resolves EXACTLY, so `show gpt/5.6`
// stops being ambiguous and silently answers with one reseller's listing.
func TestCuratedIDPins_GPT56SpansItsTiers(t *testing.T) {
	const input = "gpt/5.6"
	refs, err := Resolve(input)
	var amb *ErrAmbiguous
	if !errors.As(err, &amb) {
		t.Fatalf("Resolve(%q) returned (%d refs, err=%v), want the under-specified ErrAmbiguous\n"+
			"  What: the query resolved instead of spanning the GPT-5.6 tiers\n"+
			"  Why it matters: this is exactly the hijack the openai/gpt-5.6-sol-discounted pin prevents —\n"+
			"    kilo's reseller PRICING label mints a bare gpt@5.6 holding one discounted listing, and\n"+
			"    that phantom then answers every gpt/5.6 lookup in place of the real tiers\n"+
			"  How to fix: restore the openai/gpt-5.6-sol-discounted entry in idFamilyOverrides (parse.go)\n"+
			"    and regenerate", input, len(refs), err)
	}
	const wantTiers = 6
	if len(amb.Candidates) < wantTiers {
		t.Errorf("Resolve(%q) spans %d candidates, want at least %d (the real GPT-5.6 tiers)\n"+
			"  How to fix: check the tier pins in idFamilyOverrides (parse.go) before re-pinning this number",
			input, len(amb.Candidates), wantTiers)
	}
}
