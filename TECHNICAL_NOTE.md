# Technical Note

## 1. Design decision

`Service.Progress` computes progress on every read from the append-only attempts returned by `Repository.List`, instead of storing a best-score aggregate. All rules live in one place, and a later lower score cannot overwrite anything. Tradeoff: each read loads the learner's whole attempt history for the course.

## 2. Design questions

### Retried requests

Add a migration with `UNIQUE (attempt_id)` and insert with `ON CONFLICT (attempt_id) DO NOTHING RETURNING id`. If no row returns, load the stored attempt and compare all fields: identical → return it, different → `ErrConflict` (409, already mapped). For simultaneous requests, the unique index makes the second insert wait for the first to commit, then it takes the conflict path.

### Concurrent requests

No. Both are independent inserts and the best score is a `max` at read time, so there is no read-modify-write to race. A simultaneous read sees a per-statement snapshot: no score, 70 or 90, each a valid state. Any read after both commits returns 90. No protection needed; it would be if we stored an aggregate (then `GREATEST` in one `UPDATE`).

### Redis

Not first. I would aggregate in PostgreSQL (`MAX(score) ... GROUP BY lesson_id` with an index on `(learner_id, course_id, lesson_id, score)`). If still slow, cache-aside in Redis: the computed progress per learner and course, with a TTL, deleted after the insert commits. PostgreSQL stays the source of truth; if Redis is down or flushed, reads fall back to PostgreSQL, with staleness bounded by the TTL.

## 3. Limitations and production readiness

No idempotency, so a retried POST stores a duplicate (harmless to best score and pass status). Before production I would verify the attempt-ID migration, check query plans under large histories, add authorization so learners read only their own progress, and run `make lint` and the PostgreSQL test in CI.

## 4. Time and AI tools

Time spent: 1 hour and 20 minutes

AI tools used: Claude (Anthropic) drafted the implementation, tests and this note. I reviewed every change, decided to inline the progress calculation into `Service.Progress`, and ran `make setup` and `make lint` locally. I can explain and change all of it.
