> Discover all available pages from the documentation index: https://mastra.ai/llms.txt

# RegexFilterProcessor

The `RegexFilterProcessor` applies zero-cost regex pattern matching to filter, redact, or block content in agent messages. No LLM calls are made. All detection is regex-based.

Supports built-in presets for common patterns (PII, secrets, URLs) and custom regex rules. Can be applied to input, output, or both phases.

## Usage example

Block PII in input messages:

```typescript
import { RegexFilterProcessor } from '@mastra/core/processors'

const filter = new RegexFilterProcessor({
  presets: ['pii'],
  strategy: 'block',
  phase: 'input',
})
```

Redact secrets in output:

```typescript
import { RegexFilterProcessor } from '@mastra/core/processors'

const filter = new RegexFilterProcessor({
  presets: ['secrets'],
  strategy: 'redact',
  phase: 'output',
})
```

Custom rules:

```typescript
import { RegexFilterProcessor } from '@mastra/core/processors'

const filter = new RegexFilterProcessor({
  rules: [{ name: 'internal-id', pattern: /INTERNAL-\d{6}/g, replacement: '[INTERNAL_ID]' }],
  strategy: 'redact',
})
```

Attach to an agent:

```typescript
import { Agent } from '@mastra/core/agent'
import { RegexFilterProcessor } from '@mastra/core/processors'

const agent = new Agent({
  id: 'my-agent',
  name: 'my-agent',
  model: 'openai/gpt-5-nano',
  inputProcessors: [
    new RegexFilterProcessor({
      presets: ['pii', 'secrets'],
      strategy: 'block',
    }),
  ],
})
```

## Constructor parameters

**rules** (`RegexRule[]`): Custom regex rules to apply. Each rule has a name, a regex pattern, and an optional replacement string.

**rules.name** (`string`): Display name for the rule (used in match reports and error messages).

**rules.pattern** (`RegExp`): The regex pattern to match against.

**rules.replacement** (`string`): Replacement string for redact strategy. Defaults to '\[REDACTED]'.

**presets** (`('pii' | 'secrets' | 'urls')[]`): Built-in preset categories. 'pii' matches emails, phone numbers, SSNs, credit cards. 'secrets' matches API keys, bearer tokens, AWS keys. 'urls' matches HTTP/HTTPS URLs.

**strategy** (`'block' | 'redact' | 'warn'`): Strategy when a pattern match is found. 'block' aborts with a TripWire error. 'redact' replaces matched content with replacement text. 'warn' logs a warning but passes content through unchanged. (Default: `'block'`)

**phase** (`'input' | 'output' | 'all'`): Phases to apply the filter. 'input' filters input messages. 'output' filters output stream and result. 'all' filters both. (Default: `'all'`)

**includeRedactedValues** (`boolean`): Include the text that was redacted in each report entry. Off by default, because the values are the data the processor removes. (Default: `false`)

## Returns

**id** (`'regex-filter'`): Processor identifier.

**name** (`'Regex Filter'`): Processor display name.

**processInput** (`(args: ProcessInputArgs) => ProcessInputResult`): Checks input messages against all configured rules. Blocks, redacts, or warns depending on strategy. Skipped when phase is output.

**processOutputStream** (`(args: ProcessOutputStreamArgs) => Promise<ChunkType | null | undefined>`): Checks streaming text-delta chunks against all configured rules. Skipped when phase is input.

**processOutputResult** (`(args: ProcessOutputResultArgs) => ProcessorMessageResult`): Checks output messages against all configured rules. Blocks, redacts, or warns depending on strategy. Skipped when phase is input.

## Error behavior

When the `block` strategy is active (default), `RegexFilterProcessor` throws a `TripWire` error with `retry: false` when any pattern matches. The TripWire metadata includes:

- `processorId`: `'regex-filter'`
- `matches`: Array of match objects with `rule`, `match` (redacted to `'[REDACTED_MATCH]'`), and `index`
- `strategy`: `'block'`

