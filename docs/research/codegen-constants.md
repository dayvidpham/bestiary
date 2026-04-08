# Research: Codegen Constants for Providers and Model IDs

Date: 2026-04-07
Context: GitHub issue #1 — expand codegen to all providers and generate typed constants

---

## 1. models.dev API Structure

### Provider inventory

The API returns a top-level JSON object keyed by provider slug. As of 2026-04-07:

- **110 providers** total
- **4,168 models** total (across all providers)
- **2,300 unique model IDs** (684 model IDs appear under multiple providers)

### Provider slug naming

All slugs are lowercase with hyphen separators. One exception: `302ai` starts with a digit.

- No underscores, no dots, no uppercase in any provider slug
- Single digit-prefixed slug: `302ai`
- All others are valid Go identifier bases after PascalCase conversion

### Provider-level fields

Every provider has: `id`, `name`, `models`, `doc`, `npm`, `env`. 87/110 also have `api` (base URL).

### Model count distribution

Distribution is extremely long-tailed:

| Range | Provider count | % of all models |
|-------|---------------|-----------------|
| Top 5 | 5 | ~37% |
| Top 12 | 12 | ~50% |
| Top 33 | 33 | ~80% |
| Bottom 40 | 40 | ~5% |

Top 5 by model count: nano-gpt (519), kilo (335), vercel (232), llmgateway (203), openrouter (171).

### Aggregator vs first-party providers

Classifying by unique model families:

- **21 first-party providers** have at least one model family not seen on any other provider (e.g., openai, xai, nvidia, groq, upstage, cohere-via-unique-families)
- **83 pure aggregators** only host models whose families also appear under other providers (e.g., azure, amazon-bedrock, openrouter, vercel)
- Notable: `anthropic` and `google` are classified as aggregators by this metric because their model families (claude-*, gemini-*) also appear under aggregators like azure, bedrock, etc. — but they are obviously the canonical source.

Key aggregators to watch: openrouter (171 models, 40 families), vercel (232 models, 67 families), nano-gpt (519 models, 57 families), kilo (335 models).

### Model ID patterns

Model IDs contain complex characters that affect Go identifier generation:

| Pattern | Count | Examples |
|---------|-------|---------|
| Contains `/` | 2,240 | `public/deepseek-r1`, `google/gemma-3` |
| Contains `.` | 1,655 | `kimi-k2.5`, `glm-4.6` |
| Contains `:` | 178 | `mistral-large-3:675b`, `cogito-2.1:671b` |
| Contains `@` | 95 | `workers-ai/@cf/openai/gpt-oss-120b` |
| Contains `+` | 3 | `Llama-3.3+(3v3.3)-70B-TenyxChat-DaybreakStorywriter` |
| Starts with digit | 0 | (none) |
| Mixed case | many | `ByteDance-Seed/Seed-OSS-36B-Instruct` |

---

## 2. Go Codegen Patterns for Typed String Constants

### Prior art from well-known projects

**golang.org/x/text/language (CLDR tags)**
- Uses `go generate` with internal codegen tools
- Generates both constants (`var Afrikaans = Tag{...}`) and lookup tables
- Separate generated files per concern: `tables.go`, `index.go`, `match.go`
- Special-casing table for known exceptions (e.g., `"en"` maps to `English`)

**google/go-github**
- API-surface constants are hand-maintained, not generated
- Uses `type String = string` approach for optional fields
- No codegen for enum-like values — relies on documentation

**aws-sdk-go-v2**
- Generates typed string enums per service: `type InstanceType string`
- Constants like `InstanceTypeT2Micro InstanceType = "t2.micro"`
- Uses an override table for special casing (e.g., `"io1"` -> `InstanceTypeIo1`)
- Separate `_gen.go` files per service, generated from API model JSON
- Includes `Values()` function returning all valid values for each enum type

**hashicorp/terraform-plugin-framework**
- Uses `stringer`-style code generation
- Generates validation functions alongside constants

### Naming conventions for slug-to-Go-identifier

The standard Go approach:

1. Split on `-`, `_`, `.`, `/`, `:`, `@`
2. PascalCase each segment
3. Apply known-word overrides: `Ai` -> `AI`, `Api` -> `API`, `Url` -> `URL`, etc.
4. Prefix with type name: `Provider` + PascalCase or `Model` + PascalCase
5. Handle leading digits with underscore or `X` prefix (only `302ai` needs this)

### File organization patterns

Common approach: **separate generated files per concern**.

```
providers_gen.go      # Provider constants + knownProviders slice + Providers() func
models_static_gen.go  # ModelInfo data (existing, expanded)
```

Some projects use a single generated file. The two-file split is preferred when:
- Constants are imported independently of data (callers may only need `ProviderAnthropic`)
- Data file is large (models_static_gen.go is already 100KB with 3 providers)

### Known vs unknown distinction

Two patterns in practice:

**Pattern A: Closed enum (current bestiary approach)**
- `IsKnown()` returns true only for hand-maintained constants
- Unknown values are valid at the type level but fail `IsKnown()`
- Pro: Explicit. Con: Must update list when new providers appear.

**Pattern B: Generated "known" set (aws-sdk-go approach)**
- `Values()` returns all generated constants
- `IsKnown()` checks membership in generated set
- Pro: Always in sync with API. Con: "known" means "in the API" not "supported".

**Pattern C: Two-tier (recommended for bestiary)**
- Generate constants for all API providers
- Keep a separate `IsSupported()` or `IsPrimaryProvider()` for the subset that bestiary actually tests/validates
- `IsKnown()` covers all generated constants
- Pro: Best of both. Con: Two concepts to document.

---

## 3. Identifier Collision Risks

### Provider slug collisions

**Zero collisions** when converting all 110 provider slugs to PascalCase Go identifiers. Each slug maps to a unique `Provider*` constant.

Special case: `302ai` starts with a digit. Recommended: `Provider302AI` (prefix with type name handles the digit issue, since `Provider302AI` is a valid Go identifier).

### Model ID collisions

**191 identifier collisions** when converting 2,300 unique model IDs to Go identifiers across all providers. These occur when two or more different model ID slugs normalize to the same PascalCase Go identifier. The 191 collisions involve 238 distinct model ID slugs being reduced to 191 unique identifiers.

**Collision patterns:**

1. **Slash vs double-hyphen**: `anthropic/claude-3-haiku` and `anthropic--claude-3-haiku` both map to `AnthropicClaude3Haiku`
2. **Hyphen vs dot**: `claude-opus-4.5` and `claude-opus-4-5` both map to `ClaudeOpus45`
3. **Colon vs hyphen**: `claude-opus-4-5-20251101:thinking` and `claude-opus-4-5-20251101-thinking` collide to `ClaudeOpus4520251101Thinking`
4. **Version segment ambiguity**: `claude-opus-41` vs `claude-opus-4-1` both become `ClaudeOpus41`
5. **Case normalization**: `essentialai/Rnj-1-Instruct` and `essentialai/rnj-1-instruct` collide to `EssentialaiRnj1Instruct`
6. **Leading capitals with slash/dot variations**: Various providers (e.g., `Gryphe/MythoMax-L2-13b` vs `gryphe/mythomax-l2-13b`)
7. **Provider prefix variations**: `minimax.minimax-m2.5`, `minimax/minimax-m2.5` vs other variants all map to same identifier
8. **Dot-based separator collision**: `kimi-k2.5` and related variants map to `KimiK25`

**Critical implication**: Generating `ModelID` constants for all models globally is not feasible without a collision resolution strategy.

---

## Appendix: Full Enumeration of 191 Model ID Collisions

