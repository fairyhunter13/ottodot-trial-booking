INSERT INTO parents (id, name, email) VALUES
  ('p1', 'Sari Wijaya',  'sari@example.test'),
  ('p2', 'Andi Pratama', 'andi@example.test'),
  ('p3', 'Rina Halim',   'rina@example.test'),
  ('p4', 'Budi Santoso', 'budi@example.test');

INSERT INTO students (id, parent_id, name, grade) VALUES
  ('s1',  'p1', 'Nadia', 'Grade 4'),
  ('s2',  'p1', 'Bayu',  'Grade 2'),
  ('s3',  'p1', 'Rania', 'Grade 6'),
  ('s4',  'p2', 'Farel', 'Grade 3'),
  ('s5',  'p3', 'Dinda', 'Grade 4'),
  ('s6',  'p3', 'Galih', 'Grade 5'),
  ('s7',  'p3', 'Intan', 'Grade 4'),
  ('s8',  'p4', 'Yusuf', 'Grade 3'),
  ('s9',  'p4', 'Laras', 'Grade 5'),
  ('s10', 'p4', 'Fajar', 'Grade 6'),
  ('s11', 'p4', 'Maya',  'Grade 2');

-- c1 has seats free, c2 has exactly one seat left, c3 is full.
INSERT INTO trial_classes (id, subject, starts_at, capacity) VALUES
  ('c1', 'Math Trial',    datetime('now', '+2 days'), 4),
  ('c2', 'Science Trial', datetime('now', '+3 days'), 4),
  ('c3', 'Math Trial',    datetime('now', '+4 days'), 4);

INSERT INTO bookings (id, student_id, class_id, status, reason, created_at, updated_at) VALUES
  -- s1 already holds a seat in c1, so booking s1 into c1 again is the duplicate case.
  ('b1', 's1',  'c1', 'confirmed',      '', datetime('now'), datetime('now')),
  -- s2 failed payment on c1, so the retry case is one click.
  ('b2', 's2',  'c1', 'payment_failed', '', datetime('now'), datetime('now')),
  ('b3', 's5',  'c2', 'confirmed',      '', datetime('now'), datetime('now')),
  ('b4', 's6',  'c2', 'confirmed',      '', datetime('now'), datetime('now')),
  ('b5', 's7',  'c2', 'confirmed',      '', datetime('now'), datetime('now')),
  ('b6', 's8',  'c3', 'confirmed',      '', datetime('now'), datetime('now')),
  ('b7', 's9',  'c3', 'confirmed',      '', datetime('now'), datetime('now')),
  ('b8', 's10', 'c3', 'confirmed',      '', datetime('now'), datetime('now')),
  ('b9', 's11', 'c3', 'confirmed',      '', datetime('now'), datetime('now'));

INSERT INTO payment_attempts (id, booking_id, outcome, amount_cents, refund_required, created_at) VALUES
  ('pa1', 'b1', 'succeeded', 2500, 0, datetime('now')),
  ('pa2', 'b2', 'failed',    2500, 0, datetime('now'));
