-- Port of prisma/schema.prisma to PostgreSQL. Applied at boot; recorded in schema_migrations.

CREATE TABLE IF NOT EXISTS users (
  id                  SERIAL PRIMARY KEY,
  email               TEXT NOT NULL UNIQUE,
  password            TEXT NOT NULL,
  role                TEXT NOT NULL DEFAULT 'client',
  email_verified      BOOLEAN NOT NULL DEFAULT false,
  must_change_password BOOLEAN NOT NULL DEFAULT false,
  verification_token  TEXT UNIQUE,
  reset_token         TEXT UNIQUE,
  reset_token_expiry  TIMESTAMPTZ,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One-time registration details. A missing row (not a boolean flag, unlike
-- must_change_password) is what gates a user into the completion form — the
-- data itself is the "is this done" signal, so there's nothing to keep in sync.
CREATE TABLE IF NOT EXISTS user_profiles (
  id                       SERIAL PRIMARY KEY,
  user_id                  INTEGER NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  first_name               TEXT NOT NULL,
  last_name                TEXT NOT NULL,
  address                  TEXT NOT NULL,
  address2                 TEXT,
  state                    TEXT NOT NULL,
  zip                      TEXT NOT NULL,
  country                  TEXT NOT NULL DEFAULT 'US',
  phone                    TEXT NOT NULL,
  communication_preference TEXT NOT NULL DEFAULT 'email',
  linkedin                 TEXT,
  github                   TEXT,
  alt_email                TEXT
);

CREATE TABLE IF NOT EXISTS email_queue (
  id         SERIAL PRIMARY KEY,
  "to"       TEXT NOT NULL,
  subject    TEXT NOT NULL,
  body       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  attempts   INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  sent_at    TIMESTAMPTZ
);