As of 2026-04-08, there are **191 identifier collisions** involving **238 distinct model ID slugs**:

1. **Identifier**: `AionLabsAion20`
   - `aion-labs.aion-2-0` (venice)
   - `aion-labs/aion-2.0` (kilo)

2. **Identifier**: `AnthropicClaude35Haiku`
   - `anthropic/claude-3-5-haiku` (cloudflare-ai-gateway)
   - `anthropic/claude-3.5-haiku` (cloudflare-ai-gateway, kilo, openrouter, vercel, zenmux)

3. **Identifier**: `AnthropicClaude35Sonnet`
   - `anthropic--claude-3.5-sonnet` (sap-ai-core)
   - `anthropic/claude-3.5-sonnet` (cloudflare-ai-gateway, kilo, vercel)

4. **Identifier**: `AnthropicClaude37Sonnet`
   - `anthropic--claude-3.7-sonnet` (sap-ai-core)
   - `anthropic/claude-3-7-sonnet` (requesty)
   - `anthropic/claude-3.7-sonnet` (kilo, openrouter, vercel, zenmux)

5. **Identifier**: `AnthropicClaude3Haiku`
   - `anthropic--claude-3-haiku` (sap-ai-core)
   - `anthropic/claude-3-haiku` (cloudflare-ai-gateway, kilo, vercel)

6. **Identifier**: `AnthropicClaude3Opus`
   - `anthropic--claude-3-opus` (sap-ai-core)
   - `anthropic/claude-3-opus` (cloudflare-ai-gateway, vercel)

7. **Identifier**: `AnthropicClaude3Sonnet`
   - `anthropic--claude-3-sonnet` (sap-ai-core)
   - `anthropic/claude-3-sonnet` (cloudflare-ai-gateway)

8. **Identifier**: `AnthropicClaude4Opus`
   - `anthropic--claude-4-opus` (sap-ai-core)
   - `anthropic/claude-4-opus` (deepinfra)

9. **Identifier**: `AnthropicClaudeHaiku45`
   - `anthropic/claude-haiku-4-5` (cloudflare-ai-gateway, perplexity-agent, requesty)
   - `anthropic/claude-haiku-4.5` (kilo, openrouter, poe, vercel, zenmux)

10. **Identifier**: `AnthropicClaudeOpus41`
    - `anthropic/claude-opus-4-1` (cloudflare-ai-gateway, requesty)
    - `anthropic/claude-opus-4.1` (fastrouter, kilo, openrouter, poe, vercel, zenmux)

11. **Identifier**: `AnthropicClaudeOpus45`
    - `anthropic/claude-opus-4-5` (cloudflare-ai-gateway, perplexity-agent, requesty)
    - `anthropic/claude-opus-4.5` (kilo, openrouter, poe, vercel, zenmux)

12. **Identifier**: `AnthropicClaudeOpus46`
    - `anthropic/claude-opus-4-6` (cloudflare-ai-gateway, perplexity-agent, requesty)
    - `anthropic/claude-opus-4.6` (kilo, nano-gpt, openrouter, poe, vercel, zenmux)

13. **Identifier**: `AnthropicClaudeSonnet45`
    - `anthropic/claude-sonnet-4-5` (cloudflare-ai-gateway, perplexity-agent, requesty)
    - `anthropic/claude-sonnet-4.5` (kilo, openrouter, poe, vercel, zenmux)

14. **Identifier**: `AnthropicClaudeSonnet46`
    - `anthropic.claude-sonnet-4-6` (amazon-bedrock)
    - `anthropic/claude-sonnet-4-6` (cloudflare-ai-gateway, perplexity-agent, requesty)
    - `anthropic/claude-sonnet-4.6` (kilo, nano-gpt, openrouter, poe, vercel, zenmux)

15. **Identifier**: `BaiduErnie4521bA3b`
    - `baidu/ernie-4.5-21B-a3b` (novita-ai)
    - `baidu/ernie-4.5-21b-a3b` (kilo)

16. **Identifier**: `BaiduErnie4521bA3bThinking`
    - `baidu/ernie-4.5-21B-a3b-thinking` (novita-ai)
    - `baidu/ernie-4.5-21b-a3b-thinking` (kilo)

17. **Identifier**: `BaiduErnie45300bA47b`
    - `baidu/ERNIE-4.5-300B-A47B` (siliconflow, siliconflow-cn)
    - `baidu/ernie-4.5-300b-a47b` (kilo, nano-gpt)

18. **Identifier**: `Claude35Haiku`
    - `claude-3-5-haiku` (llmgateway, opencode)
    - `claude-3.5-haiku` (helicone, qiniu-ai)

19. **Identifier**: `Claude35Haiku20241022`
    - `claude-3-5-haiku-20241022` (anthropic, nano-gpt)
    - `claude-3-5-haiku@20241022` (google-vertex-anthropic)

20. **Identifier**: `Claude35Sonnet`
    - `claude-3-5-sonnet` (llmgateway)
    - `claude-3.5-sonnet` (qiniu-ai)

21. **Identifier**: `Claude35Sonnet20241022`
    - `claude-3-5-sonnet-20241022` (anthropic, llmgateway, nano-gpt)
    - `claude-3-5-sonnet@20241022` (google-vertex-anthropic)

22. **Identifier**: `Claude37Sonnet`
    - `claude-3-7-sonnet` (llmgateway)
    - `claude-3.7-sonnet` (helicone, qiniu-ai)

23. **Identifier**: `Claude37Sonnet20250219`
    - `claude-3-7-sonnet-20250219` (abacus, anthropic, llmgateway, nano-gpt)
    - `claude-3-7-sonnet@20250219` (google-vertex-anthropic)

24. **Identifier**: `Claude45Sonnet`
    - `claude-4-5-sonnet` (cortecs)
    - `claude-4.5-sonnet` (helicone, qiniu-ai)

25. **Identifier**: `ClaudeHaiku45`
    - `claude-haiku-4-5` (aihubmix, anthropic, azure, azure-cognitive-services, cortecs, firmware, llmgateway, opencode)
    - `claude-haiku-4.5` (github-copilot)

26. **Identifier**: `ClaudeHaiku4520251001`
    - `claude-haiku-4-5-20251001` (302ai, abacus, anthropic, helicone, jiekou, llmgateway, nano-gpt, qihang-ai)
    - `claude-haiku-4-5@20251001` (google-vertex-anthropic)

27. **Identifier**: `ClaudeOpus41`
    - `claude-opus-4-1` (aihubmix, anthropic, azure, azure-cognitive-services, helicone, opencode)
    - `claude-opus-41` (github-copilot)

28. **Identifier**: `ClaudeOpus4120250805`
    - `claude-opus-4-1-20250805` (302ai, abacus, anthropic, helicone, jiekou, llmgateway, nano-gpt)
    - `claude-opus-4-1@20250805` (google-vertex-anthropic)

29. **Identifier**: `ClaudeOpus420250514`
    - `claude-opus-4-20250514` (abacus, anthropic, jiekou, llmgateway, nano-gpt)
    - `claude-opus-4@20250514` (google-vertex-anthropic)

30. **Identifier**: `ClaudeOpus45`
    - `claude-opus-4-5` (aihubmix, anthropic, azure, azure-cognitive-services, firmware, opencode)
    - `claude-opus-4.5` (github-copilot)
    - `claude-opus-45` (venice)
    - `claude-opus4-5` (cortecs)

31. **Identifier**: `ClaudeOpus4520251101`
    - `claude-opus-4-5-20251101` (302ai, abacus, anthropic, jiekou, llmgateway, nano-gpt, qihang-ai)
    - `claude-opus-4-5@20251101` (google-vertex-anthropic)

