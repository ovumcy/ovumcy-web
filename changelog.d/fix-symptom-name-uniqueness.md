### Fixed

- **Two requests can no longer give one account two symptoms with the same name.** The
  per-owner name rule lived only in application code: the symptom service listed the owner's
  catalogue, compared normalized names in memory, and then issued an unrelated `INSERT`, with
  nothing joining the two. Two requests that both passed the check before either wrote both
  wrote, and the account was left holding two symptoms the day-entry picker, the frequency
  counts and the export cannot tell apart. The built-in seeding path had the same shape on a
  read path — two page loads each list the catalogue, each compute the same missing built-ins
  and each insert them — which is the half a normal session reaches, since it needs only two
  overlapping page loads rather than two overlapping form submissions.

  `symptom_types` now carries a unique index on `(user_id, lower(name))`, so the second writer
  is refused by the database rather than by a check it had already passed. A refused create or
  rename answers with the same "symptom name already exists" conflict a duplicate has always
  produced. A refused built-in seeding does not surface at all — the request that lost the race
  only wanted to read, and it is answered with the catalogue the other one wrote — but only once
  a re-read confirms those built-ins are in fact there now. A refusal that leaves the catalogue
  short is reported, rather than repeating in silence on every page load.

  The index is keyed per account, so two accounts on one instance each keep their own "Cramps",
  and it covers archived symptoms as well as active ones — an archived name has always stayed
  taken for its owner, and restoring an archived symptom re-checks it, so leaving archived rows
  out would have admitted a pair that can never be restored.

  What the index does **not** do is stated in the migration and pinned by a test: it is a
  backstop weaker than the application rule, which also collapses runs of internal whitespace
  and drops invalid UTF-8. Portable SQL can do neither, so "Mood&nbsp;&nbsp;swings" and "Mood
  swings" are one name to the application and two keys to the index, and the application check
  stays in front of it. `lower()` differs between engines as well: PostgreSQL folds by locale,
  SQLite folds ASCII only, so a case-only variant of a non-ASCII name is covered on one and not
  the other.

- **The day-entry picker hides the four built-ins it says it hides.** The set of built-ins kept
  out of the picker was keyed on a spelling of the display name and looked up through a
  normalizer that lowercases and collapses whitespace but never removes it, so the entry
  `moodswings` matched nothing the product can create. Fatigue, Irritability and Insomnia were
  kept out; Mood swings was not, and was merely pushed into the picker's overflow by a second,
  unrelated rule. The set is now keyed on the catalogue's own identity for a built-in, so all
  four are kept out of the list unless the day already carries them, and a key that names no
  built-in fails a test instead of quietly hiding nothing.

### Internal

- **A migration that adds a unique index stops rather than making room.** On a database that
  already holds rows the new index cannot cover, the migration refuses and names every
  conflicting group — the key value and how many rows share it — instead of creating the index.
  It never deletes, merges or rewrites a row: this instance stores one person's health history,
  and a schema change is not consent to lose part of it. Nothing is executed, the migration's
  transaction is rolled back, and the message says what to resolve through the application
  before starting it again. The check reads the key expressions out of the `CREATE UNIQUE INDEX`
  statement itself, so it covers the next unique index as well as this one, and it excludes
  `NULL` keys because both engines treat those as distinct in a unique index. A statement it
  cannot read — a partial index, or anything trailing the key list — is left to the engine,
  which still refuses a genuine conflict with its own message.

  Rolling this migration back is `DROP INDEX idx_symptom_types_user_name_unique`. It writes no
  data at all, so dropping the index restores the previous state exactly.

- **A migration can no longer cut its own prose in half.** The runner splits statements on every
  `;` without stripping comments, so a semicolon in the middle of a comment line turns the rest
  of that sentence into the next statement, and the engine answers with a syntax error naming
  English. Every migration file opens with a long prose header, and the rule against it lived
  only in the closing paragraph of the files that happened to repeat it. A sweep over both
  dialect trees now enforces it, narrowed to the form that actually breaks: a semicolon that ends
  a comment line is harmless and three shipped files rely on that, so only a semicolon with text
  after it on the same line is refused.

- **The duplicate-name class is held by a barrier test rather than by timing.** Two goroutines
  are released together and rendezvous on the return of the catalogue read, so both hold a
  decision made from the same snapshot before either writes — starting two goroutines together
  is not enough, since one usually finishes its insert before the other reads, and the guard
  then passes on a tree with no constraint at all. One case covers the create path and one the
  built-in seeding path; a third pins what the index does and does not normalize, and a fourth
  pins that the name stays per owner and stays taken across archival.
