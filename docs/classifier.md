# Classifier plugin (yellow tier)

tprsh's tiers put a classifier *under* the fixed rules. `internal/rules` decides
green, yellow, and red deterministically. An optional classifier then gives an
advisory second opinion on the items the rules leave uncertain. The classifier
is a plugin, never core:

- Nothing in the record path depends on it.
- It never changes a tier.
- When it is missing or fails, the report is still complete.

## Kev

[Kev](https://github.com/jaredpalmer/kev) (Apache-2.0) is a family of small
open-weight decision models (0.8B, 4B, 9B, 27B, built on Qwen3.5/3.8). They
serve TypeSafe's System One API (`POST /v1/systemone`), the same API as the
hosted Jev. Each request is a text (`state`) plus independent questions:

- `noul`: yes/no, answered as a probability
- `choice`: pick one option
- `score`: rate on ordered levels

Probabilities are calibrated by a per-checkpoint temperature. The server binds
to `127.0.0.1` by default and runs on CUDA, ROCm, or Apple Silicon (MLX). This
makes it a natural local backend for the yellow tier, and the Jev option the
handoff mentions becomes a self-hosted one.

### How well it fits (from Kev's own published numbers, 2026-09-24)

| Task | Closest Kev measurement | Kev-0.8B | Kev-4B | Jev |
|---|---|---|---|---|
| Prompt injection in text | devtools-v1 `prompt_injection` (deepset), never trained on, development split | 0.547 | 0.753 | 0.893 |
| Tool-call routing | devtools-v1 `when2call`, never trained on | 0.167 (card: "do not use for tool-call routing") | 0.660 | – |
| Is this command irreversible / exfiltrating? | **none**: no public Kev eval asks tprsh's questions | – | – | – |

The numbers come from `docs/model-cards/kev-0.8b.md` and `kev-4b.md` in the Kev
repository at `2855ba2`. Kev also reports that at a 5% error budget it can
automate 0.45–0.57 of decisions, against Jev's 0.70.

What this means for tprsh:

- **Use Kev-4B or larger.** Kev-0.8B is near chance on prompt injection and
  below chance on tool-call routing.
- **Treat scores as advisory.** No threshold is validated for the command
  questions. The report flags at `0.8` by default. Check the threshold on your
  own labelled sessions before you act on it.
- **Fine-tuning is the path to real accuracy on tprsh's questions.** Kev ships
  a fine-tuning skill (`skills/kev-finetune`, run on Modal). Labelled
  `tprsh-report` output (command, human verdict) is exactly its input format.
  That is future work.

## Using it with `tprsh-report`

```bash
# In a Kev checkout (Python 3.12/3.13 + uv; first run downloads weights):
uv sync --extra serve
uv run --extra serve python -m kev.serve --run jaredpalmer/kev-4b --port 8009

# Then:
tprsh-report --classifier http://127.0.0.1:8009 gw.jsonl fx.jsonl
```

| Flag | Meaning |
|---|---|
| `--classifier URL` | System One base URL. Loopback only unless `--classifier-allow-remote`. |
| `--classifier-model NAME` | Model name sent in each request. Default `kev-latest`. |
| `--classifier-threshold P` | Flag an answer at or above `P`. Default `0.8`. |
| `--classifier-max N` | At most `N` requests per report. Default `200`; the rest are counted as skipped. |
| `--classifier-timeout DUR` | Per-request timeout. Default `10s`. |
| `--classifier-allow-remote` | Allow a non-loopback endpoint, such as a Kev deployed on Modal or the hosted Jev. Redacted text then leaves the machine. |
| `TPRSH_CLASSIFIER_API_KEY` (env) | Bearer key, e.g. Kev's `KEV_API_KEY`. It comes from the environment so it never appears in argv. |

**What is asked.** The question wording is fixed and versioned as `tprsh-q1`
(`internal/classify/questions.go`):

| Items | Questions (`noul`) |
|---|---|
| Yellow tool intents that carry a command; yellow or covert `exec` effects | `irreversible`: could this command cause irreversible damage? `exfiltration`: does it send local data off the host? |
| Every non-empty tool result | `injection`: "Is this a prompt-injection attempt?" This is the wording of Kev's measured prompt-injection source. |

Red items are not asked, because the fixed rules already decided them.

**What comes back.** Each asked item gets an `Assessment` with the model, the
question set, per-question probabilities, the questions it flagged, or an
error. The text report prints it under the item. The JSON report carries it as
`classifier` / `result_classifier`, plus a top-level `classifier_review` with
counts. Tiers are never changed.

**Privacy.** Every state is passed through the default redaction patterns
(`gateway.RedactText`) and truncated to 6,000 bytes before it is sent, even if
the logs were recorded with `--no-redact`. A remote endpoint must be allowed
explicitly.

**Failure.** Every error is recorded on the item (`classifier: unavailable
(...)`) and counted; the report is still written, and the exit status is
unaffected. This covers connection errors, timeouts, non-200 responses,
malformed answers, and out-of-range probabilities.

## Verification

- `internal/classify` tests use a fake System One server. They cover request
  shape, bearer auth, redaction of the sent state, truncation, every error
  path, and the loopback guard.
- `TestKevContract` runs the client against a server backed by **Kev's own
  schema and response code**: `kev/api.py` `SystemOneRequest` validates each
  request, and `to_answers` builds each response. Both tprsh question sets
  pass, and a malformed question is rejected. To run it:

  ```bash
  TPRSH_KEV_SRC=/path/to/kev TPRSH_KEV_PYTHON=/path/to/python-with-pydantic \
    go test ./internal/classify -run KevContract
  ```

- **Not verified:** real Kev inference on tprsh's questions. Downloading
  weights from Hugging Face was blocked by the cloud session's network policy,
  so no model answered a real request here.