32. **Identifier**: `ClaudeOpus4520251101Thinking`
    - `claude-opus-4-5-20251101-thinking` (302ai)
    - `claude-opus-4-5-20251101:thinking` (nano-gpt)

33. **Identifier**: `ClaudeOpus46`
    - `claude-opus-4-6` (abacus, aihubmix, anthropic, azure, azure-cognitive-services, firmware, jiekou, llmgateway, opencode, venice)
    - `claude-opus-4.6` (github-copilot)
    - `claude-opus4-6` (cortecs)

34. **Identifier**: `ClaudeSonnet420250514`
    - `claude-sonnet-4-20250514` (abacus, anthropic, jiekou, llmgateway, nano-gpt)
    - `claude-sonnet-4@20250514` (google-vertex-anthropic)

35. **Identifier**: `ClaudeSonnet45`
    - `claude-sonnet-4-5` (aihubmix, anthropic, azure, azure-cognitive-services, firmware, llmgateway, opencode)
    - `claude-sonnet-4.5` (github-copilot)
    - `claude-sonnet-45` (venice)

36. **Identifier**: `ClaudeSonnet4520250929`
    - `claude-sonnet-4-5-20250929` (302ai, abacus, anthropic, helicone, jiekou, llmgateway, nano-gpt, qihang-ai)
    - `claude-sonnet-4-5@20250929` (google-vertex-anthropic)

37. **Identifier**: `ClaudeSonnet46`
    - `claude-sonnet-4-6` (abacus, aihubmix, anthropic, firmware, llmgateway, opencode, venice)
    - `claude-sonnet-4.6` (github-copilot)

38. **Identifier**: `CohereCommandA`
    - `cohere-command-a` (azure, azure-cognitive-services)
    - `cohere/command-a` (kilo, vercel)

39. **Identifier**: `CohereCommandR082024`
    - `cohere-command-r-08-2024` (azure, azure-cognitive-services)
    - `cohere/command-r-08-2024` (kilo)

40. **Identifier**: `CohereCommandRPlus082024`
    - `cohere-command-r-plus-08-2024` (azure, azure-cognitive-services)
    - `cohere/command-r-plus-08-2024` (kilo, nano-gpt)

41. **Identifier**: `CohereEmbedV40`
    - `cohere-embed-v-4-0` (azure, azure-cognitive-services)
    - `cohere/embed-v4.0` (vercel)

42. **Identifier**: `DeepseekAIDeepseekR1`
    - `deepseek-ai/DeepSeek-R1` (abacus, siliconflow, siliconflow-cn, togetherai)
    - `deepseek-ai/deepseek-r1` (nvidia)

43. **Identifier**: `DeepseekAIDeepseekR10528`
    - `deepseek-ai/DeepSeek-R1-0528` (deepinfra, huggingface, io-net, meganova, nano-gpt, nebius, submodel)
    - `deepseek-ai/deepseek-r1-0528` (nvidia)

44. **Identifier**: `DeepseekAIDeepseekR1DistillLlama70b`
    - `deepseek-ai/DeepSeek-R1-Distill-Llama-70B` (chutes)
    - `deepseek-ai/deepseek-r1-distill-llama-70b` (fastrouter)

45. **Identifier**: `DeepseekAIDeepseekV31`
    - `deepseek-ai/DeepSeek-V3-1` (togetherai)
    - `deepseek-ai/DeepSeek-V3.1` (baseten, meganova, nano-gpt, siliconflow, submodel, wandb)
    - `deepseek-ai/deepseek-v3.1` (nvidia)

46. **Identifier**: `DeepseekAIDeepseekV31Terminus`
    - `deepseek-ai/DeepSeek-V3.1-Terminus` (abacus, nano-gpt, siliconflow, siliconflow-cn)
    - `deepseek-ai/deepseek-v3.1-terminus` (nvidia)

47. **Identifier**: `DeepseekAIDeepseekV32`
    - `deepseek-ai/DeepSeek-V3.2` (abacus, baseten, deepinfra, huggingface, meganova, nebius, siliconflow, siliconflow-cn)
    - `deepseek-ai/deepseek-v3.2` (nvidia)

48. **Identifier**: `DeepseekAIDeepseekV32Exp`
    - `deepseek-ai/DeepSeek-V3.2-Exp` (meganova, siliconflow)
    - `deepseek-ai/deepseek-v3.2-exp` (nano-gpt)

49. **Identifier**: `DeepseekDeepseekV32Thinking`
    - `deepseek/deepseek-v3.2-thinking` (vercel)
    - `deepseek/deepseek-v3.2:thinking` (nano-gpt)

50. **Identifier**: `DeepseekV31`
    - `deepseek-v3-1` (alibaba-cn)
    - `deepseek-v3.1` (azure, azure-cognitive-services, llmgateway, qiniu-ai)

51. **Identifier**: `DeepseekV32`
    - `DeepSeek-V3.2` (vultr)
    - `deepseek-v3-2` (firmware)
    - `deepseek-v3.2` (302ai, aihubmix, azure, azure-cognitive-services, helicone, iflowcn, llmgateway, ollama-cloud, venice, vivgrid)
    - `deepseek.v3.2` (amazon-bedrock)

52. **Identifier**: `EssentialaiRnj1Instruct`
    - `essentialai/Rnj-1-Instruct` (togetherai)
    - `essentialai/rnj-1-instruct` (kilo, nano-gpt)

53. **Identifier**: `GPT53Codex`
    - `gpt-5-3-codex` (firmware)
    - `gpt-5.3-codex` (abacus, azure, azure-cognitive-services, github-copilot, llmgateway, openai, opencode, vivgrid)

54. **Identifier**: `GPT54`
    - `gpt-5-4` (firmware)
    - `gpt-5.4` (abacus, azure, azure-cognitive-services, github-copilot, llmgateway, openai, opencode, vivgrid)

55. **Identifier**: `GPTOss120b`
    - `gpt-oss-120b` (cerebras, cortecs, dinference, firmware, helicone, llmgateway, ovhcloud, privatemode-ai, qiniu-ai, scaleway, vultr)
    - `gpt-oss:120b` (ollama-cloud)

56. **Identifier**: `GPTOss20b`
    - `gpt-oss-20b` (firmware, helicone, llmgateway, ovhcloud, qiniu-ai)
    - `gpt-oss:20b` (ollama-cloud)

57. **Identifier**: `Gemini31ProPreview`
    - `gemini-3-1-pro-preview` (firmware, venice)
    - `gemini-3.1-pro-preview` (abacus, github-copilot, google, google-vertex, llmgateway, vivgrid)

58. **Identifier**: `Gemma327b`
    - `gemma-3-27b` (llmgateway, privatemode-ai)
    - `gemma3:27b` (ollama-cloud)

59. **Identifier**: `Gemma327bIt`
    - `Gemma-3-27B-it` (nano-gpt)
    - `gemma-3-27b-it` (google, scaleway)

60. **Identifier**: `Glm47`
    - `GLM-4.7` (kuae-cloud-coding-plan, moark)
    - `glm-4.7` (302ai, aihubmix, alibaba-coding-plan, alibaba-coding-plan-cn, cortecs, dinference, llmgateway, ollama-cloud, opencode, zai, zai-coding-plan, zhipuai, zhipuai-coding-plan)

61. **Identifier**: `GoogleGemma312bIt`
    - `google.gemma-3-12b-it` (amazon-bedrock)
    - `google/gemma-3-12b-it` (kilo, nvidia, openrouter)