## Built-in presets

| Preset    | Patterns                                         | Default replacement                            |
| --------- | ------------------------------------------------ | ---------------------------------------------- |
| `pii`     | Emails, phone numbers, SSNs, credit card numbers | `[EMAIL]`, `[PHONE]`, `[SSN]`, `[CREDIT_CARD]` |
| `secrets` | API keys, bearer tokens, AWS access keys         | `[API_KEY]`, `[BEARER_TOKEN]`, `[AWS_KEY]`     |
| `urls`    | HTTP/HTTPS URLs                                  | `[URL]`                                        |

## Redaction behavior

Every rule is matched independently, so two rules can claim text that overlaps. A card number written without separators matches both `phone` and `credit-card`, for example. Overlapping matches are combined into a single region and replaced once, using the replacement of the longest match.

```typescript
const filter = new RegexFilterProcessor({
  presets: ['pii'],
  strategy: 'redact',
})

// "Charge 4111111111111111 today" becomes "Charge [CREDIT_CARD] today"
```

A replacement string can reference capture groups with `$1` or `$&`. Those references resolve for a single match whose pattern also matches the matched text on its own. In a combined region, or for a rule anchored on its surroundings with a lookbehind or lookahead, the replacement string is inserted as written. The region is redacted either way.

## Redaction reporting

The `redact` strategy rewrites text in place, so nothing downstream can tell what changed. Assign `onViolation` to record it. The processor calls it once per redacted message, message part, or stream chunk, and offsets are relative to that piece of text. Async callbacks are awaited, and errors are caught so an unavailable audit sink can't fail the request.

```typescript
import { RegexFilterProcessor, type RegexRedactionDetail } from '@mastra/core/processors'

const filter = new RegexFilterProcessor({
  presets: ['pii'],
  strategy: 'redact',
})

filter.onViolation = async ({ detail }) => {
  const redaction = detail as RegexRedactionDetail

  for (const entry of redaction.redactions) {
    await auditLog.write({
      phase: redaction.phase,
      messageId: redaction.messageId,
      rule: entry.rule,
      offset: entry.index,
      length: entry.length,
    })
  }
}
```

The callback is awaited, including in `processOutputStream`, where it runs for every chunk that contains a match. Keep the callback fast, or hand the work to a queue, so a slow audit sink doesn't stall a streaming response. With no callback attached, the `redact` path stays synchronous.

The `block` strategy reports through the same callback. There the processor runner invokes it when it catches the `TripWire`, so `detail` holds the tripwire metadata described under [Error behavior](#error-behavior) rather than the shape below.

`detail` for a redaction is a `RegexRedactionDetail`:

**strategy** (`'redact'`): Distinguishes a redaction report from the block strategy payload.

**phase** (`'processInput' | 'processOutputStream' | 'processOutputResult'`): Processor method that applied the redactions.

**messageId** (`string`): Id of the message the text came from. Absent for stream chunks.

**partIndex** (`number`): Index of the redacted part in the message's parts array, which also contains non-text parts. Absent for string content and stream chunks.

**redactions** (`RegexRedaction[]`): Redactions in the order they appear in the text.

**redactions.rule** (`string`): Name of the rule whose replacement was used.

**redactions.index** (`number`): Start offset of the redacted span in the text.

**redactions.length** (`number`): Length of the redacted span.

**redactions.replacement** (`string`): Text that replaced the span.

**redactions.overlappingRules** (`string[]`): Names of all rules that matched this span, set only when more than one overlapped.

**redactions.value** (`string`): The text that was redacted. Set only when includeRedactedValues is enabled.

Values are left out by default. An audit trail that copies the data it protects widens the exposure it was added to narrow. Set `includeRedactedValues` only when the destination is as protected as the original, and note that the `block` strategy also withholds matched text from its `TripWire` metadata for the same reason.