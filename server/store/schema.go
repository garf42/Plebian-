package store

import "fmt"

// migrations are forward-only and versioned; a migration never rewrites sealed bytes.
// A node adds its own tables by appending a migration to this one list (never in init
// functions: their order follows file names).
var migrations = []string{
	// 1: TP-03 tables and the server's own records.
	`
CREATE TABLE turns(
  id INTEGER PRIMARY KEY,          -- n: one model call
  opener TEXT NOT NULL,
  spelling BLOB NOT NULL,          -- labels, loose words and changes; never the raw bytes
  sha BLOB NOT NULL,               -- sha256 of the exact bytes, fixed at seal
  at TEXT NOT NULL,                -- wall time, addressing only
  shown TEXT NOT NULL DEFAULT '[]',-- fragment ids the spotlight showed
  heard_opened INTEGER NOT NULL DEFAULT 0,
  outside_in_view INTEGER NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  request_id TEXT NOT NULL DEFAULT '',
  bands TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE fragments(
  id INTEGER PRIMARY KEY,
  turn INTEGER,                    -- sealed turn, or NULL for a heard stretch
  heard INTEGER,                   -- heard row, or NULL
  part INTEGER NOT NULL,
  start INTEGER NOT NULL,          -- byte offsets into the row's exact bytes
  stop INTEGER NOT NULL,
  tokens INTEGER NOT NULL DEFAULT 0,
  writer TEXT NOT NULL,
  heard_opened INTEGER NOT NULL DEFAULT 0,
  light REAL NOT NULL DEFAULT 1,
  parent INTEGER,
  open INTEGER NOT NULL DEFAULT 0,
  kind TEXT NOT NULL DEFAULT 'line'
);
CREATE INDEX fragments_turn ON fragments(turn);
CREATE INDEX fragments_heard ON fragments(heard);
CREATE TABLE symbols(
  id INTEGER PRIMARY KEY,
  body TEXT NOT NULL,              -- thread | heard
  expansion BLOB NOT NULL,
  left_sym INTEGER, right_sym INTEGER,
  uses INTEGER NOT NULL DEFAULT 0,
  info REAL NOT NULL DEFAULT 0,
  rec_a REAL NOT NULL DEFAULT 0,   -- record in calls: successes
  rec_b REAL NOT NULL DEFAULT 0    -- failures
);
CREATE INDEX symbols_expansion ON symbols(body, expansion);
CREATE TABLE pairs(left_sym INTEGER, right_sym INTEGER, turn INTEGER, heard INTEGER, pos INTEGER);
CREATE INDEX pairs_lr ON pairs(left_sym, right_sym);
CREATE TABLE uses(
  symbol INTEGER NOT NULL,
  turn INTEGER,                    -- thread clock
  tick INTEGER,                    -- underlay clock (one per stretch or message)
  body TEXT NOT NULL,
  d REAL NOT NULL,                 -- decay fixed at the moment of use
  change TEXT                      -- the change if a near-match
);
CREATE INDEX uses_symbol_turn ON uses(symbol, turn);
CREATE TABLE frames(id INTEGER PRIMARY KEY, left_sym INTEGER, right_sym INTEGER, filler INTEGER, n INTEGER NOT NULL DEFAULT 0);
CREATE TABLE classes(id INTEGER PRIMARY KEY, members TEXT NOT NULL);
CREATE TABLE stand_ins(word TEXT PRIMARY KEY, proposals TEXT NOT NULL, written_at TEXT NOT NULL);
CREATE TABLE outcomes(
  id INTEGER PRIMARY KEY,
  turn INTEGER NOT NULL,           -- the turn that settled it
  call_turn INTEGER, call_id TEXT, -- the call it settles
  kind TEXT NOT NULL,              -- exit | reading | owner | draft | redo
  exit_code INTEGER, error TEXT, success INTEGER NOT NULL,
  settles TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE trust(fragment INTEGER PRIMARY KEY, a REAL NOT NULL DEFAULT 0, b REAL NOT NULL DEFAULT 0);
CREATE TABLE bands(id INTEGER PRIMARY KEY, layer TEXT NOT NULL, bytes BLOB NOT NULL, sha BLOB NOT NULL, turn INTEGER NOT NULL);
CREATE TABLE heard(
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL,            -- talk | mail | text | calendar
  at TEXT NOT NULL, end_at TEXT NOT NULL,
  spelling BLOB NOT NULL, sha BLOB NOT NULL,
  blob TEXT NOT NULL DEFAULT '',   -- sha256 of the original in the blob store
  ext TEXT NOT NULL DEFAULT '',    -- source key (mailbox/uidvalidity/uid, uid/recurrence-id, conversation/message, audio sha+span)
  covers INTEGER,                  -- an earlier reading of the same thing this one covers
  tick INTEGER NOT NULL DEFAULT 0,
  landed_at TEXT NOT NULL,
  settled INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX heard_at ON heard(at);
CREATE INDEX heard_ext ON heard(source, ext);
CREATE TABLE standing(
  step TEXT PRIMARY KEY,
  drafts INTEGER NOT NULL DEFAULT 0, as_drafted INTEGER NOT NULL DEFAULT 0,
  changed INTEGER NOT NULL DEFAULT 0, turned_down INTEGER NOT NULL DEFAULT 0,
  has_standing INTEGER NOT NULL DEFAULT 0, granted_tau REAL, grant_id TEXT
);

CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE devices(id TEXT PRIMARY KEY, name TEXT NOT NULL, public_key BLOB NOT NULL, token_hash BLOB NOT NULL UNIQUE, paired_at TEXT NOT NULL);
CREATE TABLE pair_codes(code_hash BLOB PRIMARY KEY, created_at TEXT NOT NULL, used_at TEXT);
CREATE TABLE nonces(device TEXT NOT NULL, nonce TEXT NOT NULL, PRIMARY KEY(device, nonce));
CREATE TABLE owner_messages(
  id INTEGER PRIMARY KEY, device TEXT NOT NULL, client_msg_id TEXT NOT NULL,
  text TEXT NOT NULL, quote TEXT NOT NULL DEFAULT '', sent_at TEXT NOT NULL, received_at TEXT NOT NULL,
  turn INTEGER, UNIQUE(device, client_msg_id)
);
CREATE TABLE events(id INTEGER PRIMARY KEY, at TEXT NOT NULL, type TEXT NOT NULL, body TEXT NOT NULL, turn INTEGER);
CREATE TABLE calls(
  turn INTEGER NOT NULL, call_id TEXT NOT NULL, tool TEXT NOT NULL, class TEXT NOT NULL,
  state TEXT NOT NULL,             -- sealed | running | done | failed | interrupted | held | drafted
  exit_code INTEGER, job TEXT, PRIMARY KEY(turn, call_id)
);
CREATE TABLE drafts(
  id INTEGER PRIMARY KEY, turn INTEGER NOT NULL, call_id TEXT NOT NULL, step TEXT NOT NULL, tool TEXT NOT NULL,
  call BLOB NOT NULL,              -- the call exactly as written
  payload BLOB NOT NULL,           -- the exact bytes that would run
  shown TEXT NOT NULL,             -- the payload decoded for reading, nothing left out
  state TEXT NOT NULL,             -- waiting | ran | failed | declined | superseded
  shown_at TEXT NOT NULL, settled_turn INTEGER
);
CREATE TABLE jobs(id TEXT PRIMARY KEY, turn INTEGER NOT NULL, call_id TEXT NOT NULL, opener TEXT NOT NULL, state TEXT NOT NULL, result BLOB, exit_code INTEGER);
CREATE TABLE taps(id INTEGER PRIMARY KEY, at TEXT NOT NULL, how TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', stretch INTEGER, title TEXT NOT NULL DEFAULT '', line TEXT NOT NULL DEFAULT '');
CREATE TABLE voices(id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT '', owner INTEGER NOT NULL DEFAULT 0, embedding BLOB NOT NULL, stretches INTEGER NOT NULL DEFAULT 0, named_turn INTEGER, asked INTEGER NOT NULL DEFAULT 0);
CREATE TABLE spend(model TEXT NOT NULL, day TEXT NOT NULL, tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0, tokens_cached INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(model, day));
CREATE TABLE budget(model TEXT PRIMARY KEY, b INTEGER NOT NULL, state TEXT NOT NULL DEFAULT '{}');
CREATE TABLE feeds(feed TEXT PRIMARY KEY, last_landing TEXT, longest_quiet_ms INTEGER NOT NULL DEFAULT 0, cursor TEXT NOT NULL DEFAULT '{}', asked_quiet INTEGER NOT NULL DEFAULT 0);
CREATE TABLE loops(
  id INTEGER PRIMARY KEY, kind TEXT NOT NULL,  -- redline | question | order
  fragment INTEGER, step TEXT NOT NULL DEFAULT '', text TEXT NOT NULL,
  opened_turn INTEGER NOT NULL, closed_turn INTEGER, due_at TEXT, call_turn INTEGER, call_id TEXT
);
CREATE TABLE inbounds(id INTEGER PRIMARY KEY, kind TEXT NOT NULL, body TEXT NOT NULL, at TEXT NOT NULL, done INTEGER NOT NULL DEFAULT 0);
`,
	// 2: speech seconds per minute, kept with the stretch when it lands (the ribbon).
	`CREATE TABLE speech(minute TEXT PRIMARY KEY, seconds REAL NOT NULL, stretch INTEGER);`,
	// 3: offline files from the Omi that have landed (an upload repeated lands once).
	`CREATE TABLE audio_files(sha TEXT PRIMARY KEY, device_ms INTEGER NOT NULL, offset_ms INTEGER NOT NULL, landed_at TEXT NOT NULL);`,
}

func (s *Store) migrate() error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_version(v INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(v),0) FROM schema_version`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.DB.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version(v) VALUES(?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