62. **Identifier**: `GoogleGemma327bIt`
    - `google-gemma-3-27b-it` (venice)
    - `google.gemma-3-27b-it` (amazon-bedrock)
    - `google/gemma-3-27b-it` (kilo, nebius, novita-ai, nvidia, openrouter, stackit)

63. **Identifier**: `GoogleGemma34bIt`
    - `google.gemma-3-4b-it` (amazon-bedrock)
    - `google/gemma-3-4b-it` (kilo, openrouter)

64. **Identifier**: `GoogleGemma426bA4bIt`
    - `google.gemma-4-26b-a4b-it` (venice)
    - `google/gemma-4-26b-a4b-it` (openrouter, vercel)

65. **Identifier**: `GoogleGemma431bIt`
    - `google.gemma-4-31b-it` (venice)
    - `google/gemma-4-31b-it` (nvidia, openrouter, vercel)

66. **Identifier**: `Grok41Fast`
    - `grok-4-1-fast` (llmgateway, xai)
    - `grok-41-fast` (venice)

67. **Identifier**: `GrypheMythomaxL213b`
    - `Gryphe/MythoMax-L2-13b` (nano-gpt)
    - `gryphe/mythomax-l2-13b` (kilo, novita-ai)

68. **Identifier**: `KiloAutoFree`
    - `kilo-auto/free` (kilo)
    - `kilo/auto-free` (kilo)

69. **Identifier**: `KiloAutoSmall`
    - `kilo-auto/small` (kilo)
    - `kilo/auto-small` (kilo)

70. **Identifier**: `KimiK20905`
    - `Kimi-K2-0905` (aihubmix)
    - `kimi-k2-0905` (helicone, iflowcn)

71. **Identifier**: `KimiK25`
    - `Kimi-K2.5` (vultr)
    - `kimi-k2-5` (venice)
    - `kimi-k2.5` (abacus, aihubmix, alibaba-cn, alibaba-coding-plan, alibaba-coding-plan-cn, azure, azure-cognitive-services, cortecs, firmware, llmgateway, moonshotai, moonshotai-cn, ollama-cloud, opencode, opencode-go, tencent-coding-plan)

72. **Identifier**: `MetaLlama31405bInstruct`
    - `meta-llama-3.1-405b-instruct` (azure, azure-cognitive-services)
    - `meta/llama-3.1-405b-instruct` (nvidia)

73. **Identifier**: `MetaLlama3170bInstruct`
    - `meta-llama-3.1-70b-instruct` (azure, azure-cognitive-services)
    - `meta/llama-3.1-70b-instruct` (nvidia)

74. **Identifier**: `MetaLlama318bInstruct`
    - `meta-llama-3.1-8b-instruct` (azure, azure-cognitive-services)
    - `meta/llama-3.1-8b-instruct` (inference)

75. **Identifier**: `MetaLlama3370bInstruct`
    - `meta-llama-3_3-70b-instruct` (ovhcloud)
    - `meta/llama-3.3-70b-instruct` (github-models, nvidia)

76. **Identifier**: `MetaLlama370bInstruct`
    - `meta-llama-3-70b-instruct` (azure, azure-cognitive-services)
    - `meta/llama3-70b-instruct` (nvidia)

77. **Identifier**: `MetaLlama38bInstruct`
    - `meta-llama-3-8b-instruct` (azure, azure-cognitive-services)
    - `meta/llama3-8b-instruct` (nvidia)

78. **Identifier**: `MetaLlamaLlama3170bInstruct`
    - `meta-llama/Llama-3.1-70B-Instruct` (deepinfra, wandb)
    - `meta-llama/llama-3.1-70b-instruct` (kilo)

79. **Identifier**: `MetaLlamaLlama318bInstruct`
    - `meta-llama/Llama-3.1-8B-Instruct` (deepinfra, friendli, wandb)
    - `meta-llama/llama-3.1-8b-instruct` (kilo, nano-gpt, novita-ai)

80. **Identifier**: `MetaLlamaLlama3290bVisionInstruct`
    - `meta-llama/Llama-3.2-90B-Vision-Instruct` (io-net)
    - `meta-llama/llama-3.2-90b-vision-instruct` (nano-gpt)

81. **Identifier**: `MetaLlamaLlama3370bInstruct`
    - `meta-llama/Llama-3.3-70B-Instruct` (berget, cloudferro-sherlock, friendli, io-net, meganova, nebius, wandb)
    - `meta-llama/llama-3.3-70b-instruct` (kilo, nano-gpt, novita-ai)

82. **Identifier**: `MetaLlamaLlama4Maverick17b128eInstructFp8`
    - `meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8` (abacus, deepinfra, io-net)
    - `meta-llama/llama-4-maverick-17b-128e-instruct-fp8` (novita-ai)

83. **Identifier**: `MetaLlamaLlama4Scout17b16eInstruct`
    - `meta-llama/Llama-4-Scout-17B-16E-Instruct` (deepinfra, wandb)
    - `meta-llama/llama-4-scout-17b-16e-instruct` (groq, novita-ai)

84. **Identifier**: `MetaLlamaLlamaGuard38b`
    - `meta-llama/Llama-Guard-3-8B` (nebius)
    - `meta-llama/llama-guard-3-8b` (kilo)

85. **Identifier**: `MicrosoftPhi4MiniInstruct`
    - `microsoft/Phi-4-mini-instruct` (wandb)
    - `microsoft/phi-4-mini-instruct` (github-models, nvidia)

86. **Identifier**: `MicrosoftPhi4MultimodalInstruct`
    - `microsoft/Phi-4-multimodal-instruct` (evroc)
    - `microsoft/phi-4-multimodal-instruct` (github-models)

87. **Identifier**: `MinimaxM2`
    - `MiniMax-M2` (302ai, minimax, minimax-cn, minimax-cn-coding-plan, minimax-coding-plan, nano-gpt)
    - `minimax-m2` (cortecs, llmgateway, ollama-cloud)

88. **Identifier**: `MinimaxM21`
    - `MiniMax-M2.1` (302ai, minimax, minimax-cn, minimax-cn-coding-plan, minimax-coding-plan, moark)
    - `minimax-m2.1` (aihubmix, cortecs, llmgateway, ollama-cloud, opencode)
    - `minimax-m21` (venice)

89. **Identifier**: `MinimaxM25`
    - `MiniMax-M2.5` (alibaba-cn, alibaba-coding-plan, alibaba-coding-plan-cn, minimax, minimax-cn, minimax-cn-coding-plan, minimax-coding-plan, vultr)
    - `minimax-m2-5` (firmware)
    - `minimax-m2.5` (aihubmix, cortecs, llmgateway, ollama-cloud, opencode, opencode-go, tencent-coding-plan)
    - `minimax-m25` (venice)

90. **Identifier**: `MinimaxM25Highspeed`
    - `MiniMax-M2.5-highspeed` (minimax, minimax-cn, minimax-cn-coding-plan, minimax-coding-plan)
    - `minimax-m2.5-highspeed` (llmgateway)

91. **Identifier**: `MinimaxM27`
    - `MiniMax-M2.7` (minimax, minimax-cn, minimax-cn-coding-plan, minimax-coding-plan)
    - `minimax-m2.7` (llmgateway, ollama-cloud, opencode-go)
    - `minimax-m27` (venice)

92. **Identifier**: `MinimaxM27Highspeed`
    - `MiniMax-M2.7-highspeed` (minimax, minimax-cn, minimax-cn-coding-plan, minimax-coding-plan)
    - `minimax-m2.7-highspeed` (llmgateway)

93. **Identifier**: `MinimaxMinimaxM2`
    - `minimax.minimax-m2` (amazon-bedrock)
    - `minimax/minimax-m2` (kilo, novita-ai, openrouter, qiniu-ai, vercel, zenmux)

