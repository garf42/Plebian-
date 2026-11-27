# node: n.tools

spec: AG-03; AG-05 (sandbox, secrets, egress); Plebian bindings (tool classes); TP-T2

## assume
[BINDING] origin: specified
- in: n.browser — browse sub-agent summaries, readings, held requests as drafts, and downloads
- in: n.underlay — a message's attachments, as files, put out by lookup
- in: n.feeds — mail send and text send paths
- in: src.web — public HTTP for fetch and search

## guarantee
[BINDING] origin: specified
- out: per call — reading bytes, exit code, class (read, local change, outside change), a flag set when the bytes came from beyond the machine, and a job id when the call outlasts its turn
- `read` cannot write anywhere and has no network; `run` writes only the workspace and reaches the network only when the call asks for it
- the workspace keeps an outside folder holding attachments lookup put out, the browser's downloads, and whatever a call wrote after reaching the network or opening the folder; such a call writes nowhere else; a call opens the folder only when it asks, and its reading is then flagged as from beyond the machine
- tools run as the tools user, through a runner that user's own service keeps (a service can't switch users); the store, the secrets file and the browser profile are unreadable to it
- a secret's value never appears in a reading, from any output stream
- the tool band holds one line per tool, skill, owner-added MCP tool and library script; `use describe <name>` returns the full schema as a reading
- the MCP client works with servers speaking protocol 2026-07-28 and 2025-11-25
- a tool's class comes from the harness's table, or from annotations of MCP servers the owner added (a server joins the library only as a draft the owner approved); anything unannotated is an outside change
- search reads a results page whose URL is a setting (a keyless engine's plain-HTML page when none is set), so it works with no key or account
- the agent can write new skills and scripts into the library; a new capability that reaches past the machine starts with no standing

## oracle
type: derivable
risk: semantic
fixtures: `tests/tools/` (sandbox escapes, secret in stdout and stderr, an unannotated MCP tool, a local search page, a mail with a PDF attached, a fixture page that a networked run saves)

## metamorphic relations
[BINDING] origin: specified
- MR-1: rename a secret's variable ⇒ the reading is unchanged (no value leaks under any name)
- MR-2: the same `use` call before and after `use describe` ⇒ identical reading

## demons
- D1: scrubs secrets from stdout but not stderr
  kill_test: tests/tools/test_secret_scrub_all_streams
  negative_check: seen red — --- FAIL: TestSecretScrubAllStreams (secret left in stderr)
  status: killed
- D2: `read` can still write through /tmp
  kill_test: tests/tools/test_read_cannot_write
  negative_check: seen red — --- FAIL: TestReadCannotWrite (read wrote through /tmp)
  status: killed
- D3: an MCP tool without annotations is treated as a read
  kill_test: tests/tools/test_unannotated_is_outside_change
  negative_check: pending
  status: live
- D4: Playwright MCP's own tool definitions land in the main tool band
  kill_test: tests/tools/test_main_band_excludes_browser_defs
  negative_check: pending
  status: live
- D5: a networked run saves a page into the workspace outside the outside folder, and a later turn's `read` returns its words unflagged
  kill_test: tests/tools/test_outside_bytes_keep_mark
  negative_check: seen red — --- FAIL: TestOutsideBytesKeepMark (a networked run wrote into the workspace)
  status: killed

## clauses
- [BINDING] origin: derived — the MCP client accepts servers that negotiate 2025-11-25 (Playwright MCP 0.0.86 does) as well as 2026-07-28 (ledger: L0006) (kill_test: tests/tools/test_mcp_protocol_versions)
- [BINDING] origin: derived — browser tool definitions (25 tools, 21.8 KB from Playwright MCP) live only in the browser sub-agent's context (ledger: L0003) (kill_test: tests/tools/test_main_band_excludes_browser_defs)

## open gaps
- G007 — unprivileged sandbox on Raspberry Pi OS
- G013 — keyless search default
- G016 — DevTools endpoint reachable from loopback
