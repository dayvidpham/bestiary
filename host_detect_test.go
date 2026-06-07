package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestDetectHost_CuratedPrefix covers VC1: a curated host-prefix model ID is
// split into (Host, stripped ID), and the decomposition of the stripped ID
// refines to the host-independent tuple. The negative cases pin the two guards
// that prevent the v0.2.2 blanket provider-name strip from reappearing: a
// namespaced org token that merely begins with a host word, and a genuine
// host-as-provider whose ID carries no host prefix, must BOTH stay HostNone.
func TestDetectHost_CuratedPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc       string
		id         bestiary.ModelID
		wantHost   bestiary.Host
		wantStrip  bestiary.ModelID
		wantFamily bestiary.Family
		wantVar    string
		wantVer    string
	}{
		{
			desc:       "azure-gpt-4o → host=azure, (gpt,4o)",
			id:         "azure-gpt-4o",
			wantHost:   bestiary.HostAzure,
			wantStrip:  "gpt-4o",
			wantFamily: "gpt",
			wantVar:    "4o",
			wantVer:    "",
		},
		{
			desc:       "azure-gpt-4o-mini → host=azure, (gpt,4o)",
			id:         "azure-gpt-4o-mini",
			wantHost:   bestiary.HostAzure,
			wantStrip:  "gpt-4o-mini",
			wantFamily: "gpt",
			wantVar:    "4o",
			wantVer:    "",
		},
		{
			desc:       "azure-gpt-4-turbo → host=azure, (gpt,,4)",
			id:         "azure-gpt-4-turbo",
			wantHost:   bestiary.HostAzure,
			wantStrip:  "gpt-4-turbo",
			wantFamily: "gpt",
			wantVar:    "",
			wantVer:    "4",
		},
		{
			desc:       "azure-o1 → host=azure, (gpt,o,1)",
			id:         "azure-o1",
			wantHost:   bestiary.HostAzure,
			wantStrip:  "o1",
			wantFamily: "gpt",
			wantVar:    "o",
			wantVer:    "1",
		},
		{
			desc:       "azure-o3-mini → host=azure, (gpt,o,3)",
			id:         "azure-o3-mini",
			wantHost:   bestiary.HostAzure,
			wantStrip:  "o3-mini",
			wantFamily: "gpt",
			wantVar:    "o",
			wantVer:    "3",
		},
		{
			// Guard A: namespaced org token beginning with a host word — never split.
			desc:       "azure-cognitive-services/gpt-4o → Host='' (namespaced org, not a host route)",
			id:         "azure-cognitive-services/gpt-4o",
			wantHost:   bestiary.HostNone,
			wantStrip:  "azure-cognitive-services/gpt-4o",
			wantFamily: "gpt",
			wantVar:    "4o",
			wantVer:    "",
		},
		{
			// Guard B: a plainly-served model has no host prefix on its ID.
			desc:       "gpt-4o → Host='' (no host prefix)",
			id:         "gpt-4o",
			wantHost:   bestiary.HostNone,
			wantStrip:  "gpt-4o",
			wantFamily: "gpt",
			wantVar:    "4o",
			wantVer:    "",
		},
		{
			// A bare host token alone is not a model and must not be stripped.
			desc:      "azure (bare token) → Host='' (no '<host>-<model>' form)",
			id:        "azure",
			wantHost:  bestiary.HostNone,
			wantStrip: "azure",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			host, strip := bestiary.DetectHost(tc.id)
			if host != tc.wantHost {
				t.Errorf("DetectHost(%q) host = %q, want %q", tc.id, host, tc.wantHost)
			}
			if strip != tc.wantStrip {
				t.Errorf("DetectHost(%q) stripped = %q, want %q", tc.id, strip, tc.wantStrip)
			}

			// The decomposition consumes the ORIGINAL ID (ParseFamilyDetailed
			// strips the host prefix internally) — this is exactly the codegen
			// production path.
			family, variant, version, _, _ := bestiary.ParseFamilyDetailed("", tc.id, "nano-gpt")
			if tc.wantFamily == "" && tc.wantVar == "" && tc.wantVer == "" {
				return // bare-token case: decomposition is unconstrained here.
			}
			if family != tc.wantFamily || variant != tc.wantVar || version != tc.wantVer {
				t.Errorf("ParseFamilyDetailed(%q) = (%q,%q,%q), want (%q,%q,%q)",
					tc.id, family, variant, version, tc.wantFamily, tc.wantVar, tc.wantVer)
			}
		})
	}
}