94. **Identifier**: `MinimaxMinimaxM21`
    - `minimax.minimax-m2.1` (amazon-bedrock)
    - `minimax/minimax-m2.1` (jiekou, kilo, nano-gpt, novita-ai, openrouter, qiniu-ai, vercel, zenmux)

95. **Identifier**: `MinimaxMinimaxM25`
    - `minimax.minimax-m2.5` (amazon-bedrock)
    - `minimax/minimax-m2.5` (kilo, nano-gpt, novita-ai, openrouter, qiniu-ai, vercel, zenmux)

96. **Identifier**: `MinimaxMinimaxM27`
    - `MiniMax/MiniMax-M2.7` (alibaba-cn)
    - `minimax/minimax-m2.7` (nano-gpt, openrouter, vercel, zenmux)

97. **Identifier**: `MinimaxaiMinimaxM180k`
    - `MiniMaxAI/MiniMax-M1-80k` (nano-gpt)
    - `minimaxai/minimax-m1-80k` (jiekou, novita-ai)

98. **Identifier**: `MinimaxaiMinimaxM21`
    - `MiniMaxAI/MiniMax-M2.1` (deepinfra, huggingface, meganova, nebius, siliconflow)
    - `minimaxai/minimax-m2.1` (nvidia)

99. **Identifier**: `MinimaxaiMinimaxM25`
    - `MiniMaxAI/MiniMax-M2.5` (baseten, cloudferro-sherlock, deepinfra, friendli, huggingface, meganova, siliconflow, togetherai, wandb)
    - `minimaxai/minimax-m2.5` (nvidia)

100. **Identifier**: `MiromindAIMirothinkerV15235b`
     - `miromind-ai/MiroThinker-v1.5-235B` (chutes)
     - `miromind-ai/mirothinker-v1.5-235b` (nano-gpt)

101. **Identifier**: `MistralaiDevstralSmall2505`
     - `mistralai/Devstral-Small-2505` (io-net, nano-gpt)
     - `mistralai/devstral-small-2505` (openrouter)

102. **Identifier**: `MistralaiVoxtralSmall24b2507`
     - `mistralai/Voxtral-Small-24B-2507` (evroc)
     - `mistralai/voxtral-small-24b-2507` (kilo)

103. **Identifier**: `MoonshotaiKimiK25`
     - `moonshotai.kimi-k2.5` (amazon-bedrock)
     - `moonshotai/Kimi-K2.5` (baseten, deepinfra, evroc, huggingface, meganova, nebius, siliconflow, togetherai, wandb)
     - `moonshotai/kimi-k2.5` (jiekou, kilo, nano-gpt, novita-ai, nvidia, openrouter, qiniu-ai, vercel, zenmux)

104. **Identifier**: `MoonshotaiKimiK2Instruct`
     - `moonshotai/Kimi-K2-Instruct` (deepinfra, huggingface, nebius, siliconflow)
     - `moonshotai/kimi-k2-instruct` (groq, jiekou, nano-gpt, novita-ai, nvidia)

105. **Identifier**: `MoonshotaiKimiK2Instruct0905`
     - `moonshotai/Kimi-K2-Instruct-0905` (baseten, chutes, deepinfra, huggingface, io-net, nano-gpt, siliconflow, siliconflow-cn)
     - `moonshotai/kimi-k2-instruct-0905` (groq, nvidia)

106. **Identifier**: `MoonshotaiKimiK2Thinking`
     - `moonshotai/Kimi-K2-Thinking` (baseten, deepinfra, huggingface, io-net, meganova, nebius, siliconflow, siliconflow-cn)
     - `moonshotai/kimi-k2-thinking` (kilo, nano-gpt, novita-ai, nvidia, openrouter, qiniu-ai, vercel, zenmux)

107. **Identifier**: `NexAgiDeepseekV31NexN1`
     - `nex-agi/DeepSeek-V3.1-Nex-N1` (siliconflow)
     - `nex-agi/deepseek-v3.1-nex-n1` (kilo, nano-gpt)

108. **Identifier**: `NousresearchHermes4405b`
     - `NousResearch/Hermes-4-405B` (nebius)
     - `nousresearch/hermes-4-405b` (kilo, openrouter)

109. **Identifier**: `NousresearchHermes470b`
     - `NousResearch/Hermes-4-70B` (chutes, nebius)
     - `nousresearch/hermes-4-70b` (kilo, openrouter)

110. **Identifier**: `NvidiaLlama31NemotronUltra253bV1`
     - `nvidia/Llama-3.1-Nemotron-Ultra-253B-v1` (nano-gpt)
     - `nvidia/Llama-3_1-Nemotron-Ultra-253B-v1` (nebius)
     - `nvidia/llama-3.1-nemotron-ultra-253b-v1` (nvidia)

111. **Identifier**: `NvidiaLlama33NemotronSuper49bV1`
     - `nvidia/Llama-3.3-Nemotron-Super-49B-v1` (nano-gpt)
     - `nvidia/llama-3.3-nemotron-super-49b-v1` (nvidia)

112. **Identifier**: `NvidiaLlama33NemotronSuper49bV15`
     - `nvidia/Llama-3_3-Nemotron-Super-49B-v1_5` (nano-gpt)
     - `nvidia/llama-3.3-nemotron-super-49b-v1.5` (kilo, nvidia)

113. **Identifier**: `NvidiaNemotron3Nano30bA3b`
     - `nvidia-nemotron-3-nano-30b-a3b` (venice)
     - `nvidia/nemotron-3-nano-30b-a3b` (kilo, nano-gpt, nvidia, vercel)

114. **Identifier**: `NvidiaNemotronNano9bV2`
     - `nvidia.nemotron-nano-9b-v2` (amazon-bedrock)
     - `nvidia/nemotron-nano-9b-v2` (kilo, openrouter, vercel)

115. **Identifier**: `OpenaiGPT4o20241120`
     - `openai-gpt-4o-2024-11-20` (venice)
     - `openai/gpt-4o-2024-11-20` (kilo, nano-gpt)

116. **Identifier**: `OpenaiGPT4oMini20240718`
     - `openai-gpt-4o-mini-2024-07-18` (venice)
     - `openai/gpt-4o-mini-2024-07-18` (kilo)

117. **Identifier**: `OpenaiGPT52`
     - `openai-gpt-52` (venice)
     - `openai/gpt-5.2` (cloudflare-ai-gateway, kilo, nano-gpt, openrouter, perplexity-agent, poe, qiniu-ai, requesty, vercel, zenmux)

118. **Identifier**: `OpenaiGPT52Codex`
     - `openai-gpt-52-codex` (venice)
     - `openai/gpt-5.2-codex` (cloudflare-ai-gateway, kilo, nano-gpt, openrouter, poe, requesty, vercel, zenmux)

119. **Identifier**: `OpenaiGPT53Codex`
     - `openai-gpt-53-codex` (venice)
     - `openai/gpt-5.3-codex` (cloudflare-ai-gateway, kilo, openrouter, poe, requesty, vercel, zenmux)

120. **Identifier**: `OpenaiGPT54`
     - `openai-gpt-54` (venice)
     - `openai/gpt-5.4` (cloudflare-ai-gateway, kilo, openrouter, perplexity-agent, poe, requesty, vercel, zenmux)

121. **Identifier**: `OpenaiGPT54Mini`
     - `openai-gpt-54-mini` (venice)
     - `openai/gpt-5.4-mini` (openrouter, poe, vercel, zenmux)

122. **Identifier**: `OpenaiGPT54Pro`
     - `openai-gpt-54-pro` (venice)
     - `openai/gpt-5.4-pro` (kilo, openrouter, poe, requesty, vercel, zenmux)

