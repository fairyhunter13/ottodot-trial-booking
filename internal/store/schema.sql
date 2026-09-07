CREATE TABLE IF NOT EXISTS parents (
  id    TEXT PRIMARY KEY,
  name  TEXT NOT NULL,
  email TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS students (
  id        TEXT PRIMARY KEY,
  parent_id TEXT NOT NULL REFERENCES parents(id),
  name      TEXT NOT NULL,
  grade     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS trial_classes (
  id        TEXT PRIMARY KEY,
  subject   TEXT NOT NULL,
  starts_at TEXT NOT NULL,
  capacity  INTEGER NOT NULL CHECK (capacity > 0)
);

CREATE TABLE IF NOT EXISTS bookings (
  id         TEXT PRIMARY KEY,
  student_id TEXT NOT NULL REFERENCES students(id),
  class_id   TEXT NOT NULL REFERENCES trial_classes(id),
  status     TEXT NOT NULL CHECK (status IN ('pending_payment','confirmed','payment_failed','cancelled')),
  reason     TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Invariant 1: a child holds at most one live booking per class.
CREATE UNIQUE INDEX IF NOT EXISTS one_active_booking ON bookings (student_id, class_id)
  WHERE status IN ('pending_payment', 'confirmed');

CREATE INDEX IF NOT EXISTS bookings_class_status ON bookings (class_id, status);

CREATE TABLE IF NOT EXISTS payment_attempts (
  id              TEXT PRIMARY KEY,
  booking_id      TEXT NOT NULL REFERENCES bookings(id),
  outcome         TEXT NOT NULL CHECK (outcome IN ('succeeded','failed')),
  amount_cents    INTEGER NOT NULL,
  refund_required INTEGER NOT NULL DEFAULT 0,
  created_at      TEXT NOT NULL
);
