-- MunchBot schema.
--
-- election status lifecycle:
--   pending    -> waiting for a scheduled start time
--   join       -> users must /join before nominating
--   nominate   -> participants submit up to 3 restaurants each
--   veto       -> each participant may veto up to 1 restaurant
--   vote       -> participants submit ranked choices
--   completed  -> a winner has been chosen
--   cancelled  -> the host cancelled the election

CREATE TABLE IF NOT EXISTS users (
    id         TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS elections (
    id                   BIGSERIAL PRIMARY KEY,
    guild_id             TEXT,
    channel_id           TEXT NOT NULL,
    created_by           TEXT NOT NULL REFERENCES users(id),
    status               TEXT NOT NULL DEFAULT 'pending',
    scheduled_at         TIMESTAMPTZ,
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ,
    winner_restaurant_id BIGINT,
    result_summary       TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only one non-terminal election may be active per channel at a time.
CREATE UNIQUE INDEX IF NOT EXISTS one_active_election_per_channel
    ON elections (channel_id)
    WHERE status NOT IN ('completed', 'cancelled');

CREATE TABLE IF NOT EXISTS election_participants (
    election_id BIGINT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    user_id     TEXT NOT NULL REFERENCES users(id),
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (election_id, user_id)
);

CREATE TABLE IF NOT EXISTS restaurants (
    id              BIGSERIAL PRIMARY KEY,
    election_id     BIGINT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    name_normalized TEXT NOT NULL,
    display_name    TEXT NOT NULL,
    vetoed          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (election_id, name_normalized)
);

CREATE TABLE IF NOT EXISTS nominations (
    id            BIGSERIAL PRIMARY KEY,
    election_id   BIGINT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users(id),
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (election_id, user_id, restaurant_id)
);

CREATE TABLE IF NOT EXISTS vetoes (
    id            BIGSERIAL PRIMARY KEY,
    election_id   BIGINT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users(id),
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (election_id, user_id)
);

CREATE TABLE IF NOT EXISTS votes (
    id            BIGSERIAL PRIMARY KEY,
    election_id   BIGINT NOT NULL REFERENCES elections(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users(id),
    restaurant_id BIGINT NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    rank          SMALLINT NOT NULL CHECK (rank BETWEEN 1 AND 3),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (election_id, user_id, rank),
    UNIQUE (election_id, user_id, restaurant_id)
);