123. **Identifier**: `OpenaiGPTOss120b`
     - `openai-gpt-oss-120b` (venice)
     - `openai/gpt-oss-120b` (abacus, baseten, berget, cloudferro-sherlock, deepinfra, evroc, fastrouter, groq, io-net, kilo, nano-gpt, nebius, novita-ai, nvidia, openrouter, siliconflow, stackit, submodel, togetherai, vercel, wandb)

124. **Identifier**: `OpenaiGPTOssSafeguard20b`
     - `openai.gpt-oss-safeguard-20b` (amazon-bedrock)
     - `openai/gpt-oss-safeguard-20b` (groq, kilo, nano-gpt, openrouter, vercel)

125. **Identifier**: `PaddlepaddlePaddleocrVl`
     - `PaddlePaddle/PaddleOCR-VL` (siliconflow-cn)
     - `paddlepaddle/paddleocr-vl` (novita-ai)

126. **Identifier**: `Qwen25Coder32bInstruct`
     - `qwen2-5-coder-32b-instruct` (alibaba-cn)
     - `qwen2.5-coder-32b-instruct` (ovhcloud)

127. **Identifier**: `Qwen25Vl72bInstruct`
     - `qwen2-5-vl-72b-instruct` (alibaba, alibaba-cn, llmgateway)
     - `qwen2.5-vl-72b-instruct` (ovhcloud, qiniu-ai)
     - `qwen25-vl-72b-instruct` (nano-gpt)

128. **Identifier**: `Qwen25Vl7bInstruct`
     - `qwen2-5-vl-7b-instruct` (alibaba, alibaba-cn)
     - `qwen2.5-vl-7b-instruct` (qiniu-ai)

129. **Identifier**: `Qwen3235bA22bInstruct2507`
     - `qwen-3-235b-a22b-instruct-2507` (cerebras)
     - `qwen3-235b-a22b-instruct-2507` (302ai, aihubmix, llmgateway, qiniu-ai, scaleway, venice)

130. **Identifier**: `Qwen35397bA17b`
     - `qwen3.5-397b-a17b` (alibaba, alibaba-cn, qiniu-ai, scaleway)
     - `qwen35-397b-a17b` (llmgateway)

131. **Identifier**: `Qwen36Plus`
     - `qwen-3-6-plus` (venice)
     - `qwen3.6-plus` (alibaba, alibaba-cn, alibaba-coding-plan, alibaba-coding-plan-cn)

132. **Identifier**: `Qwen3Next80b`
     - `qwen3-next-80b` (venice)
     - `qwen3-next:80b` (ollama-cloud)

133. **Identifier**: `QwenQwen2572bInstruct`
     - `Qwen/Qwen2.5-72B-Instruct` (abacus, chutes, siliconflow, siliconflow-cn)
     - `qwen/qwen-2.5-72b-instruct` (kilo, novita-ai)

134. **Identifier**: `QwenQwen257bInstruct`
     - `Qwen/Qwen2.5-7B-Instruct` (siliconflow, siliconflow-cn)
     - `qwen/qwen-2.5-7b-instruct` (kilo)
     - `qwen/qwen2.5-7b-instruct` (novita-ai)

135. **Identifier**: `QwenQwen25Coder32bInstruct`
     - `Qwen/Qwen2.5-Coder-32B-Instruct` (chutes, siliconflow, siliconflow-cn)
     - `qwen/qwen-2.5-coder-32b-instruct` (kilo, openrouter)
     - `qwen/qwen2.5-coder-32b-instruct` (nvidia)

136. **Identifier**: `QwenQwen25Vl32bInstruct`
     - `Qwen/Qwen2.5-VL-32B-Instruct` (chutes, io-net, meganova, siliconflow, siliconflow-cn)
     - `qwen/qwen2.5-vl-32b-instruct` (kilo)

137. **Identifier**: `QwenQwen25Vl72bInstruct`
     - `Qwen/Qwen2.5-VL-72B-Instruct` (nebius, siliconflow, siliconflow-cn)
     - `qwen/qwen2.5-vl-72b-instruct` (kilo, novita-ai, openrouter)

138. **Identifier**: `QwenQwen25Vl7bInstruct`
     - `Qwen/Qwen2.5-VL-7B-Instruct` (siliconflow)
     - `qwen/qwen-2.5-vl-7b-instruct` (kilo)

139. **Identifier**: `QwenQwen314b`
     - `Qwen/Qwen3-14B` (chutes, siliconflow, siliconflow-cn)
     - `qwen/qwen3-14b` (kilo)

140. **Identifier**: `QwenQwen3235bA22b`
     - `Qwen/Qwen3-235B-A22B` (chutes, siliconflow)
     - `qwen/qwen3-235b-a22b` (kilo, nvidia)

141. **Identifier**: `QwenQwen3235bA22bInstruct2507`
     - `Qwen/Qwen3-235B-A22B-Instruct-2507` (abacus, friendli, meganova, modelscope, nebius, siliconflow, siliconflow-cn, submodel, wandb)
     - `qwen/qwen3-235b-a22b-instruct-2507` (jiekou, novita-ai)

142. **Identifier**: `QwenQwen3235bA22bThinking2507`
     - `Qwen/Qwen3-235B-A22B-Thinking-2507` (chutes, huggingface, io-net, modelscope, nebius, siliconflow, siliconflow-cn, submodel, wandb)
     - `qwen/qwen3-235b-a22b-thinking-2507` (jiekou, kilo, novita-ai, openrouter)

143. **Identifier**: `QwenQwen330bA3b`
     - `Qwen/Qwen3-30B-A3B` (chutes)
     - `qwen/qwen3-30b-a3b` (kilo)

144. **Identifier**: `QwenQwen330bA3bInstruct2507`
     - `Qwen/Qwen3-30B-A3B-Instruct-2507` (chutes, modelscope, nebius, siliconflow, siliconflow-cn, wandb)
     - `qwen/qwen3-30b-a3b-instruct-2507` (kilo, openrouter)

145. **Identifier**: `QwenQwen330bA3bThinking2507`
     - `Qwen/Qwen3-30B-A3B-Thinking-2507` (modelscope, nebius, siliconflow, siliconflow-cn)
     - `qwen/qwen3-30b-a3b-thinking-2507` (kilo, openrouter)

146. **Identifier**: `QwenQwen332b`
     - `Qwen/Qwen3-32B` (abacus, chutes, nebius, siliconflow, siliconflow-cn)
     - `qwen/qwen3-32b` (groq, kilo)

147. **Identifier**: `QwenQwen35122bA10b`
     - `Qwen/Qwen3.5-122B-A10B` (siliconflow-cn)
     - `qwen/qwen3.5-122b-a10b` (kilo, mixlayer)

148. **Identifier**: `QwenQwen3527b`
     - `Qwen/Qwen3.5-27B` (siliconflow-cn)
     - `qwen/qwen3.5-27b` (kilo, mixlayer)

149. **Identifier**: `QwenQwen3535bA3b`
     - `Qwen/Qwen3.5-35B-A3B` (siliconflow-cn)
     - `qwen/qwen3.5-35b-a3b` (kilo, mixlayer)

150. **Identifier**: `QwenQwen35397bA17b`
     - `Qwen/Qwen3.5-397B-A17B` (huggingface, siliconflow-cn, togetherai)
     - `qwen/qwen3.5-397b-a17b` (kilo, mixlayer, nano-gpt, novita-ai, nvidia, openrouter)

151. **Identifier**: `QwenQwen359b`
     - `Qwen/Qwen3.5-9B` (siliconflow-cn)
     - `qwen/qwen3.5-9b` (kilo, mixlayer)