// TestHostSplit_EntityParity covers VC1b: a host-routed instance (azure-gpt-4o)
// must decompose to the SAME (Family,Variant,Version) identity tuple as the
// plainly-served model (gpt-4o), so the two share an entity. Host being a
// per-instance attribute, it is the ONLY field that differs. We pin the parity
// pairwise across all five seeded NanoGPT azure-* instances and their plain
// counterparts as they appear under genuine providers.
func TestHostSplit_EntityParity(t *testing.T) {
	t.Parallel()

	type input struct {
		raw bestiary.Family
		id  bestiary.ModelID
		p   bestiary.Provider
	}
	cases := []struct {
		desc     string
		hosted   input
		plain    input
		wantHost bestiary.Host
	}{
		{
			desc:     "gpt-4o",
			hosted:   input{"", "azure-gpt-4o", "nano-gpt"},
			plain:    input{"gpt", "gpt-4o", "openai"},
			wantHost: bestiary.HostAzure,
		},
		{
			desc:     "gpt-4o-mini",
			hosted:   input{"", "azure-gpt-4o-mini", "nano-gpt"},
			plain:    input{"gpt", "gpt-4o-mini", "openai"},
			wantHost: bestiary.HostAzure,
		},
		{
			desc:     "gpt-4-turbo",
			hosted:   input{"", "azure-gpt-4-turbo", "nano-gpt"},
			plain:    input{"gpt", "gpt-4-turbo", "openai"},
			wantHost: bestiary.HostAzure,
		},
		{
			desc:     "o1",
			hosted:   input{"", "azure-o1", "nano-gpt"},
			plain:    input{"o", "o1", "openai"},
			wantHost: bestiary.HostAzure,
		},
		{
			desc:     "o3-mini",
			hosted:   input{"", "azure-o3-mini", "nano-gpt"},
			plain:    input{"o-mini", "o3-mini", "openai"},
			wantHost: bestiary.HostAzure,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			hf, hv, hver, hmod, _ := bestiary.ParseFamilyDetailed(tc.hosted.raw, tc.hosted.id, tc.hosted.p)
			pf, pv, pver, pmod, _ := bestiary.ParseFamilyDetailed(tc.plain.raw, tc.plain.id, tc.plain.p)

			if hf != pf || hv != pv || hver != pver {
				t.Errorf("identity tuple divergence: hosted %q = (%q,%q,%q), plain %q = (%q,%q,%q) — host-routed instance must share the base entity",
					tc.hosted.id, hf, hv, hver, tc.plain.id, pf, pv, pver)
			}
			if len(hmod) != len(pmod) {
				t.Errorf("modifier divergence: hosted %q mod=%v, plain %q mod=%v",
					tc.hosted.id, hmod, tc.plain.id, pmod)
			} else {
				for i := range hmod {
					if hmod[i] != pmod[i] {
						t.Errorf("modifier divergence at %d: hosted %v vs plain %v", i, hmod, pmod)
						break
					}
				}
			}

			// The host attribute is the sole distinguishing field of the hosted instance.
			gotHost, _ := bestiary.DetectHost(tc.hosted.id)
			if gotHost != tc.wantHost {
				t.Errorf("DetectHost(%q) = %q, want %q", tc.hosted.id, gotHost, tc.wantHost)
			}
			plainHost, _ := bestiary.DetectHost(tc.plain.id)
			if plainHost != bestiary.HostNone {
				t.Errorf("DetectHost(%q) = %q, want HostNone (plainly-served model)", tc.plain.id, plainHost)
			}
		})
	}
}
