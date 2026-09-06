# OpenRouter usage concentration: the 90% creator and family sets

Window: 2026-07-29 to 2026-08-27 (30 days, the last complete day at capture time).
Total: 348,240,585,898,718 tokens across the daily top-50 rows plus the `other` row.
Method: `scripts/openrouter_usage.py` captured `/api/v1/datasets/rankings-daily`;
each permaslug (date suffix and `:variant` stripped) resolved through the
production parser (`bestiary show openrouter/<id>`) at the 2026-08-28 catalog
(pull request #40); tokens rolled up to Creator and to family.

Bounds. The `other` row holds 6.0% of tokens: usage outside each day's top 50
that the data cannot attribute. Unmatched permaslugs hold 2.7% (8 slugs; the
largest are Anthropic tier-before-version spellings, recorded as defect class 6
in issue #43). The stealth model `ox-alpha` (7.8%) has no lab and is reported
as-is.

## Creators, cumulative to >= 90% (16 entries)

| # | creator | tokens | share | cumulative |
|---|---|---|---|---|
| 1 | deepseek | 79,707,477,580,208 | 22.9% | 22.9% |
| 2 | openai | 34,860,477,310,277 | 10.0% | 32.9% |
| 3 | tencent | 33,618,197,374,819 | 9.7% | 42.6% |
| 4 | xiaomi | 31,180,227,903,859 | 9.0% | 51.5% |
| 5 | (no creator: stealth `ox-alpha`) | 27,296,262,958,790 | 7.8% | 59.3% |
| 6 | google | 27,163,410,636,495 | 7.8% | 67.1% |
| 7 | zhipu | 18,289,847,601,797 | 5.3% | 72.4% |
| 8 | nvidia | 18,005,694,434,179 | 5.2% | 77.6% |
| 9 | anthropic | 12,840,397,592,206 | 3.7% | 81.3% |
| 10 | minimax | 8,469,905,865,165 | 2.4% | 83.7% |
| 11 | poolside | 7,157,434,941,556 | 2.1% | 85.7% |
| 12 | moonshotai | 6,481,832,919,149 | 1.9% | 87.6% |
| 13 | stepfun | 4,199,992,196,485 | 1.2% | 88.8% |
| 14 | inclusionai | 2,126,246,003,471 | 0.6% | 89.4% |
| 15 | xai | 1,897,844,774,815 | 0.5% | 90.0% |
| 16 | alibaba | 1,886,166,499,427 | 0.5% | 90.5% |

## Families, cumulative to >= 90% (17 entries)

deepseek 22.9% · gpt 10.0% · hy 9.7% · mimo 9.0% · ox-alpha 7.8% ·
gemini 6.9% · glm 5.3% · nemotron 5.2% · claude 3.7% · minimax 2.4% ·
laguna 2.1% · kimi 1.9% · step 1.2% · gemma 0.9% · ling 0.6% ·
grok 0.5% · qwen 0.5% (cumulative 90.5%).

## Data

The aggregated per-day rows are beside this file:
`openrouter-rankings-daily_2026-07-29_2026-08-27.csv`. The raw API responses
stay in the uncommitted capture cache. License of the source data: CC BY 4.0.

Source: OpenRouter (openrouter.ai/rankings), as of 2026-08-27.