152. **Identifier**: `QwenQwen35Plus`
     - `Qwen/Qwen3.5-Plus` (meganova)
     - `qwen/qwen3.5-plus` (zenmux)

153. **Identifier**: `QwenQwen38b`
     - `Qwen/Qwen3-8B` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-8b` (kilo)

154. **Identifier**: `QwenQwen3Coder30bA3bInstruct`
     - `Qwen/Qwen3-Coder-30B-A3B-Instruct` (modelscope, nebius, siliconflow, siliconflow-cn)
     - `qwen/qwen3-coder-30b-a3b-instruct` (kilo, novita-ai, openrouter)

155. **Identifier**: `QwenQwen3Coder480bA35bInstruct`
     - `Qwen/Qwen3-Coder-480B-A35B-Instruct` (deepinfra, huggingface, nebius, siliconflow, siliconflow-cn, wandb)
     - `Qwen/qwen3-coder-480b-a35b-instruct` (abacus)
     - `qwen/qwen3-coder-480b-a35b-instruct` (jiekou, novita-ai, nvidia)

156. **Identifier**: `QwenQwen3CoderNext`
     - `Qwen/Qwen3-Coder-Next` (chutes, huggingface)
     - `qwen.qwen3-coder-next` (amazon-bedrock)
     - `qwen/qwen3-coder-next` (jiekou, kilo, novita-ai)

157. **Identifier**: `QwenQwen3Embedding4b`
     - `Qwen/Qwen3-Embedding-4B` (huggingface)
     - `qwen/qwen3-embedding-4b` (inference)

158. **Identifier**: `QwenQwen3Next80bA3bInstruct`
     - `Qwen/Qwen3-Next-80B-A3B-Instruct` (chutes, huggingface, io-net, siliconflow, siliconflow-cn)
     - `qwen/qwen3-next-80b-a3b-instruct` (jiekou, kilo, novita-ai, nvidia, openrouter)

159. **Identifier**: `QwenQwen3Next80bA3bThinking`
     - `Qwen/Qwen3-Next-80B-A3B-Thinking` (huggingface, nebius, siliconflow, siliconflow-cn)
     - `qwen/qwen3-next-80b-a3b-thinking` (jiekou, kilo, novita-ai, nvidia, openrouter)

160. **Identifier**: `QwenQwen3Omni30bA3bInstruct`
     - `Qwen/Qwen3-Omni-30B-A3B-Instruct` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-omni-30b-a3b-instruct` (novita-ai)

161. **Identifier**: `QwenQwen3Omni30bA3bThinking`
     - `Qwen/Qwen3-Omni-30B-A3B-Thinking` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-omni-30b-a3b-thinking` (novita-ai)

162. **Identifier**: `QwenQwen3Vl235bA22bInstruct`
     - `Qwen/Qwen3-VL-235B-A22B-Instruct` (chutes, siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-235b-a22b-instruct` (kilo, novita-ai)

163. **Identifier**: `QwenQwen3Vl235bA22bThinking`
     - `Qwen/Qwen3-VL-235B-A22B-Thinking` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-235b-a22b-thinking` (kilo, novita-ai)

164. **Identifier**: `QwenQwen3Vl30bA3bInstruct`
     - `Qwen/Qwen3-VL-30B-A3B-Instruct` (evroc, siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-30b-a3b-instruct` (kilo, novita-ai)

165. **Identifier**: `QwenQwen3Vl30bA3bThinking`
     - `Qwen/Qwen3-VL-30B-A3B-Thinking` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-30b-a3b-thinking` (kilo, novita-ai)

166. **Identifier**: `QwenQwen3Vl32bInstruct`
     - `Qwen/Qwen3-VL-32B-Instruct` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-32b-instruct` (kilo)

