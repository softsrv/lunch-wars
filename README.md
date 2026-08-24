# MunchBot

A Discord bot that helps a group pick a restaurant for lunch via ranked-choice
voting, instead of endless back-and-forth. See [DESIGN.md](DESIGN.md) for the
full product spec.

## How a vote flows

1. **`/lunch [time]`** — starts (or schedules) a lunch decision in the
   current channel. Participants must run **`/join`** before the host closes
   joining with `/next`.
   - `time` is optional: `in 30m`, `in 1h15m`, or a 24-hour clock time like
     `12:30` (the next occurrence of that time). Omit it to start
     immediately.
2. **`/nominate <list>`** — each participant nominates up to 3 restaurants,
   comma-separated (e.g. `/nominate situ, cracklemi, marketpho`). Names are
   deduplicated case-insensitively.
3. The host runs **`/next`** to close nominations and open vetoes, which
   prints the compiled, numbered list.
4. **`/veto <name-or-number>`** — each participant may veto one restaurant.
   A veto is rejected if it would drop the list below 3 remaining choices.
5. The host runs **`/next`** again to close vetoes and open the final vote,
   printing the revised list.
6. **`/vote <list>`** — each participant ranks up to 3 remaining restaurants
   in order of preference (e.g. `/vote marketpho, situ, cracklemi`).
7. The host runs **`/next`** one more time to close voting. MunchBot runs
   instant-runoff ranked-choice voting and posts one message showing every
   round and the final winner.

**`/cancel`** (host only) cancels an in-progress vote at any point.

Only the user who ran `/lunch` (the "host") can run `/next` or `/cancel`.
Election history is kept in Postgres but is never exposed except through
these slash commands.

## Tech stack

- Go, using [`discordgo`](https://github.com/bwmarrin/discordgo) for the
  Discord gateway/API.
- Postgres for all election state (schema in
  [`internal/store/schema.sql`](internal/store/schema.sql), applied
  automatically on startup).
- No web frontend, no user authentication beyond Discord identity.

## Running it

1. Create a Discord application + bot user, invite it to your server with
   the `applications.commands` and `bot` scopes (the bot needs permission to
   send messages and read message history in the channels it'll be used
   in).
2. Have a Postgres database ready.
3. Copy [`.env.example`](.env.example) to `.env` and fill in real values (or
   otherwise set these as environment variables):
   - `DISCORD_TOKEN` — the bot token.
   - `DATABASE_URL` — a Postgres connection string, e.g.
     `postgres://user:pass@host:5432/munchbot?sslmode=disable`.
   - `DISCORD_GUILD_ID` (optional) — scope slash commands to one guild for
     near-instant registration while developing. Omit for global commands
     (which can take up to an hour to propagate to all servers).
4. Run it:

   ```sh
   go run ./cmd/munchbot
   ```

   Or, for hot reloading while developing (rebuilds and restarts on file
   changes via [air](https://github.com/air-verse/air), installing it
   automatically if it's not already on your `PATH`):

   ```sh
   make dev
   ```

   The app loads `.env` into its environment automatically (via
   [`godotenv`](https://github.com/joho/godotenv)) if the file exists;
   real environment variables always take precedence over it.

   The schema is applied automatically on every startup (`CREATE TABLE IF
   NOT EXISTS`, safe to re-run).

## Development

```sh
go build ./...
go test ./...
```

The Postgres-backed tests in `internal/store` are skipped unless
`MUNCHBOT_TEST_DATABASE_URL` is set, so the default test run needs no
database:

```sh
docker run -d --rm --name munchbot-test-pg \
  -e POSTGRES_PASSWORD=munchbot -e POSTGRES_DB=munchbot \
  -p 55432:5432 postgres:16-alpine

MUNCHBOT_TEST_DATABASE_URL="postgres://postgres:munchbot@localhost:55432/munchbot?sslmode=disable" \
  go test ./... -v

docker stop munchbot-test-pg
```