167. **Identifier**: `QwenQwen3Vl8bInstruct`
     - `Qwen/Qwen3-VL-8B-Instruct` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-8b-instruct` (kilo, novita-ai)

168. **Identifier**: `QwenQwen3Vl8bThinking`
     - `Qwen/Qwen3-VL-8B-Thinking` (siliconflow, siliconflow-cn)
     - `qwen/qwen3-vl-8b-thinking` (kilo)

169. **Identifier**: `QwenQwq32b`
     - `Qwen/QwQ-32B` (abacus, siliconflow, siliconflow-cn)
     - `qwen-qwq-32b` (groq)
     - `qwen/qwq-32b` (kilo, nvidia)

170. **Identifier**: `Sao10kL3170bEuryaleV22`
     - `Sao10K/L3.1-70B-Euryale-v2.2` (nano-gpt)
     - `sao10k/l31-70b-euryale-v2.2` (novita-ai)

171. **Identifier**: `Sao10kL3170bHanamiX1`
     - `Sao10K/L3.1-70B-Hanami-x1` (nano-gpt)
     - `sao10k/l3.1-70b-hanami-x1` (kilo)

172. **Identifier**: `Sao10kL38bSthenoV32`
     - `Sao10K/L3-8B-Stheno-v3.2` (nano-gpt)
     - `sao10k/L3-8B-Stheno-v3.2` (novita-ai)

173. **Identifier**: `StepfunAIStep35Flash`
     - `stepfun-ai/Step-3.5-Flash` (siliconflow, siliconflow-cn)
     - `stepfun-ai/step-3.5-flash` (nano-gpt, nvidia)

174. **Identifier**: `StepfunStep35FlashFree`
     - `stepfun/step-3.5-flash-free` (zenmux)
     - `stepfun/step-3.5-flash:free` (kilo, openrouter)

175. **Identifier**: `TencentHunyuanA13bInstruct`
     - `tencent/Hunyuan-A13B-Instruct` (siliconflow, siliconflow-cn)
     - `tencent/hunyuan-a13b-instruct` (kilo)

176. **Identifier**: `XaiGrok41FastNonReasoning`
     - `xai/grok-4-1-fast-non-reasoning` (perplexity-agent)
     - `xai/grok-4.1-fast-non-reasoning` (poe, vercel)

177. **Identifier**: `XiaomimimoMimoV2Flash`
     - `XiaomiMiMo/MiMo-V2-Flash` (chutes, huggingface, meganova)
     - `xiaomimimo/mimo-v2-flash` (jiekou, novita-ai)

178. **Identifier**: `ZAIGlm47`
     - `z-ai/glm-4.7` (kilo, openrouter, qiniu-ai, zenmux)
     - `z-ai/glm4.7` (nvidia)

179. **Identifier**: `ZAIGlm5`
     - `z-ai/glm-5` (fastrouter, kilo, openrouter, qiniu-ai, zenmux)
     - `z-ai/glm5` (nvidia)

180. **Identifier**: `ZaiGlm47`
     - `zai-glm-4.7` (cerebras)
     - `zai.glm-4.7` (amazon-bedrock)
     - `zai/glm-4.7` (vercel)

181. **Identifier**: `ZaiGlm47Flash`
     - `zai.glm-4.7-flash` (amazon-bedrock)
     - `zai/glm-4.7-flash` (vercel)

182. **Identifier**: `ZaiGlm5`
     - `zai-glm-5` (firmware)
     - `zai.glm-5` (amazon-bedrock)
     - `zai/glm-5` (vercel)

183. **Identifier**: `ZaiOrgGlm45`
     - `zai-org/GLM-4.5` (deepinfra, nebius, siliconflow)
     - `zai-org/glm-4.5` (abacus, jiekou, novita-ai)

184. **Identifier**: `ZaiOrgGlm45Air`
     - `zai-org/GLM-4.5-Air` (chutes, nebius, siliconflow, siliconflow-cn, submodel)
     - `zai-org/glm-4.5-air` (novita-ai)

185. **Identifier**: `ZaiOrgGlm45v`
     - `zai-org/GLM-4.5V` (siliconflow, siliconflow-cn)
     - `zai-org/glm-4.5v` (jiekou, novita-ai)

186. **Identifier**: `ZaiOrgGlm46`
     - `zai-org-glm-4.6` (venice)
     - `zai-org/GLM-4.6` (baseten, deepinfra, io-net, meganova, siliconflow, siliconflow-cn)
     - `zai-org/glm-4.6` (abacus, novita-ai)

187. **Identifier**: `ZaiOrgGlm46v`
     - `zai-org/GLM-4.6V` (chutes, deepinfra, siliconflow, siliconflow-cn)
     - `zai-org/glm-4.6v` (novita-ai)

188. **Identifier**: `ZaiOrgGlm47`
     - `zai-org-glm-4.7` (venice)
     - `zai-org/GLM-4.7` (baseten, berget, deepinfra, huggingface, meganova, siliconflow)
     - `zai-org/glm-4.7` (abacus, jiekou, nano-gpt, novita-ai)

189. **Identifier**: `ZaiOrgGlm47Flash`
     - `zai-org-glm-4.7-flash` (venice)
     - `zai-org/GLM-4.7-Flash` (chutes, deepinfra, huggingface)
     - `zai-org/glm-4.7-flash` (jiekou, nano-gpt, novita-ai)

190. **Identifier**: `ZaiOrgGlm5`
     - `zai-org-glm-5` (venice)
     - `zai-org/GLM-5` (baseten, deepinfra, friendli, huggingface, meganova, nebius, siliconflow, togetherai)
     - `zai-org/glm-5` (abacus, nano-gpt, novita-ai)

191. **Identifier**: `ZaiOrgGlm51`
     - `zai-org-glm-5-1` (venice)
     - `zai-org/GLM-5.1` (friendli, huggingface)
     - `zai-org/glm-5.1` (nano-gpt)

### Within-provider collision risk

Within a single provider's model set (e.g., anthropic's 22 models, openai's 51), there are **zero collisions** for the three currently supported providers. This was verified.

### Collision mitigation strategies

1. **Provider-prefixed model constants**: `ModelAnthropicClaudeOpus45` — eliminates cross-provider collisions but produces long names
2. **Per-provider sub-packages**: `anthropic.ModelClaudeOpus45` — clean but changes import structure significantly
3. **No model ID constants** (recommended initially): Generate Provider constants only. Model IDs are too numerous (4,168), too volatile (new models weekly), and collision-prone. Users already pass `ModelID("claude-opus-4-6")` which is readable enough.
4. **Family-level constants only**: `FamilyClaudeOpus`, `FamilyGPT4` — fewer, more stable, lower collision risk

---

## 4. Downstream Impact

### provenance dependency

`dayvidpham/provenance` imports bestiary through a single adapter file (`adapter.go`):

```go
func RegistryFromBestiary(models []bestiary.ModelInfo) ptypes.ModelRegistry
```

It converts `bestiary.Provider` to `ptypes.Provider` (both `type Provider string`), and `bestiary.ModelID` to `ptypes.ModelID`.

**Impact of expanding providers:**
- `RegistryFromBestiary` would receive more `ModelEntry` values — this is purely additive and backward-compatible
- provenance's own `Provider` type has the same 4 constants (Anthropic, Google, OpenAI, Local) with a **strict `IsValid()` that rejects unknown providers** — this would need updating or relaxing
- No import cycle risk: bestiary does not import provenance

### provenance's Provider validation problem

Provenance's `Provider.IsValid()` in `pkg/ptypes/enums.go` uses a switch statement:
```go
case ProviderAnthropic, ProviderGoogle, ProviderOpenAI, ProviderLocal:
    return true
```

And `UnmarshalText` rejects unknown providers. If bestiary expands to include `ProviderDeepseek` models, provenance would reject them at the adapter boundary unless provenance also expands its Provider type.

**Recommendation**: provenance should switch to an open validation model (accept any string, like bestiary already does) or import provider constants from bestiary.

### Binary size impact

Current state: 110 models from 3 providers, ~100KB generated source, 15MB binary.

Projections:

| Scope | Models | Est. gen source | Est. binary delta |
|-------|--------|-----------------|-------------------|
| Current (3 providers) | 110 | 100 KB | baseline (15 MB) |
| Major providers (~25) | ~630 | ~570 KB | +1-2 MB |
| All providers (110) | 4,168 | ~3.8 MB | +5-8 MB |
| All + model constants | 4,168 | ~4.5 MB | +7-10 MB |

**3.8 MB of generated Go source is large but precedented** — aws-sdk-go-v2 has individual service packages with multi-MB generated files. However, bestiary is a lightweight library, so this may be concerning.

**Recommended approach**: Generate all provider constants (110 constants = negligible size), but make the model data set configurable — either through build tags or a tiered approach (static data for major providers, sync for the rest).

### API churn risk

The models.dev API adds new providers and models frequently. Provider slugs appear stable (none have been renamed based on the git history). Model IDs are also stable once published, but new ones appear weekly.

Generating constants means each `go generate` run could change the constant set. This is fine for provider constants (rare changes) but noisy for model ID constants (frequent changes).

---

## 5. Recommended Approach

### Phase 1: Provider constants (low risk, high value)

1. **Generate `providers_gen.go`** with:
   - One `Provider` constant per API slug (110 constants)
   - A `knownProviders` slice (replaces hand-written one in `provider.go`)
   - A `Providers()` function returning all known values
   - A casing override table in the codegen for known abbreviations (AI, API, LLM, GPT, etc.)

2. **Keep `ProviderLocal`** as a hand-written constant in `provider.go` (it's not from the API)

3. **Update `IsKnown()`** to check the generated `knownProviders` set

4. **Handle `302ai`**: Constant name `Provider302AI` (valid Go since it's prefixed with `Provider`)

5. **Naming convention**: Use `name` field from API for casing hints where possible, fall back to PascalCase of slug. Maintain a small override map for edge cases.

### Phase 2: Expand model data (medium risk, high value)

1. **Remove `targetProviders` filter** in codegen — include all 110 providers
2. **Split generated output** if file size is concerning (by provider group or alphabetically)
3. **Update `providerExpr()`** to use generated constant names instead of a switch

### Phase 3: Model ID constants (higher risk, evaluate need)

1. **Do NOT generate globally-scoped `ModelID` constants** — collision risk is real and the value is low
2. **Consider family-level constants** if there's demand: `FamilyClaudeOpus`, `FamilyGemini2` — these are more stable and useful for grouping
3. **If model constants are needed**, use provider-prefixed names: `ModelAnthropicClaudeOpus46` — and only for a curated subset of providers

### File layout

```
provider.go              # Provider type, ProviderLocal, String/Marshal methods
providers_gen.go         # Generated: 110 Provider constants, knownProviders, Providers()
models_static_gen.go     # Generated: staticModels slice (expanded to all providers)
```

### Casing override table (for codegen)

```go
var casingOverrides = map[string]string{
    "ai":     "AI",
    "api":    "API",
    "gpt":    "GPT",
    "llm":    "LLM",
    "io":     "IO",
    "sap":    "SAP",
    "ovh":    "OVH",
    "cn":     "CN",
    "ams":    "AMS",
    "sgp":    "SGP",
}
```

This keeps the codegen deterministic and produces readable constants like `ProviderCloudflareAIGateway`, `ProviderSAPAICore`, `ProviderDeepInfra`.
